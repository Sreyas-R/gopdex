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
			ch <- IndexEvent{EventType: EventError, Err: fmt.Errorf("No PDFs found in %s", root)}
			close(ch)
			return
		}

		ch <- IndexEvent{EventType: EventDiscovered, TotalPDF: len(paths), Elapsed: time.Since(start)}

		pathsCh := make(chan string, len(paths))
		for _, p := range paths {
			pathsCh <- p
		}
		close(pathsCh)

		var wg sync.WaitGroup
		// FAN-OUT: N workers parsing pdf's 1 page at a time.
		for range workers {
			wg.Add(1)
			go worker(ctx, db, pathsCh, ch, &wg)
		}

		wg.Wait()
		close(ch)
	}()

	return ch
}

func worker(
	ctx context.Context,
	db *sql.DB,
	paths <-chan string,
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

			metaData, _ := parser.GetMetadata(path)
			totalPages, _ := parser.GetTotalPages(path)

			tx, txerr := db.BeginTx(ctx, nil)

			if txerr != nil {
				log.Printf("Error occured while beginning transaction for parsing pdf in path %s \n %s ", path, txerr)
				events <- IndexEvent{EventType: EventError, FilePath: path, Err: txerr}
				continue
			}
			doc_id, err := store.InsertDocument(ctx, tx, metaData, path, totalPages)
			if err != nil {
				tx.Rollback()
				events <- IndexEvent{EventType: EventError, FilePath: path, Err: err}
				continue
			}

			err = parser.ParsePage(path, totalPages, func(p parser.Page) error {
				return store.SavePage(ctx, tx, doc_id, p)
			})

			if err != nil {
				log.Printf("Finished parsing PDF: %s (Status: Error - %v)", path, err)
				tx.Rollback()
				events <- IndexEvent{EventType: EventError, FilePath: path, Err: err}
				continue
			} else {
				log.Printf("Finished parsing PDF  , committing transaction %s", path)
				tx.Commit()
				//Finished so sent it to events loop?
				events <- IndexEvent{
					EventType: EventIndexed,
					FileName:  metaData.FileName,
					FilePath:  path,
				}
			}

			log.Printf("Finished parsing PDF: %s (Status: Success)", path)
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
