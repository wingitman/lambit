package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wingitman/lambit/internal/app"
	"github.com/wingitman/lambit/internal/config"

	// Ensure runtime implementations are registered via their init() functions.
	_ "github.com/wingitman/lambit/internal/runtime"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load config: %v\n", err)
	}

	// Use the current working directory as the project root.
	projectDir, err := os.Getwd()
	if err != nil {
		projectDir = "."
	}

	model, err := app.New(cfg, projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running lambit: %v\n", err)
		os.Exit(1)
	}
}
