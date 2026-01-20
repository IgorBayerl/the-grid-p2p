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
	IsRunning     bool
	AmIHost       bool
	PlayerH       protocol.PlayerPosition
	PlayerC       protocol.PlayerPosition
	LastHeartbeat time.Time
}

func NewEngine(net p2p.NetworkNode) *Engine {
	return &Engine{
		Net:     net,
		PlayerH: protocol.PlayerPosition{X: 5, Y: 5, C: "@"},
		PlayerC: protocol.PlayerPosition{X: 35, Y: 10, C: "X"},
	}
}

// Tick is called by the UI loop every X milliseconds
func (g *Engine) Tick() {
	// Send heartbeat regardless of Role (Host/Client)
	// This ensures both sides can calculate Ping.
	if g.IsRunning {
		g.Net.Send(protocol.PacketHeartbeat, nil)
	}
}

// MoveLocal handles arrow key inputs
func (g *Engine) MoveLocal(dx, dy int) {
	if !g.IsRunning {
		return
	}

	if g.AmIHost {
		// Host moves immediately, then broadcasts
		g.PlayerH.X += dx
		g.PlayerH.Y += dy
		g.broadcastState()
	} else {
		// Client sends request to Host
		g.Net.Send(protocol.PacketInput, protocol.InputPacket{DX: dx, DY: dy})
	}
}

// ProcessPacket handles incoming network data
func (g *Engine) ProcessPacket(msg protocol.NetMessage) {
	// Heartbeat check (common to both)
	if msg.Type == protocol.PacketHeartbeat {
		g.LastHeartbeat = time.Now()
		return
	}

	if g.AmIHost {
		// HOST LOGIC: Process inputs from client
		if msg.Type == protocol.PacketInput {
			var input protocol.InputPacket
			if err := json.Unmarshal(msg.Data, &input); err == nil {
				g.PlayerC.X += input.DX
				g.PlayerC.Y += input.DY
				g.broadcastState() // Authoritative sync
			}
		}
	} else {
		// CLIENT LOGIC: Update world based on Host
		if msg.Type == protocol.PacketState {
			var state protocol.StatePacket
			if err := json.Unmarshal(msg.Data, &state); err == nil {
				g.PlayerH = state.Players["HOST"]
				g.PlayerC = state.Players["CLIENT"]
			}
		}
	}
}

// --- Session Management ---

// UserInitiateStart is called when the user presses Enter in the UI
func (g *Engine) UserInitiateStart() string {
	remote := g.Net.GetRemotePeer()
	if remote == "" {
		return "No peer connected."
	}

	// Calculate roles immediately
	myID := g.Net.ID().String()
	remoteID := remote.String()
	g.AmIHost = myID < remoteID

	if g.AmIHost {
		// I am Host: I must open the stream.
		// The UI will switch only when the stream event confirms success.
		g.Net.StartStream(g.Net.GetRemotePeer())
		return "Dialing..."
	} else {
		// I am Client: I must wait.
		return "Waiting for Host to start..."
	}
}

// OnGameStarted is called when the Network Layer confirms the stream is open
func (g *Engine) OnGameStarted() {
	g.IsRunning = true
	g.LastHeartbeat = time.Now()

	// Re-confirm roles
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
