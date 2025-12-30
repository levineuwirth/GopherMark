package ui

import "github.com/charmbracelet/lipgloss"

var (
	primaryColor   = lipgloss.Color("#7D56F4")
	secondaryColor = lipgloss.Color("#3C3C3C")
	accentColor    = lipgloss.Color("#FF79C6")
	textColor      = lipgloss.Color("#FAFAFA")
	dimColor       = lipgloss.Color("#6C6C6C")
	borderColor    = lipgloss.Color("#383838")

	baseStyle = lipgloss.NewStyle().
			Foreground(textColor)

	titleStyle = lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true).
			Padding(0, 1)

	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(0, 1)

	activePaneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(0, 1)

	selectedItemStyle = lipgloss.NewStyle().
				Foreground(accentColor).
				Bold(true).
				PaddingLeft(1)

	normalItemStyle = lipgloss.NewStyle().
			Foreground(textColor).
			PaddingLeft(1)

	folderStyle = lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(dimColor)

	helpStyle = lipgloss.NewStyle().
			Foreground(dimColor).
			Padding(1, 0)
)
