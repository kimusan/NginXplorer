// Package tui provides the terminal user interface for NginXplorer,
// built with Bubble Tea and Lip Gloss.
package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Color palette — vibrant accents on dark background
var (
	colorPrimary   = lipgloss.Color("#3b82f6") // Blue
	colorSuccess   = lipgloss.Color("#10b981") // Green
	colorWarning   = lipgloss.Color("#f59e0b") // Amber
	colorDanger    = lipgloss.Color("#ef4444") // Red
	colorInfo      = lipgloss.Color("#8b5cf6") // Purple
	colorMuted     = lipgloss.Color("#6b7280") // Gray
	colorBg        = lipgloss.Color("#111827") // Dark background
	colorCardBg    = lipgloss.Color("#1f2937") // Card background
	colorBorder    = lipgloss.Color("#374151") // Border
	colorText      = lipgloss.Color("#f9fafb") // White text
	colorTextDim   = lipgloss.Color("#9ca3af") // Dim text
)

// Styles for various UI components
var (
	// Title bar
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			PaddingRight(2)

	headerStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Background(lipgloss.Color("#1e3a5f")).
			Bold(true).
			Padding(0, 1)

	// Stat cards
	statLabelStyle = lipgloss.NewStyle().
			Foreground(colorTextDim).
			Bold(false)

	statValueStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Bold(true)

	statGoodStyle = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	statBadStyle = lipgloss.NewStyle().
			Foreground(colorDanger).
			Bold(true)

	// Bar chart styles
	barFullStyle  = lipgloss.NewStyle().Foreground(colorSuccess)
	bar3xxStyle   = lipgloss.NewStyle().Foreground(colorPrimary)
	bar4xxStyle   = lipgloss.NewStyle().Foreground(colorWarning)
	bar5xxStyle   = lipgloss.NewStyle().Foreground(colorDanger)

	// Section headers
	sectionStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true).
			MarginTop(1)

	// Table styles
	tableHeaderStyle = lipgloss.NewStyle().
			Foreground(colorTextDim).
			Bold(true).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorBorder)

	tableRowStyle = lipgloss.NewStyle().
			Foreground(colorText)

	// VHost list
	vhostActiveStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	vhostInactiveStyle = lipgloss.NewStyle().
			Foreground(colorTextDim)

	// Help bar
	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(colorTextDim)

	// Sparkline
	sparkStyle = lipgloss.NewStyle().
		Foreground(colorPrimary)

	sparkLabelStyle = lipgloss.NewStyle().
		Foreground(colorTextDim).
		Width(5)
)
