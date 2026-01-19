package protocol

import (
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	ProtocolID     = "/the-grid/1.0.0"
	DiscoveryTopic = "the-grid-discovery-v1"
	GameProtocol   = "/the-grid/game/1.0.0"

	PacketInput     = 0x01
	PacketState     = 0x02
	PacketHeartbeat = 0x99
)

// --- Game Data ---

type InputPacket struct {
	DX int `json:"dx"`
	DY int `json:"dy"`
}

type StatePacket struct {
	// Keys will be "HOST" and "CLIENT"
	Players map[string]PlayerPosition `json:"players"`
}

type PlayerPosition struct {
	X int    `json:"x"`
	Y int    `json:"y"`
	C string `json:"c"` // Character
}

// --- Internal Messaging ---

type NetMessage struct {
	// FIX: We must ignore Sender in JSON, otherwise empty string fails validation on RX
	Sender peer.ID `json:"-"`
	Type   byte    `json:"type"`
	Data   []byte  `json:"data"`
}

type StatusUpdate struct {
	Key   string
	Value string
}

type DiscoveryPacket struct {
	RoomCode string
	PeerID   string
	Addrs    []string
}

// Logic Helper
func AmIHost(selfID, peerID peer.ID) bool {
	// String comparison ensures deterministic authority
	return selfID.String() < peerID.String()
}
