package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// --- Messages ---

type TickSpinnerMsg time.Time
type TickGameMsg time.Time
type TickCountdownMsg time.Time

// --- Commands ---

func tickSpinner() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg { return TickSpinnerMsg(t) })
}

func tickGame() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg { return TickGameMsg(t) })
}

func tickCountdown() tea.Cmd {
	return tea.Tick(time.Second*1, func(t time.Time) tea.Msg { return TickCountdownMsg(t) })
}

func waitForNetworkEvent(n interface{ Events() <-chan interface{} }) tea.Cmd {
	return func() tea.Msg {
		return <-n.Events()
	}
}
