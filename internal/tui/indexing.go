package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Sreyas-R/gopdex/internal/server"
	tea "github.com/charmbracelet/bubbletea"
)

const visibleLogLines = 6

func (m Model) updateIndexing(key string) (tea.Model, tea.Cmd) {
	maxOffset := len(m.allLog) - visibleLogLines
	if maxOffset < 0 {
		maxOffset = 0
	}

	switch key {
	case "up", "k":
		if m.logOffset > 0 {
			m.logOffset--
		}
		return m, nil

	case "down", "j":
		if m.logOffset < maxOffset {
			m.logOffset++
		}
		return m, nil

	case "esc", "enter", "q":
		if m.indexFinished || key == "esc" {
			m.screen = ScreenHome
			return m, nil
		}
	}

	return m, nil
}

func (m Model) viewIndexing() string {
	var b strings.Builder

	b.WriteString("\n  PDEX > INDEXING\n")
	b.WriteString("  ─────────────────────────────────────────────────────────────\n\n")

	// Status badge
	if m.indexFinished {
		b.WriteString("  Status: [ ✓ COMPLETED ]\n\n")
	} else {
		b.WriteString("  Status: [ ⟳ INDEXING IN PROGRESS... ]\n\n")
	}

	// Official Bubbles Animated Progress Bar
	b.WriteString(fmt.Sprintf("  Progress : %s\n\n", m.progress.View()))

	// Summary Statistics
	b.WriteString("  Tally:\n")
	b.WriteString(fmt.Sprintf("    • Total PDFs Found : %d\n", m.totalPDFs))
	b.WriteString(fmt.Sprintf("    • Newly Indexed    : %d\n", m.indexedCount))
	b.WriteString(fmt.Sprintf("    • Unchanged (Skip) : %d\n", m.skippedCount))
	b.WriteString(fmt.Sprintf("    • Errors / Failed  : %d\n\n", m.errorCount))

	// Scrollable Activity Log
	totalItems := len(m.allLog)
	start := m.logOffset
	if start > totalItems {
		start = 0
	}
	end := start + visibleLogLines
	if end > totalItems {
		end = totalItems
	}

	scrollInfo := ""
	if totalItems > visibleLogLines {
		scrollInfo = fmt.Sprintf(" (Showing %d : %d of %d • [↑/↓] Scroll)", start+1, end, totalItems)
	}

	b.WriteString(fmt.Sprintf("  Activity Log%s:\n", scrollInfo))
	if totalItems == 0 {
		b.WriteString("    (Waiting for workers...)\n")
	} else {
		for i := start; i < end; i++ {
			item := m.allLog[i]
			fileName := filepath.Base(item.FileName)
			if len(fileName) > 36 {
				fileName = fileName[:33] + "..."
			}

			switch item.Type {
			case server.EventIndexed:
				b.WriteString(fmt.Sprintf("    [%d] [✓ Indexed]   %s\n", i+1, fileName))
			case server.EventSkipped:
				b.WriteString(fmt.Sprintf("    [%d] [= Unchanged] %s\n", i+1, fileName))
			case server.EventError:
				errStr := ""
				if item.Err != nil {
					errStr = fmt.Sprintf(" (%v)", item.Err)
				}
				b.WriteString(fmt.Sprintf("    [%d] [✗ Failed]    %s%s\n", i+1, fileName, errStr))
			}
		}
	}

	b.WriteString("\n  ─────────────────────────────────────────────────────────────\n")
	if m.indexFinished {
		b.WriteString("  [↑/↓/j/k] Scroll activity log • [enter/esc] Return to Home\n\n")
	} else {
		b.WriteString("  [↑/↓/j/k] Scroll activity log • [esc] Cancel and return Home\n\n")
	}

	return b.String()
}
