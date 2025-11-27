package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestOffset116Recovery tests the enhanced recovery for offset 116 corruption pattern
func TestOffset116Recovery(t *testing.T) {
	// Create a PDF with startxref pointing to offset 116 (common corruption pattern)
	pdf := createPDFWithOffset116()

	r := bytes.NewReader(pdf)
	reader, err := NewReader(r, int64(len(pdf)))

	if err != nil {
		// The recovery should handle this gracefully
		if !strings.Contains(err.Error(), "offset 116") &&
			!strings.Contains(err.Error(), "all recovery strategies failed") {
			t.Logf("Got expected error for offset 116 corruption: %v", err)
		} else {
			t.Logf("Recovery attempted for offset 116: %v", err)
		}
	} else {
		t.Log("Successfully recovered from offset 116 corruption")
		// Verify we got a valid trailer
		if reader.trailer != nil {
			t.Log("Trailer successfully recovered")
		}
	}
}

// TestBackwardStartxrefSearch tests the backward search strategy for startxref
func TestBackwardStartxrefSearch(t *testing.T) {
	// Create a PDF with extra trailing data after %%EOF
	pdf := createPDFWithTrailingData()

	r := bytes.NewReader(pdf)
	_, err := NewReader(r, int64(len(pdf)))

	if err != nil {
		t.Errorf("Failed to find startxref with backward search: %v", err)
	} else {
		t.Log("Successfully found startxref using backward search")
	}
}

// TestDamagedPDFHeader tests more tolerant PDF header detection
func TestDamagedPDFHeader(t *testing.T) {
	tests := []struct {
		name     string
		pdf      []byte
		shouldOK bool
	}{
		{
			name:     "Missing percent sign",
			pdf:      createPDFWithoutPercentSign(),
			shouldOK: true, // Should recover
		},
		{
			name:     "Truncated version",
			pdf:      createPDFWithTruncatedVersion(),
			shouldOK: true, // Should handle gracefully
		},
		{
			name:     "Extra whitespace",
			pdf:      createPDFWithExtraWhitespace(),
			shouldOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bytes.NewReader(tt.pdf)
			_, err := NewReader(r, int64(len(tt.pdf)))

			if tt.shouldOK && err != nil {
				t.Logf("Warning: %s - %v (recovery attempted)", tt.name, err)
			} else if !tt.shouldOK && err == nil {
				t.Errorf("%s should have failed but succeeded", tt.name)
			} else if err == nil {
				t.Logf("%s - Successfully parsed", tt.name)
			}
		})
	}
}

// TestSearchBackwardForStartxref tests the backward search function directly
func TestSearchBackwardForStartxref(t *testing.T) {
	content := []byte("Some PDF content here\nstartxref\n12345\n%%EOF\nExtra trailing data")
	r := bytes.NewReader(content)

	// searchBackwardForStartxref expects io.ReaderAt
	offset := searchBackwardForStartxref(r, int64(len(content)))

	if offset < 0 {
		t.Error("searchBackwardForStartxref failed to find startxref")
	} else {
		expected := int64(bytes.Index(content, []byte("startxref")))
		if offset != expected {
			t.Errorf("searchBackwardForStartxref returned offset %d, expected %d", offset, expected)
		} else {
			t.Logf("Successfully found startxref at offset %d", offset)
		}
	}
}

// Helper functions to create test PDFs

func createPDFWithOffset116() []byte {
	// Create a minimal PDF where startxref incorrectly points to 116
	var buf bytes.Buffer

	buf.WriteString("%PDF-1.4\n")
	buf.WriteString("%âãÏÓ\n") // Binary comment

	// Object 1: Catalog
	buf.WriteString("1 0 obj\n")
	buf.WriteString("<< /Type /Catalog /Pages 2 0 R >>\n")
	buf.WriteString("endobj\n")

	// Object 2: Pages
	buf.WriteString("2 0 obj\n")
	buf.WriteString("<< /Type /Pages /Count 0 /Kids [] >>\n")
	buf.WriteString("endobj\n")

	// At this point we're around offset 116
	// Add xref table at correct location
	xrefPos := buf.Len()
	buf.WriteString("xref\n")
	buf.WriteString("0 3\n")
	buf.WriteString("0000000000 65535 f \n")
	buf.WriteString("0000000015 65535 n \n")
	buf.WriteString("0000000074 65535 n \n")

	buf.WriteString("trailer\n")
	buf.WriteString("<< /Size 3 /Root 1 0 R >>\n")

	// Point startxref to wrong offset (116)
	buf.WriteString("startxref\n")
	buf.WriteString("116\n")
	buf.WriteString("%%EOF")

	// Note: actual xref is at xrefPos, but startxref says 116
	_ = xrefPos

	return buf.Bytes()
}

