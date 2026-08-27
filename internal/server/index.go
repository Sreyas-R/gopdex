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
	EventDiscovered EventKind = iota // all .pdfs found in directory, TotalPDF populated
	EventError                       // single-file failure, Err populated
	EventIndexed                     // pdf parsed + saved to DB
	EventSkipped                     // pdf unchanged, no reindex needed
)

// IndexEvent is every message the TUI receives from the indexer.
type IndexEvent struct {
	EventType EventKind
	FileName  string
	FilePath  string
	Err       error
	TotalPDF  int           // non-zero only on EventDiscovered
	Elapsed   time.Duration // non-zero only on EventDiscovered (total walk time)
}

// Keeping DB writes on a single goroutine avoids concurrent SQLite write errors.
type parsedResult struct {
	doc  *parser.Document
	path string
}

// GetAllPDFs walks root recursively and returns absolute paths of all .pdf files.
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

// Run walks root, indexes all PDFs, and returns a read-only channel of progress events.
// The channel is closed when all work is done or ctx is cancelled.
func Run(ctx context.Context, db *sql.DB, root string, workers int) <-chan IndexEvent {
	ch := make(chan IndexEvent)

	go func() {
		start := time.Now()

		paths, err := GetAllPDFs(root)
		if err != nil {
			ch <- IndexEvent{EventType: EventError, Err: fmt.Errorf("error walking %s: %w", root, err)}
			close(ch)
			return
		}
		if len(paths) == 0 {
			ch <- IndexEvent{EventType: EventError, Err: fmt.Errorf("no PDFs found in %s", root)}
			close(ch)
			return
		}

		ch <- IndexEvent{EventType: EventDiscovered, TotalPDF: len(paths), Elapsed: time.Since(start)}

		pathsCh := make(chan string, len(paths))
		for _, p := range paths {
			pathsCh <- p
		}
		close(pathsCh)

		parsedResults := make(chan parsedResult, workers)

		var wg sync.WaitGroup
		// FAN-OUT: N workers parse in parallel
		for range workers {
			wg.Add(1)
			go worker(ctx, db, pathsCh, parsedResults, ch, &wg)
		}

		go func() {
			wg.Wait()
			close(parsedResults)
		}()

		// FAN-IN: serializes all DB saves
		for res := range parsedResults {
			if err := store.SaveDocument(ctx, db, res.doc, res.path); err != nil {
				ch <- IndexEvent{EventType: EventError, FilePath: res.path, Err: err}
				continue
			}
			ch <- IndexEvent{
				EventType: EventIndexed,
				FilePath:  res.path,
				FileName:  res.doc.Metadata.FileName,
			}
		}

		close(ch)
	}()

	return ch
}

func worker(
	ctx context.Context,
	db *sql.DB,
	paths <-chan string,
	parsed chan<- parsedResult,
	events chan<- IndexEvent,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return

		case path, ok := <-paths:
			if !ok {
				return // pathsCh closed, no more work
			}

			// GetDocumentByPath returns nil doc + sql.ErrNoRows if not yet indexed.
			// NeedsReindexing(nil) returns true, so we handle both cases the same way lol
			existing, _ := store.GetDocumentByPath(ctx, db, path)
			needsReindexing, _ := parser.NeedsReindexing(existing)

			if !needsReindexing {
				log.Printf("Skipped parsing PDF (Already indexed & unchanged): %s", path)
				events <- IndexEvent{
					EventType: EventSkipped,
					FileName:  existing.Metadata.FileName,
					FilePath:  path,
				}
				continue
			}

			fileCtx, cancel := context.WithTimeout(ctx, 30*time.Second) // Note: user mentioned 30s previously, setting to 30s.

			type parseRes struct {
				doc parser.Document
				err error
			}
			resCh := make(chan parseRes, 1)

			log.Printf("Starting to parse PDF: %s", path)
			
			go func(p string) {
				d, e := parser.Parse(p)
				resCh <- parseRes{doc: d, err: e}
			}(path)

			var doc parser.Document
			var err error

			select {
			case <-fileCtx.Done():
				err = fmt.Errorf("PDF Corrupted / Too Large!: %w", fileCtx.Err())
			case res := <-resCh:
				doc = res.doc
				err = res.err
			}
			cancel()

			if err != nil {
				log.Printf("Finished parsing PDF: %s (Status: Error - %v)", path, err)
				events <- IndexEvent{EventType: EventError, FilePath: path, Err: err}
				continue
			}

			log.Printf("Finished parsing PDF: %s (Status: Success)", path)

			// hand off to writer — EventIndexed is sent there after DB save succeeds
			parsed <- parsedResult{doc: &doc, path: path}
		}
	}
}

func Search(ctx context.Context, db *sql.DB, query string) ([]store.SearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("Search query is empty")
	}

	res, err := store.SearchFTS(ctx, db, query)
	if err != nil {
		return nil, err
	}

	return res, nil
}
