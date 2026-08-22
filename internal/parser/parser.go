package parser

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ledongthuc/pdf"
)

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

func NeedsReindexing(doc *Document) (bool, error) {
	if doc == nil {
		return true, nil
	}
	path := doc.FilePath
	lastModified := doc.Metadata.LastChanged
	lastSize := doc.Metadata.Size

	stats, err := os.Stat(path)
	if err != nil {
		return true, err
	}
	if lastSize != stats.Size() || !(stats.ModTime().Equal(lastModified)) {
		return true, nil
	}

	lastHash := doc.Metadata.PartialHash
	currHash, err := ComputePartialHash(path)
	if err != nil {
		return true, err
	}
	return (lastHash != currHash), nil
}

func GetMetadata(path string) (Metadata, error) {
	stats, err := os.Stat(path)
	if err != nil {
		return Metadata{}, err
	}
	name, err := filepath.Abs(path)
	_, p := filepath.Split(name)
	pdfName := p
	if err != nil {
		return Metadata{}, err
	}
	hash, err := ComputePartialHash(path)
	if err != nil {
		return Metadata{}, err
	}

	m := Metadata{
		FileName:    pdfName,
		Size:        stats.Size(),
		LastChanged: stats.ModTime(),
		PartialHash: hash,
	}

	return m, nil
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

	buf := make([]byte, CHUNKSIZE)

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

func Parse(path string) (Document, error) {
	// pdf.DebugOn = true
	f, r, err := pdf.Open(path)
	if err != nil {
		return Document{}, err
	}
	defer f.Close()

	totalPages := r.NumPage()
	pageContents := make([]Page, 0, totalPages)

	for pageIdx := 1; pageIdx <= totalPages; pageIdx++ {
		p := r.Page(pageIdx)

		if p.V.IsNull() {
			continue
		}
		contents, err := p.GetPlainText(nil)
		if err != nil {
			return Document{}, err
		}

		page := Page{
			Number: pageIdx,
			Text:   contents,
		}

		pageContents = append(pageContents, page)
	}

	metadata, err := GetMetadata(path)
	if err != nil {
		return Document{}, fmt.Errorf("failed to compute metadata: %w", err)
	}

	d := Document{
		FilePath:  path,
		Metadata:  metadata,
		PageCount: totalPages,
		Pages:     pageContents,
	}

	return d, nil
}
