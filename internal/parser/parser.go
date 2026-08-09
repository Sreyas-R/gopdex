package parser

//Extract text's from pdf's

//this will take in pdf by path one at a time or through a goroutine and return the text
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

// When checking if file is modified , first we check size and date has been changed , then compute the hash and verify
type Metadata struct {
	fileName    string
	size        int64
	lastChanged time.Time
	hash64      string
}
type Document struct {
	numbers  int
	pages    []Page
	metadata Metadata
}

type Page struct {
	Number int
	text   string
}

// Call this from main loop initially to check if the file needs reindexing / parsing
// doc is value returned from sqlite fetch
func needsReindexing(doc *Document, path string) (bool, error) {
	if doc == nil {
		return true, nil
	}

	//Exists in DB , check if modified
	lastModified := doc.metadata.lastChanged
	lastSize := doc.metadata.size

	stats, err := os.Stat(path)
	if err != nil {
		return true, err
	}
	if lastSize != stats.Size() || !(stats.ModTime().Equal(lastModified)) {
		return true, nil
	}

	//Check partial hash to confirm if changed
	lastHash := doc.metadata.hash64
	currHash, err := computePartialHash(path)

	if err != nil {
		return true, err
	}
	return (lastHash == currHash), nil

}
func getMetadata(path string) (Metadata, error) {
	stats, err := os.Stat(path)
	if err != nil {
		return Metadata{}, err //Some error occured while fetching stats , we are going to consider this as a new entry
	}
	name, err := filepath.Abs(path)
	if err != nil {
		return Metadata{}, err
	}

	m := Metadata{
		fileName:    name,
		size:        stats.Size(),
		lastChanged: stats.ModTime(),
	}

	hash, err := computePartialHash(path)
	m.hash64 = hash

	return m, nil
}

func computePartialHash(path string) (string, error) {
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

	if _, err := io.ReadFull(file, buf); err != nil {
		return "", fmt.Errorf("failed reading head: %w", err)
	}
	hasher.Write(buf)

	if _, err := file.Seek(fsz-CHUNKSIZE, io.SeekStart); err != nil {
		return "", fmt.Errorf("failed seeking tail: %w", err)
	}
	if _, err := io.ReadFull(file, buf); err != nil {
		return "", fmt.Errorf("failed reading tail: %w", err)
	}
	hasher.Write(buf)

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// Parse's PDF and returns a Document
// This function should not be called concurrently with getMetadata to avoid multiple file.Open()
func Parse(path string) (Document, error) {
	//First we check if this file has already been parsed before , and exists in the user's disk to reduce compuattion and disk i/o
	pdf.DebugOn = true
	f, r, err := pdf.Open(path)
	if err != nil {
		return Document{}, err
	}
	//metadata, isExists := checkFile(path, f)
	defer f.Close()
	fileName := f.Name()
	totalPages := r.NumPage()

	fmt.Printf("FileName = %s Pages = %d \n", fileName, totalPages)
	pageContents := make([]Page, totalPages)

	for pageIdx := 1; pageIdx <= totalPages; pageIdx++ {
		p := r.Page(pageIdx)

		if p.V.IsNull() {
			continue //Page contains nothin
		}
		contents, err := p.GetPlainText(nil)
		if err != nil {
			return Document{}, err
		}

		page := Page{
			Number: pageIdx,
			text:   contents,
		}

		pageContents = append(pageContents, page)
		fmt.Printf("Page Number = %d  pageContents = %s \n\n", pageIdx, contents)
	}
	metadata, err := getMetadata(path)
	if err != nil {
		fmt.Println("Error occured while computing the metadata ", err)
	}
	d := Document{
		metadata: metadata,
		numbers:  totalPages,
		pages:    pageContents,
	}

	return d, nil
}
