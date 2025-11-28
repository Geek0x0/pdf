package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"testing"
)

func TestXrefCompatibilityMatrix(t *testing.T) {
	tests := []struct {
		name           string
		pdf            []byte
		expectSize     int64
		expectProducer string
		expectInfoProd string
		checkFont      bool
	}{
		{
			name:           "classic_table_pdf14_crlf",
			pdf:            buildClassicXrefPDF("1.4", "\r\n", "Adobe Distiller 7.0"),
			expectSize:     4,
			expectProducer: "Adobe Distiller 7.0",
		},
		{
			name:           "xref_stream_pdf20_chrome_style",
			pdf:            buildXrefStreamPDF("2.0", "Chrome 118"),
			expectSize:     5,
			expectProducer: "Chrome 118",
		},
		{
			name:      "hybrid_xrefstm_adobe_style",
			pdf:       buildHybridXrefPDF(),
			expectSize: 5,
			checkFont: true,
		},
		{
			name:           "incremental_prev_chain_with_new_info",
			pdf:            buildIncrementalUpdatePDF(),
			expectSize:     5,
			expectInfoProd: "Incremental Writer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewReader(bytes.NewReader(tt.pdf), int64(len(tt.pdf)))
			if err != nil {
				t.Fatalf("NewReader failed: %v", err)
			}
			defer r.Close()

			trailer := r.Trailer()
			if trailer.IsNull() {
				t.Fatal("trailer is null")
			}

			if tt.expectSize > 0 {
				if got := trailer.Key("Size").Int64(); got != tt.expectSize {
					t.Fatalf("unexpected /Size: got %d want %d", got, tt.expectSize)
				}
			}

			if tt.expectProducer != "" {
				if got := trailer.Key("Producer").Text(); got != tt.expectProducer {
					t.Fatalf("unexpected trailer Producer: got %q want %q", got, tt.expectProducer)
				}
			}

			if tt.expectInfoProd != "" {
				info := trailer.Key("Info").Key("Producer").Text()
				if info != tt.expectInfoProd {
					t.Fatalf("unexpected Info.Producer: got %q want %q", info, tt.expectInfoProd)
				}
			}

			if tt.checkFont {
				p := r.Page(1)
				if p.V.IsNull() {
					t.Fatal("page 1 not found")
				}
				font := p.Resources().Key("Font").Key("F1")
				if font.IsNull() {
					t.Fatal("font F1 not resolved from hybrid xref")
				}
				if got := font.Key("BaseFont").Name(); got != "Helvetica" {
					t.Fatalf("unexpected BaseFont: got %q", got)
				}
			}
		})
	}
}

func TestFindXRefStreamPositionsFromGenerators(t *testing.T) {
	samples := map[string]string{
		"acrobat_spaced":   "<<\n/Type /XRef\n/Length 20 >>",
		"pdfbox_compact":   "<< /Type/XRef/Length 0 >>",
		"wkhtmltopdf_tabs": "<<\n/Type\t/XRef\n/Filter /FlateDecode\n>>",
	}

	for name, body := range samples {
		t.Run(name, func(t *testing.T) {
			if positions := findXRefStreamPositions([]byte(body)); len(positions) == 0 {
				t.Fatalf("findXRefStreamPositions did not find marker in %s sample", name)
			}
		})
	}
}

func buildClassicXrefPDF(version, eol, producer string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-" + version + eol)

	obj1 := buf.Len()
	buf.WriteString(fmt.Sprintf("1 0 obj%s<< /Type /Catalog /Pages 2 0 R >>%sendobj%s", eol, eol, eol))

	obj2 := buf.Len()
	buf.WriteString(fmt.Sprintf("2 0 obj%s<< /Type /Pages /Kids [3 0 R] /Count 1 >>%sendobj%s", eol, eol, eol))

	obj3 := buf.Len()
	buf.WriteString(fmt.Sprintf("3 0 obj%s<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] >>%sendobj%s", eol, eol, eol))

	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref%s0 4%s", eol, eol)
	fmt.Fprintf(&buf, "0000000000 65535 f %s", eol)
	fmt.Fprintf(&buf, "%010d 00000 n %s", obj1, eol)
	fmt.Fprintf(&buf, "%010d 00000 n %s", obj2, eol)
	fmt.Fprintf(&buf, "%010d 00000 n %s", obj3, eol)
	fmt.Fprintf(&buf, "trailer%s<< /Size 4 /Root 1 0 R /Producer (%s) >>%s", eol, producer, eol)
	fmt.Fprintf(&buf, "startxref%s%d%s%%%%EOF", eol, xrefOffset, eol)

	return buf.Bytes()
}

