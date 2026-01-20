package protocol

import "github.com/libp2p/go-libp2p/core/peer"

const (
	ProtocolID   = "/the-grid/1.0.0"
	GameProtocol = "/the-grid/game/1.0.0"

	PacketInput = 0x01
	PacketState = 0x02
	PacketPing  = 0x10
	PacketPong  = 0x11
)

// --- Game Structures ---

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

type PingPayload struct {
	OriginTime int64 `json:"t"`
}

// --- Internal Event Types ---

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
