package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/Sreyas-R/gopdex/internal/store"
	"github.com/Sreyas-R/gopdex/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	f, err := tea.LogToFile("debug.log", "pdex")
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not open debug.log: %v\n", err)
	} else {
		defer f.Close()
		log.Println("--- PDEX Session Started ---")
	}

	dbDir := "./db"
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		log.Printf("failed to create db dir: %v", err)
		fmt.Fprintf(os.Stderr, "failed to create db dir: %v\n", err)
		os.Exit(1)
	}

	dbPath := filepath.Join(dbDir, "pdex.db")
	db, err := store.Open(dbPath)
	if err != nil {
		log.Printf("failed to open db at %s: %v", dbPath, err)
		fmt.Fprintf(os.Stderr, "failed to open db: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := tui.New(ctx, db)
	p := tea.NewProgram(m)

	if _, err := p.Run(); err != nil {
		log.Printf("tui runtime error: %v", err)
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
}
