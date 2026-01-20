package main

import (
	"github.com/IgorBayerl/the-grid/internal/game"
	"github.com/IgorBayerl/the-grid/internal/p2p"
	"github.com/IgorBayerl/the-grid/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// 1. Infrastructure
	network := p2p.NewNode()

	// 2. Domain
	engine := game.NewEngine(network)

	// 3. Presentation
	model := ui.NewModel(engine)

	// 4. Run
	p := tea.NewProgram(model, tea.WithAltScreen())
	p.Run()
}
