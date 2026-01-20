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

	// Latency Tracking
	Latency        time.Duration
	LastPacketTime time.Time
}

func NewEngine(net p2p.NetworkNode) *Engine {
	e := &Engine{
		Net: net,
	}
	e.Reset()
	return e
}

func (g *Engine) Reset() {
	g.IsRunning = false
	g.Latency = 0
	// Initialize default positions
	g.PlayerH = protocol.PlayerPosition{X: 2, Y: 2, C: "@"}
	g.PlayerC = protocol.PlayerPosition{X: 23, Y: 12, C: "X"}
}

// Tick executes periodic logic (heartbeats)
func (g *Engine) Tick() {
	if g.IsRunning {
		payload := protocol.PingPayload{OriginTime: time.Now().UnixNano()}
		g.Net.Send(protocol.PacketPing, payload)
	}
}

// MoveLocal handles local input inputs
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

// ProcessPacket handles incoming network messages
func (g *Engine) ProcessPacket(msg protocol.NetMessage) {
	g.LastPacketTime = time.Now()

	switch msg.Type {

	case protocol.PacketPing:
		var p protocol.PingPayload
		if err := json.Unmarshal(msg.Data, &p); err == nil {
			g.Net.Send(protocol.PacketPong, p)
		}

	case protocol.PacketPong:
		var p protocol.PingPayload
		if err := json.Unmarshal(msg.Data, &p); err == nil {
			origin := time.Unix(0, p.OriginTime)
			g.updateLatency(time.Since(origin))
		}

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

func (g *Engine) updateLatency(newRtt time.Duration) {
	if g.Latency == 0 {
		g.Latency = newRtt
		return
	}
	// Exponential Moving Average
	g.Latency = time.Duration(float64(g.Latency)*0.8 + float64(newRtt)*0.2)
}

// DetermineRole assigns Host/Client based on Peer ID
func (g *Engine) DetermineRole() string {
	remote := g.Net.GetRemotePeer()
	if remote == "" {
		return "No peer connected."
	}
	myID := g.Net.ID().String()
	remoteID := remote.String()
	g.AmIHost = myID < remoteID

	if g.AmIHost {
		g.Net.StartStream(g.Net.GetRemotePeer())
		return "Connecting Stream (Host)..."
	} else {
		return "Waiting for Stream (Client)..."
	}
}

func (g *Engine) OnGameStarted() {
	g.IsRunning = true
	g.LastPacketTime = time.Now()
	g.DetermineRole()

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
