package torrentfile

import (
	"bytes"
	"crypto/sha1"
	"os"

	"github.com/jackpal/bencode-go"
)

type bencodeInfo struct {
	Pieces      string `bencode:"pieces"`
	PieceLength int    `bencode:"piece length"`
	Length      int    `bencode:"length"`
	Name        string `bencode:"name"`
}

type bencodeTorrent struct {
	Announce string      `bencode:"announce"`
	Info     bencodeInfo `bencode:"info"`
}

func (b *bencodeInfo) hash() ([20]byte, error) {
	var buf bytes.Buffer
	err := bencode.Marshal(&buf, *b)
	if err != nil {
		return [20]byte{}, err
	}

	h := sha1.Sum(buf.Bytes())
	// Placeholder for actual hashing logic
	return h, nil
}

func (b *bencodeInfo) splitPieceHashes() ([][20]byte, error) {
	hashLen := 20
	buf := []byte(b.Pieces)
	if len(buf)%hashLen != 0 {
		return nil, os.ErrInvalid
	}
	pieceCount := len(buf) / hashLen
	pieceHashes := make([][20]byte, pieceCount)
	for i := 0; i < pieceCount; i++ {
		copy(pieceHashes[i][:], buf[i*hashLen:(i+1)*hashLen])
	}
	return pieceHashes, nil
}

func (b *bencodeTorrent) toTorrentFile() (*TorrentFile, error) {
	infoHash, err := b.Info.hash()
	if err != nil {
		return nil, err
	}
	pieceHashes, err := b.Info.splitPieceHashes()
	if err != nil {
		return nil, err
	}
	t := TorrentFile{
		Announce:    b.Announce,
		InfoHash:    infoHash,
		PieceHashes: pieceHashes,
		PieceLength: b.Info.PieceLength,
		Length:      b.Info.Length,
		Name:        b.Info.Name,
	}
	return &t, nil
}

type bencodeTrackerResp struct {
	Interval int    `bencode:"interval"`
	Peers    string `bencode:"peers"`
}
