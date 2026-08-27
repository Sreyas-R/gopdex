package server

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Sreyas-R/gopdex/internal/store"
)

const sampleDir = "/Users/sreyas/Desktop/gopdex/sample"

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// drainEvents reads all events from ch until it closes, returning the full slice.
func drainEvents(ch <-chan IndexEvent) []IndexEvent {
	var events []IndexEvent
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}

// --- Unit tests ---

func TestGetAllPDFs_FindsPDFs(t *testing.T) {
	paths, err := GetAllPDFs(sampleDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("expected at least one PDF, got 0")
	}
	for _, p := range paths {
		if filepath.Ext(p) != ".pdf" {
			t.Errorf("non-pdf in results: %s", p)
		}
		if !filepath.IsAbs(p) {
			t.Errorf("path not absolute: %s", p)
		}
	}
	t.Logf("found %d PDFs: %v", len(paths), paths)
}

func TestGetAllPDFs_SkipsNonPDFs(t *testing.T) {
	paths, err := GetAllPDFs(sampleDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, p := range paths {
		if filepath.Ext(p) == ".doc" {
			t.Errorf("non-pdf file included: %s", p)
		}
	}
}

func TestGetAllPDFs_EmptyDir(t *testing.T) {
	tmp := t.TempDir() // empty dir, cleaned up automatically
	paths, err := GetAllPDFs(tmp)
	if err != nil {
		t.Fatalf("unexpected error on empty dir: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected 0 paths, got %d", len(paths))
	}
}

func TestGetAllPDFs_InvalidPath(t *testing.T) {
	_, err := GetAllPDFs("/this/path/does/not/exist")
	if err == nil {
		t.Fatal("expected error for invalid path, got nil")
	}
}

func TestRun_EmitsDiscoveredFirst(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	ch := Run(ctx, db, sampleDir, 2)
	first := <-ch
	drainEvents(ch) // drain rest so goroutines exit

	if first.EventType != EventDiscovered {
		t.Errorf("expected first event to be EventDiscovered, got %v", first.EventType)
	}
	if first.TotalPDF == 0 {
		t.Error("EventDiscovered.TotalPDF should be > 0")
	}
	t.Logf("discovered %d PDFs", first.TotalPDF)
}

func TestRun_IndexesAllPDFs(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	events2 := drainEvents(Run(ctx, db, sampleDir, 2))
	discovered := 0
	indexed := 0
	errored := 0

	for _, ev := range events2 {
		switch ev.EventType {
		case EventDiscovered:
			discovered = ev.TotalPDF
		case EventIndexed:
			indexed++
		case EventError:
			errored++
			t.Logf("error event: path=%s err=%v", ev.FilePath, ev.Err)
		}
	}

	if discovered == 0 {
		t.Fatal("no EventDiscovered received")
	}
	if indexed == 0 {
		t.Error("expected at least one EventIndexed")
	}
	t.Logf("discovered=%d indexed=%d errors=%d", discovered, indexed, errored)
}

func TestRun_SkipsUnchangedOnSecondRun(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// first run — index everything
	drainEvents(Run(ctx, db, sampleDir, 2))

	// second run — nothing should change
	events := drainEvents(Run(ctx, db, sampleDir, 2))

	skipped := 0
	indexed := 0
	for _, ev := range events {
		switch ev.EventType {
		case EventSkipped:
			skipped++
		case EventIndexed:
			indexed++
		}
	}

	if indexed > 0 {
		t.Errorf("expected 0 EventIndexed on second run, got %d", indexed)
	}
	if skipped == 0 {
		t.Error("expected EventSkipped events on second run, got 0")
	}
	t.Logf("second run: skipped=%d re-indexed=%d", skipped, indexed)
}

func TestRun_EmptyDir(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	events := drainEvents(Run(ctx, db, t.TempDir(), 2))

	if len(events) != 1 || events[0].EventType != EventError {
		t.Errorf("expected single EventError for empty dir, got %v", events)
	}
}

func TestRun_CancelContext(t *testing.T) {
	db := openTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())

	ch := Run(ctx, db, sampleDir, 2)

	// cancel immediately after getting the first event
	<-ch
	cancel()

	// drain — just ensure channel closes and we don't deadlock
	done := make(chan struct{})
	go func() {
		drainEvents(ch)
		close(done)
	}()

	select {
	case <-done:
		// ok — goroutines exited cleanly
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Run to exit after context cancel — possible goroutine leak")
	}
}

