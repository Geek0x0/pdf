package pdf

import (
	"bytes"
	"testing"
)

// Ensure truncated xref does not trigger panic, but returns a handleable error.
func TestNewReaderTruncatedXrefNoPanic(t *testing.T) {
	data := []byte("%PDF-1.4\nxref\n0 1\n") // intentionally truncated xref table
	data = append(data, []byte("startxref\n0\n%%EOF")...)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()

	_, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err == nil {
		t.Fatalf("expected error for truncated xref")
	}
}
