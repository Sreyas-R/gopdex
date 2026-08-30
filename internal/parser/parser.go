package parser

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/chappihappymeal/pdf"
)

var hashBufPool = sync.Pool{
	New: func() any { return make([]byte, CHUNKSIZE) },
}

const CHUNKSIZE int64 = 1024 * 1024

type Metadata struct {
	FileName    string
	Size        int64
	LastChanged time.Time
	PartialHash string
}

type Document struct {
	PageCount int
	Pages     []Page
	Metadata  Metadata
	FilePath  string
}

type Page struct {
	Number int
	Text   string
}

func NeedsReindexing(path string, doc *Document) (bool, Metadata, error) {
	stats, err := os.Stat(path)
	if err != nil {
		return true, Metadata{}, err
	}

	name, _ := filepath.Abs(path)
	_, pdfName := filepath.Split(name)

	m := Metadata{
		FileName:    pdfName,
		Size:        stats.Size(),
		LastChanged: stats.ModTime(),
	}

	// First-time index: skip hash comparison, just compute for storage
	if doc == nil {
		m.PartialHash, _ = ComputePartialHash(path)
		return true, m, nil
	}

	// Tier 1: size/mtime changed → definitely need reindex
	if doc.Metadata.Size != m.Size || !doc.Metadata.LastChanged.Equal(m.LastChanged) {
		m.PartialHash, _ = ComputePartialHash(path)
		return true, m, nil
	}

	currHash, err := ComputePartialHash(path)
	if err != nil {
		return true, m, err
	}
	m.PartialHash = currHash

	if doc.Metadata.PartialHash != currHash {
		return true, m, nil
	}

	return false, m, nil
}

func ComputePartialHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", err
	}

	fsz := info.Size()
	hasher := sha256.New()

	sizeBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(sizeBuf, uint64(fsz))
	hasher.Write(sizeBuf)

	if fsz <= 2*CHUNKSIZE {
		if _, err := io.Copy(hasher, file); err != nil {
			return "", fmt.Errorf("failed to hash full file: %w", err)
		}
		return hex.EncodeToString(hasher.Sum(nil)), nil
	}

	buf := hashBufPool.Get().([]byte)
	defer hashBufPool.Put(buf)

	if _, err := file.ReadAt(buf, 0); err != nil {
		return "", fmt.Errorf("failed reading head: %w", err)
	}
	hasher.Write(buf)

	if _, err := file.ReadAt(buf, fsz-CHUNKSIZE); err != nil {
		return "", fmt.Errorf("failed reading tail: %w", err)
	}
	hasher.Write(buf)

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// ParseFile opens the PDF once to get totalPages, then parses in 50-page
func ParseFile(path string) ([]Page, int, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return nil, 0, err
	}
	totalPages := r.NumPage()
	f.Close()

	pages := make([]Page, 0, totalPages)
	batchSize := 50
	for batchStart := 1; batchStart <= totalPages; batchStart += batchSize {
		batchEnd := min(batchStart+batchSize-1, totalPages)

		f, r, err := pdf.Open(path)
		if err != nil {
			return pages, totalPages, err
		}

		for pageIdx := batchStart; pageIdx <= batchEnd; pageIdx++ {
			p := r.Page(pageIdx)
			if p.V.IsNull() {
				continue
			}

			text, _ := p.GetPlainText(nil)
			if text == "" {
				continue
			}

			pages = append(pages, Page{Number: pageIdx, Text: text})
		}
		f.Close()
	}

	return pages, totalPages, nil
}
