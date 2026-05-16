package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wingitman/lambit/internal/app"
	"github.com/wingitman/lambit/internal/config"
	"github.com/wingitman/lambit/internal/version"

	// Ensure runtime implementations are registered via their init() functions.
	_ "github.com/wingitman/lambit/internal/runtime"
)

func main() {
	recordUpdate := flag.Bool("record-update", false, "record installed update metadata and exit")
	updateCommit := flag.String("update-commit", "", "commit to record with --record-update")
	updateRepo := flag.String("update-repo", "", "repo path to record with --record-update")
	flag.Parse()

	if *recordUpdate {
		if err := config.RecordUpdateMetadata(*updateCommit, *updateRepo); err != nil {
			fmt.Fprintf(os.Stderr, "Error recording update metadata: %v\n", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load config: %v\n", err)
	}
	if version.Commit != "" && version.Commit != "dev" && cfg.Updates.CurrentCommit != version.Commit {
		cfg.Updates.CurrentCommit = version.Commit
		_ = config.RecordUpdateMetadata(version.Commit, cfg.Updates.RepoPath)
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

	// Give the API server goroutine a reference to the program so it can
	// send results into the BubbleTea event loop.
	model.SetProgram(p)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running lambit: %v\n", err)
		os.Exit(1)
	}
}
