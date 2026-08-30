package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	parser "github.com/Sreyas-R/gopdex/internal/parser"
)

func TestParseAndSave(t *testing.T) {
	fileComplete := "/Users/sreyas/Desktop/gopdex/sample/lorem.pdf"

	pages, totalPages, err := parser.ParseFile(fileComplete)
	if err != nil {
		t.Fatalf("Parsing document failed with err %v", err)
	}
	fmt.Printf("Parsed %d pages (total: %d)\n", len(pages), totalPages)

	_, meta, _ := parser.NeedsReindexing(fileComplete, nil)

	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Opening DB Connection failed with err %v", err)
	}
	defer db.Close()

	err = SaveDocument(ctx, db, fileComplete, meta, totalPages, pages)
	if err != nil {
		t.Fatalf("Error occured while saving the parsed document %v ", err)
	}

	doc, err := GetDocumentByPath(ctx, db, fileComplete)
	if err == sql.ErrNoRows {
		t.Fatal("No rows found after save")
	}
	if doc.PageCount != totalPages {
		t.Fatalf("Page count mismatch - fetched = %d , actual - %d", doc.PageCount, totalPages)
	}
}

func TestSaveAndGetDocument(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	docPath := "/abs/path/to/test.pdf"

	meta := parser.Metadata{
		FileName:    "test.pdf",
		Size:        1024,
		LastChanged: time.Now().Truncate(time.Second),
		PartialHash: "abc123hash",
	}
	pages := []parser.Page{
		{Number: 1, Text: "Hello World on page 1"},
		{Number: 2, Text: "Testing FTS5 search on page 2"},
	}

	err = SaveDocument(ctx, db, docPath, meta, 2, pages)
	if err != nil {
		t.Fatalf("SaveDocument failed: %v", err)
	}

	fetched, err := GetDocumentByPath(ctx, db, docPath)
	if err != nil {
		t.Fatalf("GetDocumentByPath failed: %v", err)
	}

	if fetched.PageCount != 2 {
		t.Errorf("expected PageCount 2, got %d", fetched.PageCount)
	}

	if fetched.Metadata.PartialHash != meta.PartialHash {
		t.Errorf("expected Hash %s, got %s", meta.PartialHash, fetched.Metadata.PartialHash)
	}
}

func TestSearchFTS(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	docPath := "/abs/path/to/search_test.pdf"

	meta := parser.Metadata{
		FileName:    "search_test.pdf",
		Size:        2048,
		LastChanged: time.Now().Truncate(time.Second),
		PartialHash: "searchhash123",
	}
	pages := []parser.Page{
		{Number: 1, Text: "Private Go indexing engine search test"},
		{Number: 2, Text: "SQLite FTS5 full text BM25 ranking algorithm snippet preview"},
	}

	if err := SaveDocument(ctx, db, docPath, meta, 2, pages); err != nil {
		t.Fatalf("SaveDocument failed: %v", err)
	}

	results, err := SearchFTS(ctx, db, "algorithm")
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}
	fmt.Printf("Search results = %v \n", results)

	if len(results) == 0 {
		t.Fatalf("expected search results for query 'algorithm', got 0")
	}
	if results[0].PageNumber != 2 {
		t.Errorf("expected match on PageNumber 2, got %d", results[0].PageNumber)
	}
	if results[0].FileName != "search_test.pdf" {
		t.Errorf("expected FileName 'search_test.pdf', got %s", results[0].FileName)
	}
}
