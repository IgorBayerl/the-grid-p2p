package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/IgorBayerl/the-grid/internal/p2p"
	"github.com/IgorBayerl/the-grid/internal/protocol"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var styleLog = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
var styleHost = lipgloss.NewStyle().Foreground(lipgloss.Color("120")).Bold(true)   // Green
var styleClient = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true) // Red
var styleHeart = lipgloss.NewStyle().Foreground(lipgloss.Color("211"))             // Pink

type Screen int

const (
	ScreenBoot Screen = iota
	ScreenLobby
	ScreenAirlock
	ScreenGame
)

type Model struct {
	Node   *p2p.Node
	Screen Screen

	LobbyCodeInput string
	BootProgress   float64
	SwarmPeers     string
	DebugLog       []string

	DHTReady    bool
	FoundPeerID string
	IsDirect    bool

	AmIHost       bool
	LastHeartbeat time.Time
	PlayerH       protocol.PlayerPosition
	PlayerC       protocol.PlayerPosition
}

func NewModel(node *p2p.Node) Model {
	return Model{
		Node:       node,
		Screen:     ScreenBoot,
		DebugLog:   []string{"Initializing UI..."},
		SwarmPeers: "0",
		PlayerH:    protocol.PlayerPosition{X: 5, Y: 5, C: "@"},
		PlayerC:    protocol.PlayerPosition{X: 35, Y: 10, C: "X"},
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tickBoot(),
		waitForStatus(m.Node),
		waitForPacket(m.Node),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}

		if m.Screen == ScreenLobby {
			switch msg.Type {
			case tea.KeyBackspace, tea.KeyDelete:
				if len(m.LobbyCodeInput) > 0 {
					m.LobbyCodeInput = m.LobbyCodeInput[:len(m.LobbyCodeInput)-1]
				}
			case tea.KeyEnter:
				if len(m.LobbyCodeInput) >= 3 {
					m.Screen = ScreenAirlock
					m.Node.JoinLobby(m.LobbyCodeInput)
					return m, nil
				}
			case tea.KeyRunes:
				if len(m.LobbyCodeInput) < 10 {
					m.LobbyCodeInput += msg.String()
				}
			}
		} else if m.Screen == ScreenAirlock {
			if msg.Type == tea.KeyEnter && m.IsDirect {
				// --- Role Calculation ---
				amIHost := protocol.AmIHost(m.Node.Host.ID(), m.Node.RemotePeer)
				if amIHost {
					m.DebugLog = append(m.DebugLog, "I am Host. Dialing...")
					m.Node.StartGame()
				} else {
					m.DebugLog = append(m.DebugLog, "I am Client. Waiting for Host...")
				}
			}
		} else if m.Screen == ScreenGame {
			dx, dy := 0, 0
			switch msg.String() {
			case "up":
				dy = -1
			case "down":
				dy = 1
			case "left":
				dx = -1
			case "right":
				dx = 1
			}

			if dx != 0 || dy != 0 {
				if m.AmIHost {
					m.PlayerH.X += dx
					m.PlayerH.Y += dy
					m.broadcastState()
					m.DebugLog = append(m.DebugLog, "Host Moved -> Broadcast")
				} else {
					m.Node.SendPacket(protocol.PacketInput, protocol.InputPacket{DX: dx, DY: dy})
					m.DebugLog = append(m.DebugLog, "Client Input -> Sent")
				}
			}
		}

	case TickBootMsg:
		if m.BootProgress < 1.0 {
			m.BootProgress += 0.05
			return m, tickBoot()
		}
		go m.Node.Bootstrap()

	case protocol.StatusUpdate:
		switch msg.Key {
		case "LOG":
			m.DebugLog = append(m.DebugLog, fmt.Sprintf("> %s", msg.Value))
			if len(m.DebugLog) > 8 {
				m.DebugLog = m.DebugLog[1:]
			}
		case "SWARM":
			m.SwarmPeers = msg.Value
		case "DHT":
			if msg.Value == "DONE" {
				m.DHTReady = true
				m.Screen = ScreenLobby
			}
		case "PEERS":
			m.FoundPeerID = msg.Value
		case "HOLEPUNCH":
			m.IsDirect = true

		case "GAME":
			if msg.Value == "START" {
				m.Screen = ScreenGame
				m.AmIHost = protocol.AmIHost(m.Node.Host.ID(), m.Node.RemotePeer)
				m.DebugLog = append(m.DebugLog, fmt.Sprintf("GAME STARTED. Role: Host=%v", m.AmIHost))

				if m.AmIHost {
					m.broadcastState()
				}
				return m, tea.ClearScreen
			} else if msg.Value == "STOP" {
				m.Screen = ScreenAirlock
				m.DebugLog = append(m.DebugLog, "PEER DISCONNECTED")
				return m, tea.ClearScreen
			}
		}
		return m, waitForStatus(m.Node)

	case protocol.NetMessage:
		handled := false

		// Heartbeats are common to both roles
		if msg.Type == protocol.PacketHeartbeat {
			m.LastHeartbeat = time.Now()
			handled = true
		}

		if m.AmIHost && msg.Type == protocol.PacketInput {
			var input protocol.InputPacket
			if err := json.Unmarshal(msg.Data, &input); err == nil {
				m.PlayerC.X += input.DX
				m.PlayerC.Y += input.DY
				m.broadcastState()
				m.DebugLog = append(m.DebugLog, "Rx Input -> Update")
				handled = true
			}
		} else if !m.AmIHost && msg.Type == protocol.PacketState {
			var state protocol.StatePacket
			if err := json.Unmarshal(msg.Data, &state); err == nil {
				m.PlayerH = state.Players["HOST"]
				m.PlayerC = state.Players["CLIENT"]
				handled = true
			}
		}

		if !handled {
			// This catches the silent failures (e.g. Host receiving StatePacket, Client receiving Input, or parsing errors)
			m.DebugLog = append(m.DebugLog, fmt.Sprintf("Ignored Packet: T=%d Host=%v", msg.Type, m.AmIHost))
		}

		return m, waitForPacket(m.Node)
	}

	return m, nil
}

