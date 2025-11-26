package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"testing"
)

// createPrefixedXrefStreamPDF builds a minimal PDF 1.5 file with an XRef stream
// and allows callers to insert arbitrary bytes before the %PDF header (e.g., BOM/whitespace).
func createPrefixedXrefStreamPDF(prefix []byte) []byte {
	var buf bytes.Buffer

	buf.Write(prefix)
	buf.WriteString("%PDF-1.5\n")
	buf.WriteString("%\x80\x80\x80\x80\n")

	// Object 1: Catalog
	obj1Offset := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	// Object 2: Pages
	obj2Offset := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	// Object 3: XRef stream
	xrefOffset := buf.Len()

	xrefData := []byte{
		0, 0, 0, 255, // free entry
		1, byte(obj1Offset >> 8), byte(obj1Offset), 0,
		1, byte(obj2Offset >> 8), byte(obj2Offset), 0,
		1, byte(xrefOffset >> 8), byte(xrefOffset), 0,
	}

	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	w.Write(xrefData)
	w.Close()

	fmt.Fprintf(&buf, "3 0 obj\n<< /Type /XRef /Size 4 /W [1 2 1] /Root 1 0 R /Length %d /Filter /FlateDecode >>\n", compressed.Len())
	buf.WriteString("stream\n")
	buf.Write(compressed.Bytes())
	buf.WriteString("\nendstream\nendobj\n")

	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xrefOffset)

	return buf.Bytes()
}

func TestNewReaderAllowsLeadingBOMAndWhitespace(t *testing.T) {
	prefix := []byte{0xEF, 0xBB, 0xBF, ' ', '\t', '\n'}
	pdf := createPrefixedXrefStreamPDF(prefix)

	r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		t.Fatalf("expected PDF to open with leading BOM/whitespace, got error: %v", err)
	}
	defer r.Close()

	if r.Trailer().IsNull() {
		t.Fatal("trailer should not be null")
	}
}

func TestNewReaderFindsHeaderAfterPadding(t *testing.T) {
	prefix := bytes.Repeat([]byte{' '}, 2048)
	pdf := createPrefixedXrefStreamPDF(prefix)

	r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		t.Fatalf("expected PDF to open with long prefix, got error: %v", err)
	}
	defer r.Close()

	if r.Trailer().IsNull() {
		t.Fatal("trailer should not be null")
	}
}
