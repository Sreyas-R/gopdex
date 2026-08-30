package server

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Sreyas-R/gopdex/internal/parser"
	"github.com/Sreyas-R/gopdex/internal/store"
)

type EventKind int

const (
	EventDiscovered EventKind = iota
	EventError
	EventIndexed
	EventSkipped
)

type IndexEvent struct {
	EventType EventKind
	FileName  string
	FilePath  string
	Err       error
	TotalPDF  int
	Elapsed   time.Duration
}

func GetAllPDFs(root string) ([]string, error) {
	output := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".pdf") {
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			output = append(output, abs)
		}
		return nil
	})
	return output, err
}

func Run(ctx context.Context, db *sql.DB, root string, workers int) <-chan IndexEvent {
	ch := make(chan IndexEvent)

	go func() {
		defer close(ch)
		start := time.Now()

		paths, err := GetAllPDFs(root)
		if err != nil {
			ch <- IndexEvent{EventType: EventError, Err: fmt.Errorf("error walking %s: %w", root, err)}
			return
		}
		if len(paths) == 0 {
			ch <- IndexEvent{EventType: EventError, Err: fmt.Errorf("no PDFs found in %s", root)}
			return
		}

		ch <- IndexEvent{EventType: EventDiscovered, TotalPDF: len(paths), Elapsed: time.Since(start)}

		existingDocs, err := store.GetAllDocuments(ctx, db)
		if err != nil {
			existingDocs = make(map[string]*parser.Document)
		}

		pathsCh := make(chan string, len(paths))
		for _, p := range paths {
			pathsCh <- p
		}
		close(pathsCh)

		var wg sync.WaitGroup
		for range workers {
			wg.Add(1)
			go worker(ctx, db, pathsCh, existingDocs, ch, &wg)
		}
		wg.Wait()
	}()

	return ch
}

func worker(ctx context.Context, db *sql.DB, paths <-chan string, existingDocs map[string]*parser.Document, events chan<- IndexEvent, wg *sync.WaitGroup) {
	defer wg.Done()

	for path := range paths {
		select {
		case <-ctx.Done():
			return
		default:
		}

		existing := existingDocs[path]
		needsReindex, meta, _ := parser.NeedsReindexing(path, existing)

		if !needsReindex {
			log.Printf("Skipped (unchanged): %s", path)
			events <- IndexEvent{EventType: EventSkipped, FileName: meta.FileName, FilePath: path}
			continue
		}

		pages, totalPages, err := parser.ParseFile(path)
		if err != nil {
			log.Printf("Parse error: %s — %v", path, err)
			events <- IndexEvent{EventType: EventError, FilePath: path, Err: err}
			continue
		}

		if err := store.SaveDocument(ctx, db, path, meta, totalPages, pages); err != nil {
			log.Printf("DB error: %s — %v", path, err)
			events <- IndexEvent{EventType: EventError, FilePath: path, Err: err}
			continue
		}

		log.Printf("Indexed: %s (%d pages)", path, totalPages)
		events <- IndexEvent{EventType: EventIndexed, FileName: meta.FileName, FilePath: path}
	}
}

func Search(ctx context.Context, db *sql.DB, query string) ([]store.SearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("search query is empty")
	}
	return store.SearchFTS(ctx, db, query)
}
