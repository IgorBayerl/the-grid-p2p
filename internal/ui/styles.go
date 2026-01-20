package ui

import "github.com/charmbracelet/lipgloss"

// UI Styles
var (
	styleSuccess = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))  // Green
	stylePending = lipgloss.NewStyle().Foreground(lipgloss.Color("220")) // Yellow
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // Gray
	styleHost    = lipgloss.NewStyle().Foreground(lipgloss.Color("120")).Bold(true)
	styleClient  = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	styleLog     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)
