package game

import (
	"encoding/json"
	"time"

	"github.com/IgorBayerl/the-grid/internal/p2p"
	"github.com/IgorBayerl/the-grid/internal/protocol"
)

type Engine struct {
	Net p2p.NetworkNode

	// Game State
	IsRunning bool
	AmIHost   bool
	PlayerH   protocol.PlayerPosition
	PlayerC   protocol.PlayerPosition

	// Ping / Latency Logic
	Latency        time.Duration // The smoothed Round Trip Time
	LastPacketTime time.Time     // Used to detect disconnects
}

func NewEngine(net p2p.NetworkNode) *Engine {
	return &Engine{
		Net:     net,
		PlayerH: protocol.PlayerPosition{X: 5, Y: 5, C: "@"},
		PlayerC: protocol.PlayerPosition{X: 35, Y: 10, C: "X"},
	}
}

// Tick is called by the UI loop every 100ms
func (g *Engine) Tick() {
	if g.IsRunning {
		// Send a PING with the current time
		payload := protocol.PingPayload{OriginTime: time.Now().UnixNano()}
		g.Net.Send(protocol.PacketPing, payload)
	}
}

// MoveLocal handles arrow key inputs
func (g *Engine) MoveLocal(dx, dy int) {
	if !g.IsRunning {
		return
	}

	if g.AmIHost {
		g.PlayerH.X += dx
		g.PlayerH.Y += dy
		g.broadcastState()
	} else {
		g.Net.Send(protocol.PacketInput, protocol.InputPacket{DX: dx, DY: dy})
	}
}

// ProcessPacket handles incoming network data
func (g *Engine) ProcessPacket(msg protocol.NetMessage) {
	// Always update LastPacketTime so we know the connection is alive
	g.LastPacketTime = time.Now()

	switch msg.Type {

	// Case 1: Received a Ping -> Immediately send back a Pong with the SAME timestamp
	case protocol.PacketPing:
		var p protocol.PingPayload
		if err := json.Unmarshal(msg.Data, &p); err == nil {
			g.Net.Send(protocol.PacketPong, p)
		}

	// Case 2: Received a Pong -> Calculate RTT
	case protocol.PacketPong:
		var p protocol.PingPayload
		if err := json.Unmarshal(msg.Data, &p); err == nil {
			origin := time.Unix(0, p.OriginTime)
			rtt := time.Since(origin)
			g.updateLatency(rtt)
		}

	// Game Logic
	case protocol.PacketInput:
		if g.AmIHost {
			var input protocol.InputPacket
			if err := json.Unmarshal(msg.Data, &input); err == nil {
				g.PlayerC.X += input.DX
				g.PlayerC.Y += input.DY
				g.broadcastState()
			}
		}

	case protocol.PacketState:
		if !g.AmIHost {
			var state protocol.StatePacket
			if err := json.Unmarshal(msg.Data, &state); err == nil {
				g.PlayerH = state.Players["HOST"]
				g.PlayerC = state.Players["CLIENT"]
			}
		}
	}
}

// Helper to smooth the Ping numbers so they don't jump around
func (g *Engine) updateLatency(newRtt time.Duration) {
	if g.Latency == 0 {
		g.Latency = newRtt
		return
	}
	// Simple Exponential Moving Average (Alpha = 0.2)
	// Keeps it stable but responsive
	g.Latency = time.Duration(float64(g.Latency)*0.8 + float64(newRtt)*0.2)
}

// --- Session Management ---

func (g *Engine) UserInitiateStart() string {
	remote := g.Net.GetRemotePeer()
	if remote == "" {
		return "No peer connected."
	}
	myID := g.Net.ID().String()
	remoteID := remote.String()
	g.AmIHost = myID < remoteID

	if g.AmIHost {
		g.Net.StartStream(g.Net.GetRemotePeer())
		return "Dialing..."
	} else {
		return "Waiting for Host to start..."
	}
}

func (g *Engine) OnGameStarted() {
	g.IsRunning = true
	g.LastPacketTime = time.Now() // Reset timer
	myID := g.Net.ID().String()
	remoteID := g.Net.GetRemotePeer().String()
	g.AmIHost = myID < remoteID

	if g.AmIHost {
		g.broadcastState()
	}
}

func (g *Engine) broadcastState() {
	state := protocol.StatePacket{
		Players: map[string]protocol.PlayerPosition{
			"HOST":   g.PlayerH,
			"CLIENT": g.PlayerC,
		},
	}
	g.Net.Send(protocol.PacketState, state)
}
