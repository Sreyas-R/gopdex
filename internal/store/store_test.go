package store

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	parser "github.com/Sreyas-R/gopdex/internal/parser"
)

func TestParseAndSave(t *testing.T) {
	filePath := "/Users/sreyas/Desktop/gopdex/sample"
	fileName := "lorem.pdf"
	fileComplete := filePath + "/" + fileName
	d, err := parser.Parse(fileComplete)
	fmt.Printf("Doc parsing done - details %v \n", d.Metadata)
	if err != nil {
		t.Fatalf("Parsing document failed with err %v", err)
	}

	needsParsing, err := parser.NeedsReindexing(&d)
	if needsParsing || err != nil {
		t.Fatalf("Reparsing required for parsed document - %s", err)
	}
	ctx := context.Background()

	db, err := Open("../../db/pdex.db")
	if err != nil {
		t.Fatalf("Opening DB Connection failed with err %v", err)
	}

	err = SaveDocument(ctx, db, &d, fileComplete)
	if err != nil {
		t.Fatalf("Error occured while saving the parsed document %v ", err)
	}

	doc, err := GetDocumentByPath(ctx, db, fileComplete)
	if err == sql.ErrNoRows {
		fmt.Printf("No rows or docs there breh")
	}
	if doc.PageCount != d.PageCount {
		t.Fatalf("Page count mismatch - fetched = %d , actual - %d \n", doc.PageCount, d.PageCount)
	}

}

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
		FilePath:  docPath,
		Metadata: parser.Metadata{
			FileName:    "test.pdf",
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