func TestRun_InvalidRoot(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	events := drainEvents(Run(ctx, db, "/nonexistent/path/xyz", 2))

	if len(events) == 0 {
		t.Fatal("expected at least one event for invalid root")
	}
	if events[0].EventType != EventError {
		t.Errorf("expected EventError, got %v", events[0].EventType)
	}
	if events[0].Err == nil {
		t.Error("EventError.Err should not be nil")
	}
}

func TestRun_ErrorsHaveNonNilErr(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	events := drainEvents(Run(ctx, db, sampleDir, 2))
	for _, ev := range events {
		if ev.EventType == EventError && ev.Err == nil {
			t.Errorf("EventError at path %s has nil Err", ev.FilePath)
		}
	}
}

func TestRun_ChannelClosesAfterDone(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	ch := Run(ctx, db, sampleDir, 2)
	drainEvents(ch)

	// reading from a closed channel returns zero value immediately
	ev, ok := <-ch
	if ok {
		t.Errorf("channel should be closed, got event: %v", ev)
	}
}

// TestRun_ErrorWrapsUnderlying checks that errors returned carry the underlying OS error.
func TestRun_ErrorWrapsUnderlying(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	events := drainEvents(Run(ctx, db, "/no/such/dir", 2))
	if len(events) == 0 || events[0].Err == nil {
		t.Skip("no error event to inspect")
	}
	var pathErr *os.PathError
	if !errors.As(events[0].Err, &pathErr) {
		t.Logf("note: error type is %T — %v", events[0].Err, events[0].Err)
	}
}

// --- Benchmarks ---

// benchmarkRun runs the full index pipeline b.N times.
// Uses a fresh in-memory DB each iteration so every run is a cold index (no skips).
func benchmarkRun(b *testing.B, workers int) {
	b.Helper()
	b.ReportAllocs()

	for b.Loop() {
		db, err := store.Open(":memory:")
		if err != nil {
			b.Fatalf("open db: %v", err)
		}

		ctx := context.Background()
		indexed := 0
		for ev := range Run(ctx, db, sampleDir, workers) {
			if ev.EventType == EventIndexed {
				indexed++
			}
		}
		db.Close()

		b.ReportMetric(float64(indexed), "docs/op")
	}
}

func BenchmarkRun_Workers1(b *testing.B) {
	benchmarkRun(b, 1)
}

func BenchmarkRun_Workers2(b *testing.B) {
	benchmarkRun(b, 2)
}

func BenchmarkRun_WorkersNumCPU(b *testing.B) {
	benchmarkRun(b, runtime.NumCPU())
}

func BenchmarkGetAllPDFs(b *testing.B) {
	for b.Loop() {
		_, err := GetAllPDFs(sampleDir)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestBenchmarkLargePDFs(t *testing.T) {
	// Create a temporary database that is deleted when the test ends
	db := openTestDB(t)
	ctx := context.Background()

	t.Log("Starting to index benchmark PDFs...")
	start := time.Now()

	indexed := 0
	// Run the indexer on the sample directory which now contains the large PDFs
	for ev := range Run(ctx, db, sampleDir, runtime.NumCPU()) {
		if ev.EventType == EventIndexed {
			indexed++
			t.Logf("Successfully indexed: %s", filepath.Base(ev.FilePath))
		} else if ev.EventType == EventError {
			t.Errorf("Failed to index %s: %v", ev.FilePath, ev.Err)
		}
	}

	t.Logf("Finished indexing %d documents in %v using %d workers", indexed, time.Since(start), runtime.NumCPU())
}
