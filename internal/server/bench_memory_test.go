package server

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Sreyas-R/gopdex/internal/store"
)

func TestBenchmarkMemoryAndTime(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bench.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Force GC and record baseline memory
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	start := time.Now()

	indexed := 0
	errors := 0
	skipped := 0

	for ev := range Run(ctx, db, sampleDir, runtime.NumCPU()) {
		switch ev.EventType {
		case EventDiscovered:
			t.Logf("Discovered %d PDFs", ev.TotalPDF)
		case EventIndexed:
			indexed++
			t.Logf("Indexed: %s", filepath.Base(ev.FilePath))
		case EventSkipped:
			skipped++
		case EventError:
			errors++
			t.Logf("Error: %s - %v", ev.FilePath, ev.Err)
		}
	}

	elapsed := time.Since(start)

	// Force GC and measure peak memory
	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	t.Log("════════════════════════════════════════════")
	t.Log("         BENCHMARK RESULTS")
	t.Log("════════════════════════════════════════════")
	t.Logf("  Workers        : %d", runtime.NumCPU())
	t.Logf("  Total Time     : %v", elapsed)
	t.Logf("  Indexed        : %d", indexed)
	t.Logf("  Skipped        : %d", skipped)
	t.Logf("  Errors         : %d", errors)
	t.Log("────────────────────────────────────────────")
	t.Logf("  Heap Alloc     : %s", formatBytes(memAfter.HeapAlloc))
	t.Logf("  Total Alloc    : %s", formatBytes(memAfter.TotalAlloc))
	t.Logf("  Sys (OS RSS)   : %s", formatBytes(memAfter.Sys))
	t.Logf("  Heap Objects   : %d", memAfter.HeapObjects)
	t.Logf("  GC Cycles      : %d", memAfter.NumGC)
	t.Logf("  Mallocs        : %d", memAfter.Mallocs)
	t.Logf("  Frees          : %d", memAfter.Frees)
	t.Logf("  Δ TotalAlloc   : %s", formatBytes(memAfter.TotalAlloc-memBefore.TotalAlloc))
	t.Log("════════════════════════════════════════════")
}

func formatBytes(b uint64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
