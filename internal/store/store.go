package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	parser "github.com/Sreyas-R/gopdex/internal/parser"

	_ "github.com/mattn/go-sqlite3"
)

type SearchResult struct {
	DocumentID int64
	Path       string
	PageNumber int
	FileName   string
	Snippet    string
	Score      float64
}

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 5000; PRAGMA synchronous = NORMAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable pragmas: %w", err)
	}

	db.SetMaxOpenConns(2)
	db.SetConnMaxLifetime(0)

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	statements := []string{
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

		`CREATE TABLE IF NOT EXISTS pages (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			document_id INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			page_number INTEGER NOT NULL,
			text        TEXT NOT NULL,
			UNIQUE (document_id, page_number)
		);`,

		`CREATE INDEX IF NOT EXISTS idx_pages_document_id ON pages(document_id);`,

		`CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5(
			text,
			content = 'pages',
			content_rowid = 'id',
			tokenize = 'porter unicode61'
		);`,

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

	return tx.Commit()
}

// SaveDocument upserts a document and all its pages in one short-lived transaction.
// Old doc + pages are cascade-deleted first, then fresh rows inserted.
func SaveDocument(ctx context.Context, db *sql.DB, path string, meta parser.Metadata, totalPages int, pages []parser.Page) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `DELETE FROM documents WHERE path = ?`, path)
	if err != nil {
		return fmt.Errorf("delete old doc: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO documents (path, file_name, size, last_changed, partial_hash, page_count, indexed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		path, meta.FileName, meta.Size, meta.LastChanged, meta.PartialHash, totalPages, time.Now())
	if err != nil {
		return fmt.Errorf("insert doc: %w", err)
	}

	docID, _ := res.LastInsertId()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO pages (document_id, page_number, text) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, p := range pages {
		if _, err := stmt.ExecContext(ctx, docID, p.Number, p.Text); err != nil {
			return fmt.Errorf("insert page %d: %w", p.Number, err)
		}
	}

	return tx.Commit()
}

func SearchFTS(ctx context.Context, db *sql.DB, query string) ([]SearchResult, error) {
	searchQuery := `SELECT
					p.document_id,
					d.path,
					d.file_name,
					p.page_number,
					snippet(pages_fts, 0, '[', ']', '...', 12) AS snippet,
					bm25(pages_fts) AS score
				FROM pages_fts
				JOIN pages p ON p.id = pages_fts.rowid
				JOIN documents d ON p.document_id = d.id
				WHERE pages_fts MATCH ?
				ORDER BY score ASC
				LIMIT 5;
	`

	formattedQuery := fmt.Sprintf("\"%s\"", strings.ReplaceAll(query, "\"", "\"\""))
	rows, err := db.QueryContext(ctx, searchQuery, formattedQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []SearchResult
	for rows.Next() {
		var curr SearchResult
		if err := rows.Scan(&curr.DocumentID, &curr.Path, &curr.FileName, &curr.PageNumber, &curr.Snippet, &curr.Score); err != nil {
			return nil, err
		}
		res = append(res, curr)
	}

	return res, rows.Err()
}

func GetDocumentByPath(ctx context.Context, db *sql.DB, path string) (*parser.Document, error) {
	var m parser.Metadata
	var pageCount int

	err := db.QueryRowContext(ctx,
		`SELECT file_name, size, last_changed, partial_hash, page_count FROM documents WHERE path = ?`,
		path).Scan(&m.FileName, &m.Size, &m.LastChanged, &m.PartialHash, &pageCount)
	if err != nil {
		return nil, err
	}

	return &parser.Document{
		FilePath:  path,
		PageCount: pageCount,
		Metadata:  m,
	}, nil
}

// GetAllDocuments loads all indexed documents into a map keyed by path.
// One query replaces N individual GetDocumentByPath calls.
func GetAllDocuments(ctx context.Context, db *sql.DB) (map[string]*parser.Document, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT path, file_name, size, last_changed, partial_hash, page_count FROM documents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	docs := make(map[string]*parser.Document)
	for rows.Next() {
		var path string
		var m parser.Metadata
		var pageCount int
		if err := rows.Scan(&path, &m.FileName, &m.Size, &m.LastChanged, &m.PartialHash, &pageCount); err != nil {
			return nil, err
		}
		docs[path] = &parser.Document{
			FilePath:  path,
			PageCount: pageCount,
			Metadata:  m,
		}
	}
	return docs, rows.Err()
}
