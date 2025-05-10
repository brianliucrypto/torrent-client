package peers

import (
	"net"
	"strconv"
)

type Peer struct {
	IP   net.IP
	Port uint16
}

func Unmarshal(peersBin []byte) ([]Peer, error) {
	peers := make([]Peer, len(peersBin)/6)
	for i := 0; i < len(peersBin); i += 6 {
		ip := net.IP(peersBin[i : i+4])
		port := uint16(peersBin[i+4])<<8 | uint16(peersBin[i+5])
		peers[i/6] = Peer{IP: ip, Port: port}
	}
	return peers, nil
}

func (p Peer) String() string {
	return p.IP.String() + ":" + strconv.Itoa(int(p.Port))
}
