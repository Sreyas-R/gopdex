package store

import (
	"context"
	"testing"
	"time"

	parser "github.com/Sreyas-R/gopdex/internal/parser"
)

func TestSaveAndGetDocument(t *testing.T) {
	db, err := Open("../../db/pdex.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	docPath := "/abs/path/to/test.pdf"

	doc := &parser.Document{
		PageCount: 2,
		Metadata: parser.Metadata{
			FileName:    docPath,
			Size:        1024,
			LastChanged: time.Now().Truncate(time.Second),
			PartialHash: "abc123hash",
		},
		Pages: []parser.Page{
			{Number: 1, Text: "Hello World on page 1"},
			{Number: 2, Text: "Testing FTS5 search on page 2"},
		},
	}

	err = SaveDocument(ctx, db, doc, docPath)
	if err != nil {
		t.Fatalf("SaveDocument failed: %v", err)
	}

	fetched, err := GetDocumentByPath(ctx, db, docPath)
	if err != nil {
		t.Fatalf("GetDocumentByPath failed: %v", err)
	}

	if fetched.PageCount != doc.PageCount {
		t.Errorf("expected PageCount %d, got %d", doc.PageCount, fetched.PageCount)
	}

	if fetched.Metadata.PartialHash != doc.Metadata.PartialHash {
		t.Errorf("expected Hash %s, got %s", doc.Metadata.PartialHash, fetched.Metadata.PartialHash)
	}
}
