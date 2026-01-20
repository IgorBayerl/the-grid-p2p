package protocol

import "github.com/libp2p/go-libp2p/core/peer"

const (
	ProtocolID     = "/the-grid/1.0.0"
	DiscoveryTopic = "the-grid-discovery-v1"
	GameProtocol   = "/the-grid/game/1.0.0"

	PacketInput = 0x01
	PacketState = 0x02
	PacketPing  = 0x10
	PacketPong  = 0x11
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

// --- Ping Data ---

type PingPayload struct {
	OriginTime int64 `json:"t"` // UnixNano timestamp
}

// --- Discovery Data (Crucial for P2P) ---

type DiscoveryPacket struct {
	RoomCode string   `json:"room_code"`
	PeerID   string   `json:"peer_id"`
	Addrs    []string `json:"addrs"`
}

// --- Internal Messaging & Events ---

type NetMessage struct {
	Sender peer.ID `json:"-"`
	Type   byte    `json:"type"`
	Data   []byte  `json:"data"`
}

type LogEvent string

type StatusEvent struct {
	Key string
	Val string
}
