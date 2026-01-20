package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/IgorBayerl/the-grid/internal/game"
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
	Game   *game.Engine
	Screen Screen

	// UI Local State
	LobbyCodeInput string
	BootProgress   float64
	SwarmPeers     string
	DebugLog       []string

	DHTReady    bool
	FoundPeerID string
	IsDirect    bool
}

func NewModel(g *game.Engine) Model {
	return Model{
		Game:       g,
		Screen:     ScreenBoot,
		DebugLog:   []string{"Initializing UI..."},
		SwarmPeers: "0",
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tickBoot(),
		waitForNetworkEvent(m.Game.Net),
		tickGame(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	// --- 1. User Input Handling ---
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}

		switch m.Screen {
		case ScreenLobby:
			switch msg.Type {
			case tea.KeyBackspace, tea.KeyDelete:
				if len(m.LobbyCodeInput) > 0 {
					m.LobbyCodeInput = m.LobbyCodeInput[:len(m.LobbyCodeInput)-1]
				}
			case tea.KeyEnter:
				if len(m.LobbyCodeInput) >= 3 {
					m.Screen = ScreenAirlock
					m.Game.Net.JoinLobby(m.LobbyCodeInput)
					return m, nil
				}
			case tea.KeyRunes:
				if len(m.LobbyCodeInput) < 10 {
					m.LobbyCodeInput += msg.String()
				}
			}

		case ScreenAirlock:
			if msg.Type == tea.KeyEnter && m.IsDirect {
				status := m.Game.UserInitiateStart()
				m.DebugLog = append(m.DebugLog, status)
			}

		case ScreenGame:
			switch msg.String() {
			case "up":
				m.Game.MoveLocal(0, -1)
			case "down":
				m.Game.MoveLocal(0, 1)
			case "left":
				m.Game.MoveLocal(-1, 0)
			case "right":
				m.Game.MoveLocal(1, 0)
			}
		}

	// --- 2. Animations ---
	case TickBootMsg:
		if m.BootProgress < 1.0 {
			m.BootProgress += 0.05
			return m, tickBoot()
		}
		go m.Game.Net.Bootstrap()

	// --- 3. Network Events ---
	case protocol.StatusEvent:
		switch msg.Key {
		case "SWARM":
			m.SwarmPeers = msg.Val
		case "DHT":
			if msg.Val == "DONE" {
				m.DHTReady = true
				m.Screen = ScreenLobby
			}
		case "PEERS":
			m.FoundPeerID = msg.Val
		case "HOLEPUNCH":
			m.IsDirect = true
		case "GAME":
			switch msg.Val {
			case "START":
				m.Screen = ScreenGame
				m.Game.OnGameStarted()
				m.DebugLog = append(m.DebugLog, fmt.Sprintf("GAME STARTED. Host=%v", m.Game.AmIHost))

				// FIX: Must restart the network listener, otherwise we stop receiving packets!
				return m, tea.Batch(tea.ClearScreen, waitForNetworkEvent(m.Game.Net))

			case "STOP":
				m.Screen = ScreenAirlock
				m.DebugLog = append(m.DebugLog, "PEER DISCONNECTED")
				m.Game.IsRunning = false

				// FIX: Restart listener here too
				return m, tea.Batch(tea.ClearScreen, waitForNetworkEvent(m.Game.Net))
			}
		}
		return m, waitForNetworkEvent(m.Game.Net)

	case protocol.LogEvent:
		m.DebugLog = append(m.DebugLog, fmt.Sprintf("> %s", string(msg)))
		if len(m.DebugLog) > 8 {
			m.DebugLog = m.DebugLog[1:]
		}
		return m, waitForNetworkEvent(m.Game.Net)

	case protocol.NetMessage:
		m.Game.ProcessPacket(msg)
		return m, waitForNetworkEvent(m.Game.Net)

	case TickGameMsg:
		m.Game.Tick()
		return m, tickGame()
	}

	return m, nil
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

// --- Render Helpers ---

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

	draw(m.Game.PlayerH, styleHost)
	draw(m.Game.PlayerC, styleClient)

	var s strings.Builder
	role := "CLIENT (You are Red X)"
	if m.Game.AmIHost {
		role = "HOST (You are Green @)"
	}

	// --- NEW PING VISUALIZATION ---
	var hb string

	// Check if the connection is actually stale (no packets for > 2 seconds)
	timeSinceLastPacket := time.Since(m.Game.LastPacketTime)

	if timeSinceLastPacket > 2*time.Second {
		// Connection Lost
		hb = styleLog.Render(fmt.Sprintf("💔 DISCONNECTED (%s)", timeSinceLastPacket.Round(time.Second)))
	} else {
		// Healthy Connection: Show smoothed Latency
		latencyMs := m.Game.Latency.Milliseconds()

		// Color coding based on lag
		pingStyle := styleHost // Green
		if latencyMs > 100 {
			pingStyle = styleClient // Red
		}

		hb = pingStyle.Render(fmt.Sprintf("📶 %d ms", latencyMs))
	}
	// ------------------------------

	s.WriteString(fmt.Sprintf("\n  THE GRID | %s | %s\n", role, hb))
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

	// Role hint logic
	roleHint := "(Searching...)"
	if m.Game.Net.GetRemotePeer() != "" {
		myID := m.Game.Net.ID().String()
		remoteID := m.Game.Net.GetRemotePeer().String()
		if myID < remoteID {
			roleHint = "(You will be HOST)"
		} else {
			roleHint = "(You will be CLIENT)"
		}
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

	// Safe type assertion for display purposes only
	room := "???"
	if n, ok := m.Game.Net.(*p2p.Node); ok {
		room = n.CurrentRoom
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
	`, room, peerDisp, symPubSub, symRelay, symHolePunch, status, roleHint, instr)
}

type TickBootMsg time.Time
type TickGameMsg time.Time

func tickBoot() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg { return TickBootMsg(t) })
}

func tickGame() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg { return TickGameMsg(t) })
}

func waitForNetworkEvent(n interface{ Events() <-chan interface{} }) tea.Cmd {
	return func() tea.Msg {
		return <-n.Events()
	}
}