func (m *Model) broadcastState() {
	state := protocol.StatePacket{
		Players: map[string]protocol.PlayerPosition{
			"HOST":   m.PlayerH,
			"CLIENT": m.PlayerC,
		},
	}
	m.Node.SendPacket(protocol.PacketState, state)
}

func (m Model) View() string {
	if m.Screen == ScreenGame {
		return renderGame(m)
	}

	renderLog := func() string {
		visibleLog := m.DebugLog
		if len(visibleLog) > 6 {
			visibleLog = visibleLog[len(visibleLog)-6:]
		}
		return "\n" + styleLog.Render("\n[ SYSTEM LOG ]\n"+strings.Join(visibleLog, "\n"))
	}

	switch m.Screen {
	case ScreenBoot:
		return renderBoot(m) + renderLog()
	case ScreenLobby:
		return renderLobby(m) + renderLog()
	case ScreenAirlock:
		return renderAirlock(m) + renderLog()
	}
	return ""
}

func renderGame(m Model) string {
	width, height := 40, 15
	grid := make([][]string, height)
	for y := 0; y < height; y++ {
		grid[y] = make([]string, width)
		for x := 0; x < width; x++ {
			grid[y][x] = "."
		}
	}

	draw := func(p protocol.PlayerPosition, style lipgloss.Style) {
		if p.X >= 0 && p.X < width && p.Y >= 0 && p.Y < height {
			grid[p.Y][p.X] = style.Render(p.C)
		}
	}

	draw(m.PlayerH, styleHost)
	draw(m.PlayerC, styleClient)

	var s strings.Builder
	role := "CLIENT (You are Red X)"
	if m.AmIHost {
		role = "HOST (You are Green @)"
	}

	// Heartbeat Visual
	hb := "DEAD"
	diff := time.Since(m.LastHeartbeat)
	if diff < 2*time.Second {
		hb = styleHeart.Render("❤️  " + diff.Round(time.Millisecond).String())
	} else {
		hb = styleLog.Render("💔 " + diff.Round(time.Second).String())
	}

	s.WriteString(fmt.Sprintf("\n  THE GRID | %s | Ping: %s\n", role, hb))
	s.WriteString("  ┌" + strings.Repeat("─", width*2) + "┐\n")
	for _, row := range grid {
		s.WriteString("  │")
		for _, cell := range row {
			s.WriteString(cell + " ")
		}
		s.WriteString("│\n")
	}
	s.WriteString("  └" + strings.Repeat("─", width*2) + "┘\n")
	s.WriteString("  [ARROWS] Move  [Q] Quit\n")

	visibleLog := m.DebugLog
	if len(visibleLog) > 4 {
		visibleLog = visibleLog[len(visibleLog)-4:]
	}
	s.WriteString(styleLog.Render("\n[ GAME LOG ]\n" + strings.Join(visibleLog, "\n")))

	return s.String()
}

