package p2p

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/brianliucrypto/torrent-client/client"
	"github.com/brianliucrypto/torrent-client/message"
	"github.com/brianliucrypto/torrent-client/peers"
)

const (
	MaxBlockSize = 16384
	MaxBacklog   = 5
)

type Torrent struct {
	Peers       []peers.Peer
	PeerID      [20]byte
	InfoHash    [20]byte
	PieceHashes [][20]byte
	PieceLength int
	Length      int
	Name        string
}

type pieceWork struct {
	index  int
	hash   [20]byte
	length int
}

type pieceResult struct {
	index int
	buf   []byte
}

type pieceProgress struct {
	index      int
	client     *client.Client
	buf        []byte
	downloaded int
	requested  int
	backlog    int
}

func (state *pieceProgress) readMessage() error {
	msg, err := state.client.Read()
	if err != nil {
		return err
	}

	if msg == nil {
		return nil
	}

	switch msg.ID {
	case message.MsgUnchoke:
		state.client.Choked = false
	case message.MsgChoke:
		state.client.Choked = true
	case message.MsgHave:
		index, err := message.ParseHave(msg)
		if err != nil {
			return err
		}
		state.client.Bitfield.SetPiece(index)
	case message.MsgPiece:
		n, err := message.ParsePiece(state.index, state.buf, msg)
		if err != nil {
			return err
		}
		state.downloaded += n
		state.backlog--
	}
	return nil
}

func (t *Torrent) calculateBoundsForPiece(index int) (begin, end int) {
	begin = index * t.PieceLength
	end = begin + t.PieceLength
	if end > t.Length {
		end = t.Length
	}

	return begin, end
}

func (t *Torrent) calculatePieceSize(index int) int {
	begin, end := t.calculateBoundsForPiece(index)
	return end - begin
}

func (t *Torrent) Download() ([]byte, error) {
	log.Printf("starting download for %v", t.Name)

	workQueue := make(chan *pieceWork, len(t.PieceHashes))
	results := make(chan *pieceResult)
	for index, hash := range t.PieceHashes {
		length := t.calculatePieceSize(index)
		workQueue <- &pieceWork{index, hash, length}
	}

	producerWg, consumerWg := sync.WaitGroup{}, sync.WaitGroup{}

	// produce the work queue
	for _, peer := range t.Peers {
		producerWg.Add(1)
		// Start a goroutine for each peer
		go func(peer peers.Peer) {
			defer producerWg.Done()
			log.Printf("Starting download worker for %s\n", peer.IP)
			// Start the download worker
			t.startDownloadWorker(peer, workQueue, results)
		}(peer)
	}

	buf := make([]byte, t.Length)
	donePieces := 0
	consumerWg.Add(1)
	// consume the results
	go func() {
		defer consumerWg.Done()

		for res := range results {
			log.Printf("Received piece %d from peer\n", res.index)
			res := <-results
			begin, end := t.calculateBoundsForPiece(res.index)

			// only one goroutine can write to the buffer at a time
			// this is a bit of a hack, but it works
			copy(buf[begin:end], res.buf)
			donePieces++

			percent := float64(donePieces) / float64(len(t.PieceHashes)) * 100
			numWorkers := runtime.NumGoroutine() - 1
			log.Printf("(%0.2f%%) Downloaded piece #%d from %d peers\n", percent, res.index, numWorkers)

			if donePieces == len(t.PieceHashes) {
				close(workQueue)
				log.Printf("All pieces downloaded. Exiting...\n")
				break
			}
		}
	}()

	go func() {
		producerWg.Wait()
		close(results)
	}()

	// Wait for all workers to finish
	consumerWg.Wait()
	if donePieces != len(t.PieceHashes) {
		log.Printf("Not all pieces were downloaded. Expected %d, got %d\n", len(t.PieceHashes), donePieces)
		return nil, fmt.Errorf("not all pieces were downloaded. Expected %d, got %d", len(t.PieceHashes), donePieces)
	}
	return buf, nil
}

func (t *Torrent) startDownloadWorker(peer peers.Peer, workQueue chan *pieceWork, results chan *pieceResult) {
	c, err := client.New(peer, t.PeerID, t.InfoHash)
	if err != nil {
		log.Printf("Could not handshake with %s. Disconnecting\n", peer.IP)
		return
	}
	defer c.Conn.Close()
	log.Printf("Completed handshake with %s\n", peer.IP)

	err = c.SendUnchoke()
	if err != nil {
		log.Printf("Could not send unchoke to %s. Disconnecting\n", peer.IP)
		return
	}

	err = c.SendInterested()
	if err != nil {
		log.Printf("Could not send interested to %s. Disconnecting\n", peer.IP)
		return
	}

	for pw := range workQueue {
		if !c.Bitfield.HasPiece(pw.index) {
			workQueue <- pw
			continue
		}

		buf, err := attemptDownPiece(c, pw)
		if err != nil {
			log.Printf("exiting %v", err)
			workQueue <- pw
			return
		}

		err = checkIntegrity(pw, buf)
		if err != nil {
			log.Printf("Piece %v failed integrity check", pw.index)
			workQueue <- pw
			continue
		}

		c.SendHave(pw.index)
		results <- &pieceResult{pw.index, buf}
	}
}

func attemptDownPiece(c *client.Client, pw *pieceWork) ([]byte, error) {
	state := pieceProgress{
		index:  pw.index,
		client: c,
		buf:    make([]byte, pw.length),
	}

	c.Conn.SetDeadline(time.Now().Add(30 * time.Second))
	defer c.Conn.SetDeadline(time.Time{})

	for state.downloaded < pw.length {
		if !state.client.Choked {
			if state.backlog < MaxBacklog && state.requested < pw.length {
				blockSize := MaxBlockSize
				if pw.length-state.requested < blockSize {
					blockSize = pw.length - state.requested
				}

				err := c.SendRequest(pw.index, state.requested, blockSize)
				if err != nil {
					return nil, err
				}

				state.backlog++
				state.requested += blockSize
			}
		}

		err := state.readMessage()
		if err != nil {
			return nil, err
		}
	}
	return state.buf, nil
}

func checkIntegrity(pw *pieceWork, buf []byte) error {
	hash := sha1.Sum(buf)
	if !bytes.Equal(hash[:], pw.hash[:]) {
		return fmt.Errorf("Index %v failed integrity check", pw.index)
	}

	return nil
}
