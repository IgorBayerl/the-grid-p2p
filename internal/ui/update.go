package ui

import (
	"fmt"

	"github.com/IgorBayerl/the-grid/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		return m.handleKeyInput(msg)

	case protocol.StatusEvent:
		return m.handleStatusEvent(msg)

	case protocol.LogEvent:
		return m.handleLogEvent(msg)

	case protocol.NetMessage:
		return m.handleNetMessage(msg)

	case TickSpinnerMsg:
		m.SpinnerIdx++
		return m, tickSpinner()

	case TickCountdownMsg:
		// Route the generic countdown ticker based on the current screen
		if m.Screen == ScreenAirlock {
			return m.handleSearchTick()
		} else if m.Screen == ScreenCountdown {
			return m.handleCountdownTick()
		}
		return m, nil

	case TickGameMsg:
		m.Game.Tick()
		return m, tickGame()
	}

	return m, nil
}

// --- Input Handlers ---

func (m Model) handleLobbyInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyBackspace, tea.KeyDelete:
		if len(m.LobbyCodeInput) > 0 {
			m.LobbyCodeInput = m.LobbyCodeInput[:len(m.LobbyCodeInput)-1]
		}
	case tea.KeyEnter:
		if len(m.LobbyCodeInput) >= 3 {
			m.Screen = ScreenAirlock
			m.SearchTimeout = 120 // Reset timer to 2 minutes
			m.Game.Net.JoinLobby(m.LobbyCodeInput)
			return m, tickCountdown() // Start the timer immediately
		}
	case tea.KeyRunes:
		// Limit code length to avoid UI breaking
		if len(m.LobbyCodeInput) < 10 {
			m.LobbyCodeInput += msg.String()
		}
	}
	return m, nil
}

func (m Model) handleSearchTick() (tea.Model, tea.Cmd) {
	if m.SearchTimeout > 0 {
		m.SearchTimeout--
		return m, tickCountdown()
	}

	// TIMEOUT REACHED
	m.DebugLog = append(m.DebugLog, "Search timed out. No peers found.")
	m.Game.Net.LeaveLobby() // Cancel DHT operations

	// Reset UI to Lobby
	m.Screen = ScreenLobby
	m.FoundPeerID = ""
	m.IsDirect = false

	// We do not clear m.LobbyCodeInput so the user can try again quickly

	// Force a clear screen to remove Airlock artifacts
	return m, tea.ClearScreen
}

func (m Model) handleKeyInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle Global Keys (Quit / Exit)
	if msg.String() == "ctrl+c" || msg.String() == "q" {
		return m.handleQuit()
	}

	// Handle Screen-Specific Keys
	switch m.Screen {
	case ScreenLobby:
		return m.handleLobbyInput(msg)
	case ScreenGame:
		return m.handleGameInput(msg)
	}

	return m, nil
}

func (m Model) handleQuit() (tea.Model, tea.Cmd) {
	// If we are connected (Game or Airlock), "Q" acts as "Leave Room" first
	if m.Screen == ScreenGame || m.Screen == ScreenAirlock {
		m.Game.Net.LeaveLobby()
		return m, nil // Wait for the "STOP" status event to actually reset UI
	}
	// Otherwise, kill the app
	return m, tea.Quit
}

func (m Model) handleGameInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	return m, nil
}

// --- Network Event Handlers ---

func (m Model) handleStatusEvent(msg protocol.StatusEvent) (tea.Model, tea.Cmd) {
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
		// Direct connection established! Start countdown.
		m.IsDirect = true
		if m.Screen == ScreenAirlock {
			m.Screen = ScreenCountdown
			m.Countdown = 3
			return m, tickCountdown()
		}

	case "GAME":
		if msg.Val == "STOP" {
			return m.handleGameDisconnect()
		}
	}
	return m, waitForNetworkEvent(m.Game.Net)
}

func (m Model) handleGameDisconnect() (tea.Model, tea.Cmd) {
	m.DebugLog = append(m.DebugLog, "DISCONNECTED. Resetting...")

	// Reset State
	m.Game.Reset()
	m.Screen = ScreenLobby
	m.IsDirect = false
	m.FoundPeerID = ""
	m.LobbyCodeInput = ""
	m.Countdown = 3

	// We must re-subscribe to events and clear the screen artifact
	return m, tea.Batch(tea.ClearScreen, waitForNetworkEvent(m.Game.Net))
}

func (m Model) handleLogEvent(msg protocol.LogEvent) (tea.Model, tea.Cmd) {
	m.DebugLog = append(m.DebugLog, fmt.Sprintf("> %s", string(msg)))
	// Keep log buffer small
	if len(m.DebugLog) > 7 {
		m.DebugLog = m.DebugLog[1:]
	}
	return m, waitForNetworkEvent(m.Game.Net)
}

func (m Model) handleNetMessage(msg protocol.NetMessage) (tea.Model, tea.Cmd) {
	m.Game.ProcessPacket(msg)
	return m, waitForNetworkEvent(m.Game.Net)
}

// --- Timer Handlers ---

func (m Model) handleCountdownTick() (tea.Model, tea.Cmd) {
	if m.Countdown > 0 {
		m.Countdown--
		return m, tickCountdown()
	}

	// Countdown finished -> Start the actual game
	m.Screen = ScreenGame
	m.Game.DetermineRole()
	m.Game.OnGameStarted()

	return m, tea.Batch(tea.ClearScreen, waitForNetworkEvent(m.Game.Net))
}
