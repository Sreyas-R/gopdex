package parser

import (
	"fmt"
	"testing"
)

func TestParser(t *testing.T) {
	filePath := "/Users/sreyas/Desktop/gopdex/sample"
	fileName := "lorem.pdf"
	fileComplete := filePath + "/" + fileName
	_, pages, err := ParseFile(fileComplete)

	if err != nil {
		t.Errorf("Error occured %s", err)
	}

	fmt.Println("total pages ", pages)
}