func renderBoot(m Model) string {
	barFill := int(m.BootProgress * 20)
	bar := strings.Repeat("█", barFill) + strings.Repeat("░", 20-barFill)
	dhtStat := "..."
	if m.DHTReady {
		dhtStat = "[ OK ]"
	}
	return fmt.Sprintf("\n  [ SYSTEM INITIALIZATION ]\n  > Crypto Module ........... [ OK ]\n  > Global Swarm Uplink ..... %s\n    (Peers: %s)\n\n  LOADING: [%s]\n", dhtStat, m.SwarmPeers, bar)
}

func renderLobby(m Model) string {
	return fmt.Sprintf("\n  ⚡ THE GRID PROTOCOL\n\n  ENTER ROOM CODE:\n  > %s_\n\n  [ INFO ]\n  Connected to %s Swarm Nodes.\n  Waiting for input...\n", m.LobbyCodeInput, m.SwarmPeers)
}

func renderAirlock(m Model) string {
	instr := ""
	if m.IsDirect {
		instr = "\n  [ PRESS ENTER TO START GAME ]\n"
	}
	// Also show role hint
	amIHost := protocol.AmIHost(m.Node.Host.ID(), m.Node.RemotePeer)
	roleHint := "(You will be CLIENT)"
	if amIHost {
		roleHint = "(You will be HOST)"
	}
	if m.Node.RemotePeer == "" {
		roleHint = "(Searching...)"
	}

	symPubSub := "[ .. ]"
	if m.FoundPeerID != "" {
		symPubSub = "[ OK ]"
	}
	symRelay := "[ .. ]"
	if m.FoundPeerID != "" || m.IsDirect {
		symRelay = "[ OK ]"
	}
	symHolePunch := "[ .. ]"
	status := "🟡 WAITING..."

	if m.FoundPeerID != "" {
		status = "🟡 NEGOTIATING..."
	}
	if m.IsDirect {
		symHolePunch = "[ OK ]"
		status = "🟢 DIRECT LINK ESTABLISHED"
	}

	peerDisp := "..."
	if len(m.FoundPeerID) > 6 {
		peerDisp = m.FoundPeerID[:6] + "..."
	}

	return fmt.Sprintf(`
  🔒 ACCESSING ROOM: %s
	
  PEER: %s
	
  [ CONNECTION LOG ]
  > Global Beacon Scan ...... %s
  > Handshake ............... %s
  > NAT Traversal ........... %s
	
  STATUS: %s
  %s %s
	`, m.Node.CurrentRoom, peerDisp, symPubSub, symRelay, symHolePunch, status, roleHint, instr)
}

type TickBootMsg time.Time

func tickBoot() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg { return TickBootMsg(t) })
}
func waitForStatus(n *p2p.Node) tea.Cmd { return func() tea.Msg { return <-n.StatusChan } }
func waitForPacket(n *p2p.Node) tea.Cmd { return func() tea.Msg { return <-n.MsgIn } }
