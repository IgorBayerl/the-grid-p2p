package main

import (
	"fmt"
	"os"

	"github.com/IgorBayerl/the-grid/internal/game"
	"github.com/IgorBayerl/the-grid/internal/p2p"
	"github.com/IgorBayerl/the-grid/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	network := p2p.NewNode()
	engine := game.NewEngine(network)
	model := ui.NewModel(engine)

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
}
