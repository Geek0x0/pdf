package pdf

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

// Ensure xref Prev chains that reference themselves are detected instead of looping forever.
func TestXrefPrevCycleDetected(t *testing.T) {
	base := "%PDF-1.4\n"
	xrefOffset := len(base)

	xrefSection := fmt.Sprintf(
		"xref\n0 1\n0000000000 65535 f \ntrailer\n<< /Size 1 /Root 1 0 R /Prev %d >>\nstartxref\n%d\n%%EOF\n",
		xrefOffset,
		xrefOffset,
	)

	pdfData := []byte(base + xrefSection)

	reader := bytes.NewReader(pdfData)
	buf := newBuffer(io.NewSectionReader(reader, int64(xrefOffset), int64(len(pdfData))-int64(xrefOffset)), int64(xrefOffset))
	r := &Reader{
		f:              reader,
		end:            int64(len(pdfData)),
		fontCache:      NewFontCache(),
		cacheCap:       2000,
		objStreamCache: make(map[uint32]map[int64]int64),
	}

	_, _, _, err := readXref(r, buf)
	if err == nil {
		t.Fatalf("expected error for xref Prev self-cycle")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got: %v", err)
	}
}