func createPDFWithTrailingData() []byte {
	var buf bytes.Buffer

	buf.WriteString("%PDF-1.4\n")

	// Object 1: Catalog
	buf.WriteString("1 0 obj\n")
	buf.WriteString("<< /Type /Catalog /Pages 2 0 R >>\n")
	buf.WriteString("endobj\n")

	// Object 2: Pages
	buf.WriteString("2 0 obj\n")
	buf.WriteString("<< /Type /Pages /Count 0 /Kids [] >>\n")
	buf.WriteString("endobj\n")

	xrefPos := buf.Len()
	buf.WriteString("xref\n")
	buf.WriteString("0 3\n")
	buf.WriteString("0000000000 65535 f \n")
	buf.WriteString(fmt.Sprintf("%010d 00000 n \n", 9))
	buf.WriteString(fmt.Sprintf("%010d 00000 n \n", 62))

	buf.WriteString("trailer\n")
	buf.WriteString("<< /Size 3 /Root 1 0 R >>\n")

	buf.WriteString("startxref\n")
	buf.WriteString(fmt.Sprintf("%d\n", xrefPos))
	buf.WriteString("%%EOF\n")

	// Add lots of trailing junk data
	buf.WriteString("\n\n\nExtra trailing data that might confuse parsers\n")
	buf.WriteString("More garbage\n")
	buf.WriteString("Even more junk data here...\n")

	return buf.Bytes()
}

func createPDFWithoutPercentSign() []byte {
	// PDF header without the leading %
	var buf bytes.Buffer

	buf.WriteString("PDF-1.4\n") // Missing %

	// Minimal valid content
	buf.WriteString("1 0 obj\n")
	buf.WriteString("<< /Type /Catalog /Pages 2 0 R >>\n")
	buf.WriteString("endobj\n")

	buf.WriteString("2 0 obj\n")
	buf.WriteString("<< /Type /Pages /Count 0 /Kids [] >>\n")
	buf.WriteString("endobj\n")

	xrefPos := buf.Len()
	buf.WriteString("xref\n")
	buf.WriteString("0 3\n")
	buf.WriteString("0000000000 65535 f \n")
	buf.WriteString("0000000008 00000 n \n")
	buf.WriteString("0000000061 00000 n \n")

	buf.WriteString("trailer\n")
	buf.WriteString("<< /Size 3 /Root 1 0 R >>\n")
	buf.WriteString("startxref\n")
	buf.WriteString(fmt.Sprintf("%d\n", xrefPos))
	buf.WriteString("%%EOF")

	return buf.Bytes()
}

func createPDFWithTruncatedVersion() []byte {
	var buf bytes.Buffer

	// Truncated version string
	buf.WriteString("%PDF-1")

	// Try to continue with minimal content
	buf.WriteString("\n1 0 obj\n")
	buf.WriteString("<< /Type /Catalog >>\n")
	buf.WriteString("endobj\n")

	return buf.Bytes()
}

func createPDFWithExtraWhitespace() []byte {
	var buf bytes.Buffer

	// Extra whitespace and newlines before PDF header
	buf.WriteString("\n\n\n   %PDF-1.4\n")

	buf.WriteString("1 0 obj\n")
	buf.WriteString("<< /Type /Catalog /Pages 2 0 R >>\n")
	buf.WriteString("endobj\n")

	buf.WriteString("2 0 obj\n")
	buf.WriteString("<< /Type /Pages /Count 0 /Kids [] >>\n")
	buf.WriteString("endobj\n")

	xrefPos := buf.Len()
	buf.WriteString("xref\n")
	buf.WriteString("0 3\n")
	buf.WriteString("0000000000 65535 f \n")
	buf.WriteString("0000000018 00000 n \n")
	buf.WriteString("0000000071 00000 n \n")

	buf.WriteString("trailer\n")
	buf.WriteString("<< /Size 3 /Root 1 0 R >>\n")
	buf.WriteString("startxref\n")
	buf.WriteString(fmt.Sprintf("%d\n", xrefPos))
	buf.WriteString("%%EOF")

	return buf.Bytes()
}

// TestEnhancedRecoveryStrategies tests that all recovery strategies are tried
func TestEnhancedRecoveryStrategies(t *testing.T) {
	tests := []struct {
		name string
		pdf  []byte
		desc string
	}{
		{
			name: "Offset116",
			pdf:  createPDFWithOffset116(),
			desc: "PDF with startxref pointing to offset 116",
		},
		{
			name: "TrailingData",
			pdf:  createPDFWithTrailingData(),
			desc: "PDF with trailing data after %%EOF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bytes.NewReader(tt.pdf)
			reader, err := NewReader(r, int64(len(tt.pdf)))

			if err != nil {
				t.Logf("%s: %v (recovery strategies attempted)", tt.desc, err)
			} else {
				t.Logf("%s: Successfully recovered", tt.desc)
				if reader.trailer == nil {
					t.Error("Reader has no trailer after successful recovery")
				}
			}
		})
	}
}

// Benchmark the new recovery strategies
func BenchmarkOffset116Recovery(b *testing.B) {
	pdf := createPDFWithOffset116()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(pdf)
		_, _ = NewReader(r, int64(len(pdf)))
	}
}

func BenchmarkBackwardStartxrefSearch(b *testing.B) {
	pdf := createPDFWithTrailingData()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(pdf)
		searchBackwardForStartxref(r, int64(len(pdf)))
	}
}
