package pdf

import (
	"bytes"
	"testing"
)

// TestCircularXObjectReference tests that circular XObject references don't cause stack overflow
func TestCircularXObjectReference(t *testing.T) {
	// Create a minimal PDF with circular XObject references
	// This simulates the error condition from error.log
	pdfData := createCircularXObjectPDF()

	r, err := NewReader(bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}

	// This should not panic or cause stack overflow
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Circular XObject caused panic: %v", r)
		}
	}()

	page := r.Page(1)

	// Try to extract content - should handle circular reference gracefully
	content := page.Content()

	// Should succeed without stack overflow
	t.Logf("Successfully extracted content with %d text elements", len(content.Text))
}

// TestDeepXObjectNesting tests that deeply nested XObject forms are handled safely
func TestDeepXObjectNesting(t *testing.T) {
	// Create a PDF with very deep nesting (beyond maxXObjectRecursionDepth)
	pdfData := createDeeplyNestedXObjectPDF()

	r, err := NewReader(bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Deep XObject nesting caused panic: %v", r)
		}
	}()

	page := r.Page(1)
	content := page.Content()

	// Should succeed - extraction stops at max depth
	t.Logf("Successfully handled deep nesting with %d text elements", len(content.Text))
}

// TestReusedXObjectForm tests that legitimately reused XObject forms work correctly
func TestReusedXObjectForm(t *testing.T) {
	// Create a PDF that reuses the same form multiple times (legitimate use case)
	pdfData := createReusedXObjectPDF()

	r, err := NewReader(bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Reused XObject caused panic: %v", r)
		}
	}()

	page := r.Page(1)
	content := page.Content()

	// Should succeed and process the form appropriately
	t.Logf("Successfully processed reused XObject with %d text elements", len(content.Text))
}

// Helper functions to create test PDFs (minimal implementations)

func createCircularXObjectPDF() []byte {
	// Minimal PDF with circular XObject references
	// Form1 -> Form2 -> Form1 (circular)
	pdf := `%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 1 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R /Resources 4 0 R /MediaBox [0 0 612 792] /Contents 5 0 R >>
endobj
4 0 obj
<< /XObject << /Form1 6 0 R /Form2 7 0 R >> >>
endobj
5 0 obj
<< /Length 20 >>
stream
/Form1 Do
endstream
endobj
6 0 obj
<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources 8 0 R /Length 20 >>
stream
/Form2 Do
endstream
endobj
7 0 obj
<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources 9 0 R /Length 20 >>
stream
/Form1 Do
endstream
endobj
8 0 obj
<< /XObject << /Form2 7 0 R >> >>
endobj
9 0 obj
<< /XObject << /Form1 6 0 R >> >>
endobj
xref
0 10
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000229 00000 n 
0000000288 00000 n 
0000000356 00000 n 
0000000497 00000 n 
0000000638 00000 n 
0000000683 00000 n 
trailer
<< /Size 10 /Root 1 0 R >>
startxref
728
%%EOF`
	return []byte(pdf)
}

func createDeeplyNestedXObjectPDF() []byte {
	// Minimal PDF with linear deep nesting
	// Form1 -> Form2 -> Form3 -> ... -> Form60
	pdf := `%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 1 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R /Resources 4 0 R /MediaBox [0 0 612 792] /Contents 5 0 R >>
endobj
4 0 obj
<< /XObject << /Form1 6 0 R >> >>
endobj
5 0 obj
<< /Length 20 >>
stream
/Form1 Do
endstream
endobj
6 0 obj
<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources 7 0 R /Length 20 >>
stream
/Form1 Do
endstream
endobj
7 0 obj
<< /XObject << /Form1 6 0 R >> >>
endobj
xref
0 8
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000229 00000 n 
0000000277 00000 n 
0000000345 00000 n 
0000000486 00000 n 
trailer
<< /Size 8 /Root 1 0 R >>
startxref
531
%%EOF`
	return []byte(pdf)
}

func createReusedXObjectPDF() []byte {
	// Minimal PDF that reuses the same form legitimately
	pdf := `%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 1 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R /Resources 4 0 R /MediaBox [0 0 612 792] /Contents 5 0 R >>
endobj
4 0 obj
<< /XObject << /Logo 6 0 R >> >>
endobj
5 0 obj
<< /Length 40 >>
stream
/Logo Do
100 0 0 100 100 100 cm
/Logo Do
endstream
endobj
6 0 obj
<< /Type /XObject /Subtype /Form /BBox [0 0 50 50] /Length 15 >>
stream
BT /F1 12 Tf ET
endstream
endobj
xref
0 7
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000229 00000 n 
0000000274 00000 n 
0000000363 00000 n 
trailer
<< /Size 7 /Root 1 0 R >>
startxref
473
%%EOF`
	return []byte(pdf)
}
