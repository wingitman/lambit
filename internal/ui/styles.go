package ui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors — mirror listicles palette.
	ColorPrimary  = lipgloss.Color("#7C9EF0") // soft blue
	ColorAccent   = lipgloss.Color("#F0A47C") // soft orange
	ColorMuted    = lipgloss.Color("#666688")
	ColorError    = lipgloss.Color("#F07C7C")
	ColorSuccess  = lipgloss.Color("#7CF09C")
	ColorBorder   = lipgloss.Color("#444466")
	ColorSelected = lipgloss.Color("#2A2A4A")
	ColorHeaderBg = lipgloss.Color("#1A1A2E")
	ColorResult   = lipgloss.Color("#B0B0CC")
	ColorBrand1   = lipgloss.Color("#FFFFFF") // "delby"
	ColorBrand2   = lipgloss.Color("#5865F2") // "soft"

	// Base styles
	StyleNormal = lipgloss.NewStyle()

	StyleSelected = lipgloss.NewStyle().
			Background(ColorSelected).
			Foreground(lipgloss.Color("#EEEEFF")).
			Bold(true)

	StyleMuted = lipgloss.NewStyle().
			Foreground(ColorMuted)

	StyleError = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true)

	StyleSuccess = lipgloss.NewStyle().
			Foreground(ColorSuccess)

	StyleAccent = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true)

	StylePrimary = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	StyleHeader = lipgloss.NewStyle().
			Background(ColorHeaderBg).
			Foreground(ColorPrimary).
			Bold(true).
			Padding(0, 1)

	StyleStatusBar = lipgloss.NewStyle().
			Foreground(ColorMuted)

	StyleBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder)

	StyleConfirmBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorAccent).
			Padding(1, 2).
			Margin(1, 0)

	StyleInputPrompt = lipgloss.NewStyle().
				Foreground(ColorAccent).
				Bold(true)

	StyleDetail = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Italic(true)

	StyleResult = lipgloss.NewStyle().
			Foreground(ColorResult)

	StyleSectionTitle = lipgloss.NewStyle().
				Foreground(ColorPrimary).
				Bold(true)

	StyleBenchFilled = "█"
	StyleBenchEmpty  = "░"
)
