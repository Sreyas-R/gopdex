package tui

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/Sreyas-R/gopdex/internal/server"
	"github.com/Sreyas-R/gopdex/internal/store"
)

type searchResultMsg struct {
	results []store.SearchResult
	err     error
}

func doSearch(ctx context.Context, db *sql.DB, query string) tea.Cmd {
	return func() tea.Msg {
		results, err := server.Search(ctx, db, query)
		return searchResultMsg{results: results, err: err}
	}
}

// -------------------------------------------------------------
// Search Screen
// -------------------------------------------------------------

func (m Model) updateSearch(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.screen = ScreenHome
		return m, nil

	case "enter":
		query := strings.TrimSpace(m.searchQuery)
		if query == "" {
			return m, nil
		}
		m.searchSearching = true
		m.searchErr = nil
		return m, doSearch(m.ctx, m.db, query)

	case "backspace":
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
		}

	default:
		if len(key) == 1 {
			m.searchQuery += key
		}
	}
	return m, nil
}

func (m Model) viewSearch() string {
	var b strings.Builder

	b.WriteString("\n  PDEX > SEARCH DOCUMENTS\n")
	b.WriteString("  ─────────────────────────────────────────────────────────────\n\n")

	b.WriteString(fmt.Sprintf("  Search: %s█\n\n", m.searchQuery))

	if m.searchSearching {
		b.WriteString("  Status: [ ⟳ Searching FTS5 index... ]\n\n")
	} else if m.searchErr != nil {
		b.WriteString(fmt.Sprintf("  [!] Error: %v\n\n", m.searchErr))
	} else if len(m.searchResults) == 0 && m.searchQuery != "" {
		b.WriteString(fmt.Sprintf("  No matches found for %q.\n\n", m.searchQuery))
	}

	b.WriteString("  ─────────────────────────────────────────────────────────────\n")
	b.WriteString("  [enter] Search • [backspace] Delete • [esc] Back to Home\n\n")

	return b.String()
}

// -------------------------------------------------------------
// Results Screen (Top 5 Ranked Results)
// -------------------------------------------------------------

func (m Model) updateResults(key string) (tea.Model, tea.Cmd) {
	// If the open confirmation modal prompt is active
	if m.showOpenPrompt {
		switch key {
		case "y", "Y", "enter":
			m.showOpenPrompt = false
			return m, openPDFCmd(m.selectedDoc.Path)
		case "n", "N", "esc":
			m.showOpenPrompt = false
			return m, nil
		}
		return m, nil
	}

	switch key {
	case "esc":
		m.screen = ScreenSearch
		return m, nil

	case "up", "k":
		if m.resultsCursor > 0 {
			m.resultsCursor--
		}

	case "down", "j":
		if m.resultsCursor < len(m.searchResults)-1 {
			m.resultsCursor++
		}

	case "o", "O":
		// Open confirmation prompt directly from results list
		if len(m.searchResults) > 0 && m.resultsCursor < len(m.searchResults) {
			m.selectedDoc = m.searchResults[m.resultsCursor]
			m.showOpenPrompt = true
		}
		return m, nil

	case "enter":
		if len(m.searchResults) > 0 && m.resultsCursor < len(m.searchResults) {
			m.selectedDoc = m.searchResults[m.resultsCursor]
			m.screen = ScreenDetail
		}
	}
	return m, nil
}

func (m Model) viewResults() string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("\n  PDEX > RESULTS FOR %q (%d Top Matches)\n", m.searchQuery, len(m.searchResults)))
	b.WriteString("  ─────────────────────────────────────────────────────────────\n\n")

	for i, res := range m.searchResults {
		pointer := "   "
		if m.resultsCursor == i {
			pointer = " > "
		}

		b.WriteString(fmt.Sprintf(" %s[%d] %s • Page %d  (Rank Score: %.2f)\n",
			pointer, i+1, res.FileName, res.PageNumber, res.Score))

		snippet := strings.TrimSpace(res.Snippet)
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		if len(snippet) > 80 {
			snippet = snippet[:77] + "..."
		}
		b.WriteString(fmt.Sprintf("       Snippet: %s\n\n", snippet))
	}

	if m.showOpenPrompt {
		b.WriteString(renderOpenPrompt(m.selectedDoc))
	} else {
		b.WriteString("  ─────────────────────────────────────────────────────────────\n")
		b.WriteString("  [j/k/↑/↓] Select • [enter] View Detail • [o] Open PDF • [esc] Back\n\n")
	}

	return b.String()
}

// -------------------------------------------------------------
// Detail Screen
// -------------------------------------------------------------

func (m Model) updateDetail(key string) (tea.Model, tea.Cmd) {
	if m.showOpenPrompt {
		switch key {
		case "y", "Y", "enter":
			m.showOpenPrompt = false
			return m, openPDFCmd(m.selectedDoc.Path)
		case "n", "N", "esc":
			m.showOpenPrompt = false
			return m, nil
		}
		return m, nil
	}

	switch key {
	case "esc":
		m.screen = ScreenResults
		return m, nil
	case "o", "O", "enter":
		m.showOpenPrompt = true
		return m, nil
	}
	return m, nil
}

func (m Model) viewDetail() string {
	var b strings.Builder

	b.WriteString("\n  PDEX > DOCUMENT MATCH DETAIL\n")
	b.WriteString("  ─────────────────────────────────────────────────────────────\n\n")

	b.WriteString(fmt.Sprintf("  File     : %s\n", m.selectedDoc.FileName))
	b.WriteString(fmt.Sprintf("  Page     : %d\n", m.selectedDoc.PageNumber))
	b.WriteString(fmt.Sprintf("  BM25 Rank: %.4f\n", m.selectedDoc.Score))
	b.WriteString(fmt.Sprintf("  Path     : %s\n\n", m.selectedDoc.Path))

	b.WriteString("  Extracted Snippet:\n")
	b.WriteString("  ┌───────────────────────────────────────────────────────────┐\n")
	lines := strings.Split(m.selectedDoc.Snippet, "\n")
	for _, l := range lines {
		b.WriteString(fmt.Sprintf("  │ %-57s │\n", l))
	}
	b.WriteString("  └───────────────────────────────────────────────────────────┘\n\n")

	if m.showOpenPrompt {
		b.WriteString(renderOpenPrompt(m.selectedDoc))
	} else {
		b.WriteString("  ─────────────────────────────────────────────────────────────\n")
		b.WriteString("  [o / enter] Open PDF in Viewer • [esc] Back to Results\n\n")
	}

	return b.String()
}

func renderOpenPrompt(doc store.SearchResult) string {
	var b strings.Builder
	b.WriteString("  ┌───────────────────────────────────────────────────────────┐\n")
	b.WriteString("  │  Open Document in Default PDF Viewer?                     │\n")
	b.WriteString("  │                                                           │\n")
	b.WriteString(fmt.Sprintf("  │  File : %-49s │\n", doc.FileName))
	b.WriteString("  │                                                           │\n")
	b.WriteString("  │  [y / enter] Yes, Open           [n / esc] Cancel         │\n")
	b.WriteString("  └───────────────────────────────────────────────────────────┘\n\n")
	return b.String()
}
