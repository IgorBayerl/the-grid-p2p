package main

import (
	"fmt"
	"os"

	"github.com/IgorBayerl/the-grid/internal/p2p"
	"github.com/IgorBayerl/the-grid/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// 1. Initialize the "Serverless" Backend
	node := p2p.NewNode()

	// 2. Initialize the UI with a reference to the backend
	model := ui.NewModel(node)

	// 3. Run the Program
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
