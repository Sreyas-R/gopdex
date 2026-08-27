package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) updateAddPath(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.screen = ScreenHome
			return m, nil

		case "space", "s":
			// Shortcut to immediately index the folder currently open in the filepicker
			cmd := m.startIndexing(m.filepicker.CurrentDirectory)
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.filepicker, cmd = m.filepicker.Update(msg)

	// User pressed enter on a file or directory
	if didSelect, path := m.filepicker.DidSelectFile(msg); didSelect {
		targetPath := path
		stat, err := os.Stat(path)
		if err == nil && !stat.IsDir() {
			// If user clicked directly on a PDF file, index its containing folder
			if !strings.HasSuffix(strings.ToLower(path), ".pdf") {
				m.pickerErr = filepath.Base(path) + " is not a PDF file"
				return m, nil
			}
			targetPath = filepath.Dir(path)
		}
		return m, m.startIndexing(targetPath)
	}

	return m, cmd
}

func (m Model) viewAddPath() string {
	var b strings.Builder

	b.WriteString("\n  PDEX > SELECT FOLDER TO INDEX\n")
	b.WriteString("  ─────────────────────────────────────────────────────────────\n\n")

	// Render the official filepicker
	b.WriteString(m.filepicker.View() + "\n\n")

	if m.pickerErr != "" {
		b.WriteString(fmt.Sprintf("  [!] %s\n\n", m.pickerErr))
	}

	b.WriteString("  ─────────────────────────────────────────────────────────────\n")
	b.WriteString("  [enter] Select item • [space / s] Index current folder • [esc] Back\n\n")

	return b.String()
}
