package protocol

import "github.com/libp2p/go-libp2p/core/peer"

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
	Players map[string]PlayerPosition `json:"players"`
}

type PlayerPosition struct {
	X int    `json:"x"`
	Y int    `json:"y"`
	C string `json:"c"`
}

// --- Discovery Data ---

// DiscoveryPacket is used to broadcast presence on the DHT gossipsub topic
type DiscoveryPacket struct {
	RoomCode string   `json:"room_code"`
	PeerID   string   `json:"peer_id"`
	Addrs    []string `json:"addrs"`
}

// --- Internal Messaging & Events ---

// NetMessage wraps the raw bytes received from the stream
type NetMessage struct {
	Sender peer.ID `json:"-"`
	Type   byte    `json:"type"`
	Data   []byte  `json:"data"`
}

// LogEvent is a simple string alias for UI logging
type LogEvent string

// StatusEvent is used to update UI indicators (like connectivity icons)
type StatusEvent struct {
	Key string
	Val string
}
