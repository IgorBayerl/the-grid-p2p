package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/IgorBayerl/the-grid/internal/p2p"
	"github.com/IgorBayerl/the-grid/internal/protocol"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	renderLog := func() string {
		visibleLog := m.DebugLog
		if len(visibleLog) > 7 {
			visibleLog = visibleLog[len(visibleLog)-7:]
		}
		return "\n" + styleLog.Render("\n[ SYSTEM LOG ]\n"+strings.Join(visibleLog, "\n"))
	}

	var content string
	switch m.Screen {
	case ScreenBoot:
		content = renderBoot(m)
	case ScreenLobby:
		content = renderLobby(m)
	case ScreenAirlock:
		content = renderAirlock(m)
	case ScreenCountdown:
		content = renderCountdown(m)
	case ScreenGame:
		return renderGame(m) // Game renders its own log
	}
	return content + renderLog()
}

// --- Renderers ---

func renderGame(m Model) string {
	width, height := 25, 15
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
	role := "CLIENT (Red X)"
	if m.Game.AmIHost {
		role = "HOST (Green @)"
	}

	var hb string
	timeSinceLastPacket := time.Since(m.Game.LastPacketTime)

	if timeSinceLastPacket > 2*time.Second {
		hb = styleLog.Render(fmt.Sprintf("[!] LOST (%s)", timeSinceLastPacket.Round(time.Second)))
	} else {
		latencyMs := m.Game.Latency.Milliseconds()
		pingStyle := styleHost
		if latencyMs > 100 {
			pingStyle = styleClient
		}
		hb = pingStyle.Render(fmt.Sprintf("PING: %d ms", latencyMs))
	}

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
	s.WriteString("  [ARROWS] Move  [Q] Quit to Lobby\n")

	visibleLog := m.DebugLog
	if len(visibleLog) > 7 {
		visibleLog = visibleLog[len(visibleLog)-7:]
	}
	s.WriteString(styleLog.Render("\n[ GAME LOG ]\n" + strings.Join(visibleLog, "\n")))

	return s.String()
}

func renderBoot(m Model) string {
	spinChars := []string{"|", "/", "-", "\\"}
	spinner := spinChars[m.SpinnerIdx%len(spinChars)]

	dhtStat := "..."
	if m.DHTReady {
		dhtStat = "[ OK ]"
	}
	return fmt.Sprintf("\n  [ SYSTEM INITIALIZATION ]\n  > Crypto Module ........... [ OK ]\n  > Global Swarm Uplink ..... %s\n    (Peers: %s)\n\n  CONNECTING %s\n", dhtStat, m.SwarmPeers, spinner)
}

func renderLobby(m Model) string {
	return fmt.Sprintf("\n  THE GRID PROTOCOL\n\n  ENTER ROOM CODE:\n  > %s_\n\n  [ INFO ]\n  Connected to %s Swarm Nodes.\n  Waiting for input...\n", m.LobbyCodeInput, m.SwarmPeers)
}

func renderAirlock(m Model) string {
	spinChars := []string{"|", "/", "-", "\\"}
	spinner := spinChars[m.SpinnerIdx%len(spinChars)]

	// Determine States
	stateDiscovery := stylePending.Render("[ SEARCHING ]")
	if m.FoundPeerID != "" {
		stateDiscovery = styleSuccess.Render("[ FOUND ]    ")
	}

	stateHandshake := styleDim.Render("[ WAITING ]  ")
	if m.FoundPeerID != "" {
		stateHandshake = stylePending.Render("[ WORKING ]  ")
	}
	if m.IsDirect || (m.FoundPeerID != "" && m.Game.Net.IsConnected()) {
		stateHandshake = styleSuccess.Render("[ CONNECTED ]")
	}

	statePunch := styleDim.Render("[ PENDING ]  ")
	if m.FoundPeerID != "" && !m.IsDirect {
		statePunch = stylePending.Render("[ WORKING ]  ")
	}
	if m.IsDirect {
		statePunch = styleSuccess.Render("[ DIRECT ]   ")
	}

	room := "???"
	if n, ok := m.Game.Net.(*p2p.Node); ok {
		room = n.CurrentRoom
	}

	// Timer Formatting
	timerColor := styleHost // Default Green
	if m.SearchTimeout < 30 {
		timerColor = styleClient // Red if time is running out
	}
	timerStr := timerColor.Render(fmt.Sprintf("%ds", m.SearchTimeout))

	return fmt.Sprintf(`
  CONNECTION IN PROGRESS
  Target: ROOM %s  |  Timeout: %s
  %s Scanning Network...

  > Discovery   %s
  > Handshake   %s
  > Direct Link %s

  (Hold 'Q' to Cancel)
	`, room, timerStr, spinner, stateDiscovery, stateHandshake, statePunch)
}

func renderCountdown(m Model) string {
	return fmt.Sprintf("\n\n\n      MATCH STARTING IN\n\n            %d\n\n\n", m.Countdown)
}
