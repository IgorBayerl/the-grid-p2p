package ui

import (
	"github.com/IgorBayerl/the-grid/internal/game"
	tea "github.com/charmbracelet/bubbletea"
)

type Screen int

const (
	ScreenBoot Screen = iota
	ScreenLobby
	ScreenAirlock
	ScreenCountdown
	ScreenGame
)

type Model struct {
	Game   *game.Engine
	Screen Screen

	// UI State
	LobbyCodeInput string
	SpinnerIdx     int
	SwarmPeers     string
	DebugLog       []string

	// Connection State
	DHTReady    bool
	FoundPeerID string
	IsDirect    bool

	Countdown     int // For Game Start (3..2..1)
	SearchTimeout int // For Lobby Waiting (120s)
}

func NewModel(g *game.Engine) Model {
	return Model{
		Game:          g,
		Screen:        ScreenBoot,
		DebugLog:      []string{"Initializing UI..."},
		SwarmPeers:    "0",
		Countdown:     3,
		SearchTimeout: 120, // Default 2 minutes
	}
}

func (m Model) Init() tea.Cmd {
	startBootstrap := func() tea.Msg {
		go m.Game.Net.Bootstrap()
		return nil
	}
	return tea.Batch(
		tickSpinner(),
		waitForNetworkEvent(m.Game.Net),
		tickGame(),
		startBootstrap,
	)
}
