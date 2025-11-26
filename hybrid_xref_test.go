package pdf

import (
	"bytes"
	"fmt"
	"testing"
)

func TestHybridXref(t *testing.T) {
	// Construct a hybrid PDF
	// Object 1: Catalog
	// Object 2: Pages
	// Object 3: Page
	// Object 4: Content Stream (in XRef table)
	// Object 5: Font (in XRef Stream)

	// Adjust offsets manually or use a builder?
	// Manual adjustment is error prone.
	// Let's try to be approximate and see if it parses at all,
	// but exact offsets are needed for xref.

	// Let's use a simpler approach:
	// Create a Reader with a mock file that returns this content.
	// But offsets must be correct.

	// Re-calculating offsets:
	// Header: 9 bytes (%PDF-1.5\n) -> 0-8
	// Obj 1: 10
	// Obj 2: 60
	// Obj 3: 110
	// Obj 4: 230
	// Obj 5: 280 (This is the one in XRefStm) -> Wait, Obj 5 is at 280 in file, but we want it in XRefStm.
	// Let's put Obj 5 in the file, but NOT in the main xref table.
	// Obj 6 (XRefStm): 350

	// Let's write a helper to calculate offsets.

	var buf bytes.Buffer
	write := func(s string) int64 {
		off := int64(buf.Len())
		buf.WriteString(s)
		return off
	}

	write("%PDF-1.5\n")
	obj1 := write("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	obj2 := write("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	obj3 := write("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")
	obj4 := write("4 0 obj\n<< /Length 10 >>\nstream\n(Hello) Tj\nendstream\nendobj\n")
	obj5 := write("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	// XRef Stream for Obj 5
	// Index [5 1] means starting at obj 5, count 1.
	// W [1 2 1]
	// Entry: Type 1 (offset), Offset (2 bytes), Gen (1 byte)
	// 01 <obj5_off> 00

	xrefStmStart := int64(buf.Len())
	// We need to construct the stream content first to get length
	stmContent := []byte{0x01, byte(obj5 >> 8), byte(obj5), 0x00}

	write(fmt.Sprintf("6 0 obj\n<< /Type /XRef /Size 7 /W [1 2 1] /Index [5 1] /Length %d >>\nstream\n", len(stmContent)))
	buf.Write(stmContent)
	write("\nendstream\nendobj\n")

	xrefTableStart := write("xref\n0 5\n0000000000 65535 f \n")
	write(fmt.Sprintf("%010d 00000 n \n", obj1))
	write(fmt.Sprintf("%010d 00000 n \n", obj2))
	write(fmt.Sprintf("%010d 00000 n \n", obj3))
	write(fmt.Sprintf("%010d 00000 n \n", obj4))

	write(fmt.Sprintf("trailer\n<< /Size 7 /Root 1 0 R /XRefStm %d >>\n", xrefStmStart))
	write(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefTableStart))

	r, err := NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	// Try to read Object 5
	// It is NOT in the main xref table, only in XRefStm
	// If XRefStm is ignored, this should fail or return nil/empty

	// We can't access r.xref directly easily as it is not exported (wait, it is not exported).
	// But we can try to resolve it via indirect reference.
	// Object 3 refers to Object 5 via /F1

	p3 := r.Page(1)
	if p3.V.Kind() == Null {
		t.Fatal("Page 1 not found")
	}

	fonts := p3.Resources().Key("Font")
	f1 := fonts.Key("F1")

	if f1.Kind() == Null {
		t.Errorf("Font F1 not found - XRefStm likely ignored")
	} else {
		// Check if it resolved correctly
		baseFont := f1.Key("BaseFont")
		if baseFont.Name() != "Helvetica" {
			t.Errorf("Font F1 resolved but incorrect content: %v", f1)
		}
	}
}
