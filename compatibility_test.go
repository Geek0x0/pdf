// compatibility_test.go - Tests for PDF format compatibility
package pdf

import (
	"testing"
)

func TestPDFVersionSupport(t *testing.T) {
	tests := []struct {
		version PDFVersion
		want    bool
	}{
		{PDFVersion{1, 0}, true},
		{PDFVersion{1, 7}, true},
		{PDFVersion{2, 0}, true},
		{PDFVersion{1, 8}, false}, // Not supported yet
		{PDFVersion{2, 1}, false}, // Not supported yet
		{PDFVersion{3, 0}, false}, // Not supported
	}

	for _, tt := range tests {
		if got := tt.version.IsSupported(); got != tt.want {
			t.Errorf("PDFVersion{%d, %d}.IsSupported() = %v, want %v", tt.version.Major, tt.version.Minor, got, tt.want)
		}
	}
}

func TestParsePDFVersion(t *testing.T) {
	tests := []struct {
		data string
		want PDFVersion
		err  bool
	}{
		{"%PDF-1.4\n", PDFVersion{1, 4}, false},
		{"%PDF-2.0\n", PDFVersion{2, 0}, false},
		{"%PDF-1.7\n", PDFVersion{1, 7}, false},
		{"invalid", PDFVersion{}, true},
		{"%PDF-3.0\n", PDFVersion{3, 0}, false}, // Parsed but not supported
	}

	for _, tt := range tests {
		got, err := parsePDFVersion([]byte(tt.data))
		if (err != nil) != tt.err {
			t.Errorf("parsePDFVersion(%q) error = %v, wantErr %v", tt.data, err, tt.err)
			continue
		}
		if !tt.err && (got != tt.want) {
			t.Errorf("parsePDFVersion(%q) = %v, want %v", tt.data, got, tt.want)
		}
	}
}

func TestCheckPDFCompatibility(t *testing.T) {
	// Test with a minimal PDF 1.4
	pdf14 := `%PDF-1.4
1 0 obj
<<
/Type /Catalog
/Pages 2 0 R
>>
endobj
2 0 obj
<<
/Type /Pages
/Kids [3 0 R]
/Count 1
>>
endobj
3 0 obj
<<
/Type /Page
/Parent 2 0 R
/MediaBox [0 0 612 792]
/Contents 4 0 R
/Resources <<
/Font <<
/F1 5 0 R
>>
>>
>>
endobj
4 0 obj
<<
/Length 44
>>
stream
BT
/F1 12 Tf
100 700 Td
(Hello World) Tj
ET
endstream
endobj
5 0 obj
<<
/Type /Font
/Subtype /Type1
/BaseFont /Helvetica
>>
endobj
xref
0 6
0000000000 65535 f
0000000009 00000 n
0000000058 00000 n
0000000115 00000 n
0000000274 00000 n
0000000354 00000 n
trailer
<<
/Size 6
/Root 1 0 R
>>
startxref
459
%%EOF`

	info, err := CheckPDFCompatibility([]byte(pdf14))
	if err != nil {
		t.Fatalf("CheckPDFCompatibility failed: %v", err)
	}

	if info.Version.Major != 1 || info.Version.Minor != 4 {
		t.Errorf("Expected version 1.4, got %s", info.Version.String())
	}

	if info.SubFormat != "" {
		t.Errorf("Expected no subformat, got %s", info.SubFormat)
	}
}

func TestPDF20Features(t *testing.T) {
	// Test PDF 2.0 with some advanced features
	pdf20 := `%PDF-2.0
1 0 obj
<<
/Type /Catalog
/Pages 2 0 R
>>
endobj
2 0 obj
<<
/Type /Pages
/Kids [3 0 R]
/Count 1
>>
endobj
3 0 obj
<<
/Type /Page
/Parent 2 0 R
/MediaBox [0 0 612 792]
/Contents 4 0 R
/Resources <<
/Font <<
/F1 5 0 R
>>
/ExtGState <<
/GS1 6 0 R
>>
>>
>>
endobj
4 0 obj
<<
/Length 44
>>
stream
BT
/F1 12 Tf
100 700 Td
(Hello PDF 2.0) Tj
ET
endstream
endobj
5 0 obj
<<
/Type /Font
/Subtype /Type1
/BaseFont /Helvetica
>>
endobj
6 0 obj
<<
/Type /ExtGState
/BM /Multiply
/ca 0.5
>>
endobj
xref
0 7
0000000000 65535 f
0000000009 00000 n
0000000058 00000 n
0000000115 00000 n
0000000274 00000 n
0000000354 00000 n
0000000434 00000 n
trailer
<<
/Size 7
/Root 1 0 R
>>
startxref
520
%%EOF`

	info, err := CheckPDFCompatibility([]byte(pdf20))
	if err != nil {
		t.Fatalf("CheckPDFCompatibility failed: %v", err)
	}

	if info.Version.Major != 2 || info.Version.Minor != 0 {
		t.Errorf("Expected version 2.0, got %s", info.Version.String())
	}

	if !info.HasTransparency {
		t.Errorf("Expected transparency features to be detected")
	}
}

func TestLinearizedPDFDetection(t *testing.T) {
	// Test linearized PDF detection
	linearizedPDF := `%PDF-1.4
1 0 obj
<<
/Linearized 1
/L 1234
/O 10
/E 1000
/N 1
/T 1000
>>
endobj
2 0 obj
<<
/Type /Catalog
/Pages 3 0 R
>>
endobj
xref
0 3
0000000000 65535 f
0000000009 00000 n
0000000123 00000 n
trailer
<<
/Size 3
/Root 2 0 R
>>
startxref
200
%%EOF`

	info, err := CheckPDFCompatibility([]byte(linearizedPDF))
	if err != nil {
		t.Fatalf("CheckPDFCompatibility failed: %v", err)
	}

	if !info.IsLinearized {
		t.Errorf("Expected PDF to be detected as linearized")
	}
}

func TestSubFormatDetection(t *testing.T) {
	// Test PDF/A detection
	pdfA := `%PDF-1.4
1 0 obj
<<
/Type /Catalog
/Pages 2 0 R
/Metadata 3 0 R
>>
endobj
3 0 obj
<<
/Type /Metadata
/Subtype /XML
/Length 100
>>
stream
<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
<pdfaid:part>1</pdfaid:part>
<pdfaid:conformance>A</pdfaid:conformance>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
endstream
endobj
xref
0 4
0000000000 65535 f
0000000009 00000 n
0000000058 00000 n
0000000115 00000 n
trailer
<<
/Size 4
/Root 1 0 R
>>
startxref
300
%%EOF`

	info, err := CheckPDFCompatibility([]byte(pdfA))
	if err != nil {
		t.Fatalf("CheckPDFCompatibility failed: %v", err)
	}

	if info.SubFormat != "PDF/A" {
		t.Errorf("Expected PDF/A format, got %s", info.SubFormat)
	}
}
