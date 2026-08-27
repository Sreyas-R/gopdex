package tui

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/exec"
	"runtime"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/Sreyas-R/gopdex/internal/server"
	"github.com/Sreyas-R/gopdex/internal/store"
)

type Screen int

const (
	ScreenHome Screen = iota
	ScreenAddPath
	ScreenIndexing
	ScreenSearch
	ScreenResults
	ScreenDetail
)

// Channel bridge messages
type indexEventMsg server.IndexEvent
type indexDoneMsg struct{}
type openPDFMsg struct{ err error }

// Activity item for the live indexing feed
type ActivityItem struct {
	Type     server.EventKind
	FileName string
	Err      error
}

type Model struct {
	ctx    context.Context
	cancel context.CancelFunc
	db     *sql.DB
	screen Screen

	// Home
	menuIndex int

	// Bubbles FilePicker
	filepicker filepicker.Model
	pickerErr  string

	// Bubbles Animated Progress Bar
	progress progress.Model

	// Indexing
	indexCh       <-chan server.IndexEvent
	totalPDFs     int
	indexedCount  int
	skippedCount  int
	errorCount    int
	lastFile      string
	indexFinished bool
	allLog        []ActivityItem
	logOffset     int

	// Search & Results
	searchQuery     string
	searchSearching bool
	searchErr       error
	searchResults   []store.SearchResult
	resultsCursor   int
	selectedDoc     store.SearchResult
	showOpenPrompt  bool
	openErr         error
}

func New(ctx context.Context, db *sql.DB) Model {
	c, cancel := context.WithCancel(ctx)

	fp := filepicker.New()
	fp.DirAllowed = true
	fp.FileAllowed = true
	fp.ShowSize = true
	fp.ShowPermissions = false
	fp.AllowedTypes = []string{}
	fp.CurrentDirectory, _ = os.Getwd()

	// Animated gradient progress bar
	prog := progress.New(progress.WithDefaultGradient())
	prog.Width = 40

	return Model{
		ctx:        c,
		cancel:     cancel,
		db:         db,
		screen:     ScreenHome,
		filepicker: fp,
		progress:   prog,
		allLog:     make([]ActivityItem, 0),
	}
}

func (m Model) Init() tea.Cmd {
	return m.filepicker.Init()
}

func waitForIndexEvent(ch <-chan server.IndexEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return indexDoneMsg{}
		}
		return indexEventMsg(ev)
	}
}

func openPDFCmd(path string) tea.Cmd {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", path)
	case "windows":
		c = exec.Command("cmd", "/c", "start", path)
	default:
		c = exec.Command("xdg-open", path)
	}

	return tea.ExecProcess(c, func(err error) tea.Msg {
		return openPDFMsg{err: err}
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		var fpCmd tea.Cmd
		m.filepicker, fpCmd = m.filepicker.Update(msg)
		cmds = append(cmds, fpCmd)

		m.progress.Width = msg.Width - 20
		if m.progress.Width > 50 {
			m.progress.Width = 50
		}
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.String() == "ctrl+c" {
			log.Println("[TUI] User quit with Ctrl+C")
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}

		switch m.screen {
		case ScreenHome:
			return m.updateHome(msg.String())
		case ScreenAddPath:
			return m.updateAddPath(msg)
		case ScreenIndexing:
			return m.updateIndexing(msg.String())
		case ScreenSearch:
			return m.updateSearch(msg.String(), msg)
		case ScreenResults:
			return m.updateResults(msg.String())
		case ScreenDetail:
			return m.updateDetail(msg.String())
		}

	case indexEventMsg:
		ev := server.IndexEvent(msg)
		item := ActivityItem{Type: ev.EventType, FileName: ev.FileName, Err: ev.Err}
		if item.FileName == "" && ev.FilePath != "" {
			item.FileName = ev.FilePath
		}

		switch ev.EventType {
		case server.EventDiscovered:
			m.totalPDFs = ev.TotalPDF
			log.Printf("[INDEX] Discovered %d PDFs in folder", ev.TotalPDF)
		case server.EventIndexed:
			m.indexedCount++
			m.lastFile = ev.FileName
			m.appendLog(item)
			log.Printf("[INDEX] Successfully indexed: %s", ev.FileName)
		case server.EventSkipped:
			m.skippedCount++
			m.lastFile = ev.FileName
			m.appendLog(item)
			log.Printf("[INDEX] Skipped (unchanged): %s", ev.FileName)
		case server.EventError:
			m.errorCount++
			if ev.FilePath != "" {
				m.lastFile = ev.FilePath
			}
			m.appendLog(item)
			log.Printf("[INDEX ERROR] File: %s, Error: %v", ev.FilePath, ev.Err)
		}

		// Update animated progress bar percentage
		processed := m.indexedCount + m.skippedCount + m.errorCount
		if m.totalPDFs > 0 {
			progCmd := m.progress.SetPercent(float64(processed) / float64(m.totalPDFs))
			cmds = append(cmds, progCmd)
		}

		cmds = append(cmds, waitForIndexEvent(m.indexCh))
		return m, tea.Batch(cmds...)

	case progress.FrameMsg:
		progressModel, progCmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, progCmd

	case indexDoneMsg:
		m.indexFinished = true
		progCmd := m.progress.SetPercent(1.0)
		log.Printf("[INDEX DONE] Total: %d, Indexed: %d, Skipped: %d, Errors: %d",
			m.totalPDFs, m.indexedCount, m.skippedCount, m.errorCount)
		return m, progCmd

	case searchResultMsg:
		m.searchSearching = false
		m.searchErr = msg.err
		m.searchResults = msg.results
		m.resultsCursor = 0
		if msg.err == nil && len(msg.results) > 0 {
			m.screen = ScreenResults
		}
		return m, nil

	case openPDFMsg:
		if msg.err != nil {
			m.openErr = msg.err
			log.Printf("[OPEN PDF ERROR] %v", msg.err)
		}
		return m, nil
	}

	// Forward non-key messages to active screen / filepicker
	if m.screen == ScreenAddPath {
		return m.updateAddPath(msg)
	}

	var cmd tea.Cmd
	m.filepicker, cmd = m.filepicker.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	switch m.screen {
	case ScreenHome:
		return m.viewHome()
	case ScreenAddPath:
		return m.viewAddPath()
	case ScreenIndexing:
		return m.viewIndexing()
	case ScreenSearch:
		return m.viewSearch()
	case ScreenResults:
		return m.viewResults()
	case ScreenDetail:
		return m.viewDetail()
	}
	return ""
}

func (m *Model) appendLog(item ActivityItem) {
	m.allLog = append(m.allLog, item)
	const visibleLines = 6
	if len(m.allLog) > visibleLines {
		m.logOffset = len(m.allLog) - visibleLines
	}
}

func (m *Model) startIndexing(path string) tea.Cmd {
	m.totalPDFs = 0
	m.indexedCount = 0
	m.skippedCount = 0
	m.errorCount = 0
	m.lastFile = ""
	m.indexFinished = false
	m.allLog = make([]ActivityItem, 0)
	m.logOffset = 0
	m.progress.SetPercent(0.0)

	log.Printf("[INDEX] Starting indexing on path: %s", path)
	m.screen = ScreenIndexing
	m.indexCh = server.Run(m.ctx, m.db, path, runtime.NumCPU())
	return waitForIndexEvent(m.indexCh)
}
