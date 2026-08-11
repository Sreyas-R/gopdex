package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	parser "github.com/Sreyas-R/gopdex/internal/parser"

	_ "github.com/mattn/go-sqlite3"
)

// First time initialization of tables and triggers
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// SQLite disables foreign key enforcement by default, per connection.
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

// migrate creates all tables, indexes, virtual tables, and triggers inside a
// single transaction. Every statement is idempotent (IF NOT EXISTS / OR
// IGNORE-equivalent), so it's safe to call on every program startup.
func migrate(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() // no-op if Commit succeeds

	statements := []string{
		// 1. Documents table
		`CREATE TABLE IF NOT EXISTS documents (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			path         TEXT NOT NULL UNIQUE,
			file_name    TEXT NOT NULL,
			size         INTEGER NOT NULL,
			last_changed DATETIME NOT NULL,
			partial_hash TEXT NOT NULL,
			page_count   INTEGER NOT NULL,
			indexed_at   DATETIME NOT NULL
		);`,

		// 2. Pages table
		`CREATE TABLE IF NOT EXISTS pages (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			document_id INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			page_number INTEGER NOT NULL,
			text        TEXT NOT NULL,
			UNIQUE (document_id, page_number)
		);`,

		// 3. Index + FTS5 virtual table
		`CREATE INDEX IF NOT EXISTS idx_pages_document_id ON pages(document_id);`,

		`CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5(
			text,
			content = 'pages',
			content_rowid = 'id'
		);`,

		// 4. Triggers to keep pages_fts in sync with pages
		`CREATE TRIGGER IF NOT EXISTS pages_ai AFTER INSERT ON pages BEGIN
			INSERT INTO pages_fts(rowid, text) VALUES (new.id, new.text);
		END;`,

		`CREATE TRIGGER IF NOT EXISTS pages_ad AFTER DELETE ON pages BEGIN
			INSERT INTO pages_fts(pages_fts, rowid, text) VALUES ('delete', old.id, old.text);
		END;`,

		`CREATE TRIGGER IF NOT EXISTS pages_au AFTER UPDATE ON pages BEGIN
			INSERT INTO pages_fts(pages_fts, rowid, text) VALUES ('delete', old.id, old.text);
			INSERT INTO pages_fts(rowid, text) VALUES (new.id, new.text);
		END;`,
	}

	for _, stmt := range statements {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("exec statement %q: %w", stmt, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

// SaveDocument saves/updates a Document and its Pages in SQLite within a single transaction.
func SaveDocument(ctx context.Context, db *sql.DB, doc *parser.Document, path string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Handle re-indexing: delete existing document if present (ON DELETE CASCADE removes existing pages & triggers update FTS5)
	_, err = tx.ExecContext(ctx, `DELETE FROM documents WHERE path = ?`, path)
	if err != nil {
		return fmt.Errorf("delete old document: %w", err)
	}

	m := doc.Metadata
	docInsertQuery := `
		INSERT INTO documents (path, file_name, size, last_changed, partial_hash, page_count, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	res, err := tx.ExecContext(ctx, docInsertQuery, path, m.FileName, m.Size, m.LastChanged, m.PartialHash, doc.PageCount, time.Now())
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}

	docID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}

	// Bulk insert pages inside transaction
	pageStmt, err := tx.PrepareContext(ctx, `INSERT INTO pages (document_id, page_number, text) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare page stmt: %w", err)
	}
	defer pageStmt.Close()

	for _, page := range doc.Pages {
		if _, err := pageStmt.ExecContext(ctx, docID, page.Number, page.Text); err != nil {
			return fmt.Errorf("insert page %d: %w", page.Number, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

// GetDocumentByPath retrieves metadata for a document by path. Returns sql.ErrNoRows if not found.
func GetDocumentByPath(ctx context.Context, db *sql.DB, path string) (*parser.Document, error) {
	query := `
		SELECT file_name, size, last_changed, partial_hash, page_count
		FROM documents
		WHERE path = ?;
	`

	var m parser.Metadata
	var pageCount int

	err := db.QueryRowContext(ctx, query, path).Scan(
		&m.FileName,
		&m.Size,
		&m.LastChanged,
		&m.PartialHash,
		&pageCount,
	)
	if err != nil {
		return nil, err // Returns sql.ErrNoRows if not found
	}

	doc := &parser.Document{
		PageCount: pageCount,
		Metadata:  m,
	}

	return doc, nil
}
