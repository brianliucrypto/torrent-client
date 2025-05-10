package main

import (
	"log"
	"os"

	"github.com/brianliucrypto/torrent-client/torrentfile" // Example package for torrent handling
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("Usage: torrentfile <input.torrent> <output.file>")
	}

	inPath := os.Args[1]
	outPath := os.Args[2]
	torrentFile, err := torrentfile.Open(inPath)
	if err != nil {
		log.Fatal(err)
	}

	err = torrentFile.DownloadToFile(outPath)
	if err != nil {
		log.Fatal(err)
	}
}
