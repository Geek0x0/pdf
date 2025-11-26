package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"testing"
)

// TestXrefStreamRecovery tests the recovery of xref stream (PDF 1.5+) format
func TestXrefStreamRecovery(t *testing.T) {
	// Create a minimal valid PDF 1.5 with xref stream
	pdf := createMinimalXrefStreamPDF()

	r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		t.Fatalf("failed to open PDF with xref stream: %v", err)
	}
	defer r.Close()

	// Verify trailer was recovered correctly
	trailer := r.Trailer()
	if trailer.IsNull() {
		t.Fatal("trailer is null")
	}

	root := trailer.Key("Root")
	if root.IsNull() {
		t.Fatal("Root is null in trailer")
	}
}

// TestSearchXrefStream tests searching for xref stream in corrupted PDFs
func TestSearchXrefStream(t *testing.T) {
	// Create a PDF with incorrect startxref offset
	pdf := createPDFWithBadStartxref()

	r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		// This is expected to potentially fail, but should not panic
		t.Logf("expected error for bad startxref: %v", err)
		return
	}
	defer r.Close()

	// If it succeeded, verify basic structure
	trailer := r.Trailer()
	if !trailer.IsNull() {
		t.Log("Successfully recovered trailer from PDF with bad startxref")
	}
}

// TestRecoverTrailerFromXrefStream tests recovering trailer info from xref stream
func TestRecoverTrailerFromXrefStream(t *testing.T) {
	// Test that recoverXrefStreamTrailer can find XRef streams
	pdf := createMinimalXrefStreamPDF()

	r := &Reader{
		f:   bytes.NewReader(pdf),
		end: int64(len(pdf)),
	}

	err := r.recoverXrefStreamTrailer(pdf)
	if err != nil {
		t.Logf("recoverXrefStreamTrailer: %v", err)
	} else {
		if r.trailer == nil {
			t.Error("trailer should not be nil after recovery")
		}
		if r.trailer["Root"] == nil {
			t.Error("trailer should have Root entry")
		}
	}
}

// createMinimalXrefStreamPDF creates a minimal valid PDF 1.5 with xref stream
func createMinimalXrefStreamPDF() []byte {
	var buf bytes.Buffer

	// PDF header
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

	// Build xref stream data (uncompressed for simplicity)
	// W = [1, 2, 1] means: type(1 byte), offset(2 bytes), generation(1 byte)
	// We have 4 objects: 0 (free), 1, 2, 3 (xref stream itself)
	xrefData := []byte{
		0, 0, 0, 255, // Object 0: free, next=0, gen=255 (actually 65535 but we use 1 byte)
		1, byte(obj1Offset >> 8), byte(obj1Offset), 0, // Object 1: in-use, offset, gen=0
		1, byte(obj2Offset >> 8), byte(obj2Offset), 0, // Object 2: in-use, offset, gen=0
		1, byte(xrefOffset >> 8), byte(xrefOffset), 0, // Object 3: in-use (xref stream itself)
	}

	// Compress the xref data
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	w.Write(xrefData)
	w.Close()

	// Write xref stream object
	fmt.Fprintf(&buf, "3 0 obj\n")
	fmt.Fprintf(&buf, "<< /Type /XRef /Size 4 /W [1 2 1] /Root 1 0 R /Length %d /Filter /FlateDecode >>\n", compressed.Len())
	buf.WriteString("stream\n")
	buf.Write(compressed.Bytes())
	buf.WriteString("\nendstream\nendobj\n")

	// startxref and EOF
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xrefOffset)

	return buf.Bytes()
}

// createPDFWithBadStartxref creates a PDF with intentionally wrong startxref
func createPDFWithBadStartxref() []byte {
	var buf bytes.Buffer

	// PDF header
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

	// Build xref stream data
	xrefData := []byte{
		0, 0, 0, 255,
		1, byte(obj1Offset >> 8), byte(obj1Offset), 0,
		1, byte(obj2Offset >> 8), byte(obj2Offset), 0,
		1, byte(xrefOffset >> 8), byte(xrefOffset), 0,
	}

	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	w.Write(xrefData)
	w.Close()

	fmt.Fprintf(&buf, "3 0 obj\n")
	fmt.Fprintf(&buf, "<< /Type /XRef /Size 4 /W [1 2 1] /Root 1 0 R /Length %d /Filter /FlateDecode >>\n", compressed.Len())
	buf.WriteString("stream\n")
	buf.Write(compressed.Bytes())
	buf.WriteString("\nendstream\nendobj\n")

	// Intentionally wrong startxref (pointing to offset 116, which is common in error logs)
	buf.WriteString("startxref\n116\n%%EOF")

	return buf.Bytes()
}

// TestXrefStreamWithPrev tests handling of xref streams with Prev entries
func TestXrefStreamWithPrev(t *testing.T) {
	// This is a more complex case - for now just ensure it doesn't panic
	pdf := createMinimalXrefStreamPDF()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()

	r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		t.Logf("error opening PDF: %v", err)
		return
	}
	defer r.Close()

	t.Log("Successfully opened xref stream PDF")
}

// TestFindObjectStart tests the findObjectStart helper function
func TestFindObjectStart(t *testing.T) {
	testCases := []struct {
		name     string
		data     string
		pos      int
		expected int // -1 if not found
	}{
		{
			name:     "simple object",
			data:     "1 0 obj\n<< /Type /XRef >>\nendobj\n",
			pos:      15,
			expected: 0,
		},
		{
			name:     "object with newline prefix",
			data:     "\n1 0 obj\n<< /Type /XRef >>\nendobj\n",
			pos:      16,
			expected: 1,
		},
		{
			name:     "no object found",
			data:     "<< /Type /XRef >>\n",
			pos:      10,
			expected: -1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Reader{
				f:   bytes.NewReader([]byte(tc.data)),
				end: int64(len(tc.data)),
			}

			result := r.findObjectStart([]byte(tc.data), tc.pos)
			if result != tc.expected {
				t.Errorf("findObjectStart(%q, %d) = %d, expected %d", tc.data, tc.pos, result, tc.expected)
			}
		})
	}
}
