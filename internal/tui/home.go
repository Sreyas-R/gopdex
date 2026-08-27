package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

var menuItems = []string{
	"Search documents",
	"Add documents (Index a folder)",
	"Quit",
}

func (m Model) updateHome(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit

	case "1":
		m.screen = ScreenSearch
		return m, nil

	case "2":
		m.screen = ScreenAddPath
		return m, m.filepicker.Init()

	case "up", "k":
		if m.menuIndex > 0 {
			m.menuIndex--
		}

	case "down", "j":
		if m.menuIndex < len(menuItems)-1 {
			m.menuIndex++
		}

	case "enter":
		if m.menuIndex == 0 {
			m.screen = ScreenSearch
			return m, nil
		} else if m.menuIndex == 1 {
			m.screen = ScreenAddPath
			return m, m.filepicker.Init()
		} else if m.menuIndex == 2 {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) viewHome() string {
	var b strings.Builder

	b.WriteString("\n  PDEX • LOCAL PDF SEARCH & INDEXER\n")
	b.WriteString("  ─────────────────────────────────────────────────────────────\n\n")

	for i, item := range menuItems {
		pointer := "   "
		numKey := fmt.Sprintf("[%d]", i+1)
		if i == len(menuItems)-1 {
			numKey = "[q]"
		}

		if m.menuIndex == i {
			pointer = " > "
			b.WriteString(fmt.Sprintf(" %s %s %s\n", pointer, numKey, item))
		} else {
			b.WriteString(fmt.Sprintf(" %s %s %s\n", pointer, numKey, item))
		}
	}

	b.WriteString("\n  ─────────────────────────────────────────────────────────────\n")
	b.WriteString("  Controls: [1/2/q] Quick jump • [j/k/↑/↓] Navigate • [enter] Select\n\n")

	return b.String()
}