func buildXrefStreamPDF(version, producer string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-" + version + "\n")
	buf.WriteString("%\x80\x80\x80\x80\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	obj3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] >>\nendobj\n")

	xrefOffset := buf.Len()

	var raw []byte
	appendEntry := func(typ int, field2 int, gen int) {
		raw = append(raw, byte(typ))
		raw = append(raw, byte(field2>>16), byte(field2>>8), byte(field2))
		raw = append(raw, byte(gen))
	}

	appendEntry(0, 0, 255)
	appendEntry(1, obj1, 0)
	appendEntry(1, obj2, 0)
	appendEntry(1, obj3, 0)
	appendEntry(1, xrefOffset, 0)

	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	zw.Write(raw)
	zw.Close()

	fmt.Fprintf(&buf, "4 0 obj\n<< /Type /XRef /Size 5 /W [1 3 1] /Index [0 5] /Root 1 0 R /Producer (%s) /Length %d /Filter /FlateDecode >>\n", producer, compressed.Len())
	buf.WriteString("stream\n")
	buf.Write(compressed.Bytes())
	buf.WriteString("\nendstream\nendobj\n")

	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xrefOffset)

	return buf.Bytes()
}

func buildHybridXrefPDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.6\n")

	catalog := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	pages := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	page := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] /Resources << /Font << /F1 4 0 R >> >> >>\nendobj\n")

	font := buf.Len()
	buf.WriteString("4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	xrefStreamOffset := buf.Len()

	var raw []byte
	raw = append(raw, 1)
	raw = append(raw, byte(font>>16), byte(font>>8), byte(font))
	raw = append(raw, 0)

	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	zw.Write(raw)
	zw.Close()

	fmt.Fprintf(&buf, "5 0 obj\n<< /Type /XRef /Size 5 /Index [4 1] /W [1 3 1] /Length %d /Filter /FlateDecode >>\n", compressed.Len())
	buf.WriteString("stream\n")
	buf.Write(compressed.Bytes())
	buf.WriteString("\nendstream\nendobj\n")

	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 4\n0000000000 65535 f \n%010d 00000 n \n%010d 00000 n \n%010d 00000 n \n", catalog, pages, page)
	fmt.Fprintf(&buf, "trailer\n<< /Size 5 /Root 1 0 R /XRefStm %d >>\n", xrefStreamOffset)
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xrefOffset)

	return buf.Bytes()
}

func buildIncrementalUpdatePDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	obj3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] >>\nendobj\n")

	baseXref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 4\n0000000000 65535 f \n%010d 00000 n \n%010d 00000 n \n%010d 00000 n \n", obj1, obj2, obj3)
	fmt.Fprintf(&buf, "trailer\n<< /Size 4 /Root 1 0 R >>\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", baseXref)

	info := buf.Len()
	buf.WriteString("4 0 obj\n<< /Producer (Incremental Writer) >>\nendobj\n")

	incXref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 5\n0000000000 65535 f \n%010d 00000 n \n%010d 00000 n \n%010d 00000 n \n%010d 00000 n \n", obj1, obj2, obj3, info)
	fmt.Fprintf(&buf, "trailer\n<< /Size 5 /Root 1 0 R /Info 4 0 R /Prev %d >>\n", baseXref)
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", incXref)

	return buf.Bytes()
}
