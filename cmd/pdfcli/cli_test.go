package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/Geek0x0/pdf"
)

// redirect output to avoid polluting test logs
func withTempOutput(fn func()) {
	stdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = stdout
	buf := new(bytes.Buffer)
	buf.ReadFrom(r)
}

func TestHandlersWithEmptyReader(t *testing.T) {
	reader := &pdf.Reader{}
	withTempOutput(func() {
		handlePlain(reader, 0)
		handleStyled(reader)
		handleText(reader, "")
		handleRows(reader, 1)
		handleColumns(reader, 1)
	})
	if !isReadable("this text has many words") {
		t.Fatalf("expected readable text")
	}
}
