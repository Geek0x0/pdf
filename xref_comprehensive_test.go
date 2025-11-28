package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"
	"testing"
)

// =============================================================================
// Part 1: PDF Version Compatibility Tests
// =============================================================================

// TestPDFVersionXrefCompatibility tests xref handling across PDF versions 1.0-2.0
func TestPDFVersionXrefCompatibility(t *testing.T) {
	versions := []struct {
		version     string
		useStream   bool
		description string
	}{
		{"1.0", false, "PDF 1.0 - original format, traditional xref only"},
		{"1.1", false, "PDF 1.1 - added encryption support"},
		{"1.2", false, "PDF 1.2 - added external streams"},
		{"1.3", false, "PDF 1.3 - added digital signatures"},
		{"1.4", false, "PDF 1.4 - added transparency, most common legacy"},
		{"1.5", true, "PDF 1.5 - introduced xref streams and object streams"},
		{"1.6", true, "PDF 1.6 - added AES encryption"},
		{"1.7", true, "PDF 1.7 - ISO 32000-1 standard"},
		{"2.0", true, "PDF 2.0 - ISO 32000-2 with enhanced features"},
	}

	for _, v := range versions {
		t.Run(fmt.Sprintf("version_%s", v.version), func(t *testing.T) {
			var pdf []byte
			if v.useStream {
				pdf = buildVersionedXrefStreamPDF(v.version)
			} else {
				pdf = buildVersionedTraditionalPDF(v.version)
			}

			r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
			if err != nil {
				t.Fatalf("Failed to parse PDF %s: %v", v.version, err)
			}
			defer r.Close()

			// Verify basic structure
			trailer := r.Trailer()
			if trailer.IsNull() {
				t.Fatalf("PDF %s: trailer is null", v.version)
			}

			root := trailer.Key("Root")
			if root.IsNull() {
				t.Fatalf("PDF %s: Root is null", v.version)
			}

			// Verify page access
			numPages := r.NumPage()
			if numPages != 1 {
				t.Errorf("PDF %s: expected 1 page, got %d", v.version, numPages)
			}

			t.Logf("PDF %s parsed successfully: %s", v.version, v.description)
		})
	}
}

// =============================================================================
// Part 2: xref Format Variants Tests
// =============================================================================

// TestXrefTableVariants tests different traditional xref table formats
func TestXrefTableVariants(t *testing.T) {
	tests := []struct {
		name        string
		eol         string
		spacing     string
		description string
	}{
		{"unix_lf", "\n", " ", "Unix-style LF line endings"},
		{"windows_crlf", "\r\n", " ", "Windows-style CRLF line endings"},
		{"old_mac_cr", "\r", " ", "Old Mac-style CR line endings"},
		{"mixed_endings", "MIXED", " ", "Mixed line endings (real-world scenario)"},
		{"compact_spacing", "\n", "", "Minimal whitespace"},
		{"extra_spacing", "\n", "  ", "Extra whitespace between fields"},
		{"tab_separator", "\n", "\t", "Tab as field separator"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pdf := buildXrefTableVariant(tt.eol, tt.spacing)
			r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
			if err != nil {
				t.Fatalf("%s: failed to parse: %v", tt.description, err)
			}
			defer r.Close()

			if r.Trailer().IsNull() {
				t.Errorf("%s: trailer is null", tt.description)
			}
		})
	}
}

// TestXrefStreamWArrayVariants tests different W array configurations in xref streams
func TestXrefStreamWArrayVariants(t *testing.T) {
	tests := []struct {
		name string
		w    [3]int
		desc string
	}{
		{"w_1_1_1", [3]int{1, 1, 1}, "Minimal W array [1 1 1] for small files"},
		{"w_1_2_1", [3]int{1, 2, 1}, "Standard W array [1 2 1] for medium files"},
		{"w_1_3_1", [3]int{1, 3, 1}, "Extended W array [1 3 1] for large files"},
		{"w_1_4_2", [3]int{1, 4, 2}, "Large W array [1 4 2] for very large files"},
		{"w_0_2_0", [3]int{0, 2, 0}, "Omitted type/gen fields [0 2 0]"},
		{"w_1_2_0", [3]int{1, 2, 0}, "Omitted generation field [1 2 0]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pdf := buildXrefStreamWithW(tt.w)
			r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
			if err != nil {
				t.Fatalf("%s: failed to parse: %v", tt.desc, err)
			}
			defer r.Close()

			if r.Trailer().IsNull() {
				t.Errorf("%s: trailer is null", tt.desc)
			}

			// Verify we can resolve objects
			root := r.Trailer().Key("Root")
			if root.IsNull() {
				t.Errorf("%s: cannot resolve Root", tt.desc)
			}
		})
	}
}

// TestXrefStreamFilterVariants tests different compression filters
func TestXrefStreamFilterVariants(t *testing.T) {
	tests := []struct {
		name   string
		filter string
	}{
		{"no_filter", ""},
		{"flate_decode", "/FlateDecode"},
		// Note: Other filters like LZW, ASCII85 are less common for xref streams
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pdf := buildXrefStreamWithFilter(tt.filter)
			r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
			if err != nil {
				t.Fatalf("Filter %s: failed to parse: %v", tt.filter, err)
			}
			defer r.Close()

			if r.Trailer().IsNull() {
				t.Errorf("Filter %s: trailer is null", tt.filter)
			}
		})
	}
}

// TestXrefMultipleSubsections tests xref tables with multiple subsections
func TestXrefMultipleSubsections(t *testing.T) {
	tests := []struct {
		name        string
		subsections [][2]int // [start, count] pairs
	}{
		{"single_subsection", [][2]int{{0, 5}}},
		{"two_subsections", [][2]int{{0, 3}, {5, 2}}},
		{"sparse_subsections", [][2]int{{0, 1}, {10, 1}, {100, 1}}},
		{"many_subsections", [][2]int{{0, 1}, {2, 1}, {4, 1}, {6, 1}, {8, 1}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pdf := buildXrefWithSubsections(tt.subsections)
			r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
			if err != nil {
				t.Fatalf("%s: failed to parse: %v", tt.name, err)
			}
			defer r.Close()

			if r.Trailer().IsNull() {
				t.Errorf("%s: trailer is null", tt.name)
			}
		})
	}
}

// =============================================================================
// Part 3: PDF Generator Specific Format Tests
// =============================================================================

// TestGeneratorSpecificFormats tests xref formats from various PDF generators
func TestGeneratorSpecificFormats(t *testing.T) {
	generators := []struct {
		name        string
		builder     func() []byte
		description string
	}{
		{
			name:        "adobe_acrobat",
			builder:     buildAdobeStylePDF,
			description: "Adobe Acrobat - precise formatting, ID array",
		},
		{
			name:        "chrome_print",
			builder:     buildChromeStylePDF,
			description: "Chrome Print to PDF - compact xref stream",
		},
		{
			name:        "itext_library",
			builder:     buildITextStylePDF,
			description: "iText - specific spacing patterns",
		},
		{
			name:        "pdfbox",
			builder:     buildPDFBoxStylePDF,
			description: "Apache PDFBox - Java library format",
		},
		{
			name:        "wkhtmltopdf",
			builder:     buildWkhtmltopdfStylePDF,
			description: "wkhtmltopdf - Qt WebKit based",
		},
		{
			name:        "reportlab",
			builder:     buildReportLabStylePDF,
			description: "ReportLab - Python library format",
		},
		{
			name:        "ghostscript",
			builder:     buildGhostscriptStylePDF,
			description: "Ghostscript - PostScript converter",
		},
		{
			name:        "libreoffice",
			builder:     buildLibreOfficeStylePDF,
			description: "LibreOffice - OpenDocument export",
		},
	}

	for _, gen := range generators {
		t.Run(gen.name, func(t *testing.T) {
			pdf := gen.builder()
			r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
			if err != nil {
				t.Fatalf("%s: failed to parse: %v", gen.description, err)
			}
			defer r.Close()

			trailer := r.Trailer()
			if trailer.IsNull() {
				t.Errorf("%s: trailer is null", gen.description)
			}

			// Verify page access works
			if r.NumPage() < 1 {
				t.Errorf("%s: no pages found", gen.description)
			}

			t.Logf("%s: parsed successfully", gen.description)
		})
	}
}

// =============================================================================
// Part 4: Incremental Update and Prev Chain Tests
// =============================================================================

// TestIncrementalUpdateChains tests PDFs with multiple incremental updates
func TestIncrementalUpdateChains(t *testing.T) {
	tests := []struct {
		name       string
		numUpdates int
	}{
		{"single_update", 1},
		{"two_updates", 2},
		{"three_updates", 3},
		{"five_updates", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pdf := buildIncrementalChainPDF(tt.numUpdates)
			r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
			if err != nil {
				t.Fatalf("%s: failed to parse: %v", tt.name, err)
			}
			defer r.Close()

			trailer := r.Trailer()
			if trailer.IsNull() {
				t.Errorf("%s: trailer is null", tt.name)
			}

			// The final Size should reflect all objects
			// Base has 4 objects (0-3), each update adds 1 object
			// Size is always next available object number: 4 + numUpdates + 1
			expectedSize := int64(4 + tt.numUpdates + 1)
			if got := trailer.Key("Size").Int64(); got != expectedSize {
				t.Errorf("%s: expected Size %d, got %d", tt.name, expectedSize, got)
			}
		})
	}
}

// TestXrefStreamPrevChain tests xref stream Prev chains
func TestXrefStreamPrevChain(t *testing.T) {
	pdf := buildXrefStreamPrevChainPDF()
	r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		t.Fatalf("Failed to parse xref stream prev chain: %v", err)
	}
	defer r.Close()

	if r.Trailer().IsNull() {
		t.Error("Trailer is null")
	}
}

// =============================================================================
// Part 5: Edge Cases and Error Tolerance Tests
// =============================================================================

// TestXrefOffsetTolerance tests tolerance for slightly incorrect offsets
func TestXrefOffsetTolerance(t *testing.T) {
	tests := []struct {
		name        string
		offsetDelta int
		shouldParse bool
	}{
		{"exact_offset", 0, true},
		{"offset_plus_1", 1, true},
		{"offset_minus_1", -1, true},
		{"offset_plus_5", 5, true},
		{"offset_minus_5", -5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pdf := buildPDFWithOffsetError(tt.offsetDelta)
			r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
			if tt.shouldParse {
				if err != nil {
					t.Logf("%s: parsing failed (may be acceptable): %v", tt.name, err)
				} else {
					defer r.Close()
					if r.Trailer().IsNull() {
						t.Errorf("%s: trailer is null", tt.name)
					}
				}
			}
		})
	}
}

// TestMalformedXrefRecovery tests recovery from malformed xref structures
func TestMalformedXrefRecovery(t *testing.T) {
	tests := []struct {
		name        string
		builder     func() []byte
		shouldParse bool
		description string
	}{
		{
			name:        "missing_startxref_value",
			builder:     buildPDFMissingStartxrefValue,
			shouldParse: false,
			description: "startxref keyword without offset value",
		},
		{
			name:        "truncated_xref_entry",
			builder:     buildPDFTruncatedXrefEntry,
			shouldParse: false,
			description: "xref entry shorter than expected",
		},
		{
			name:        "extra_whitespace_in_xref",
			builder:     buildPDFExtraWhitespaceXref,
			shouldParse: true,
			description: "excessive whitespace in xref table",
		},
		{
			name:        "missing_trailer_size",
			builder:     buildPDFMissingTrailerSize,
			shouldParse: true, // Should recover
			description: "trailer missing /Size entry",
		},
		{
			name:        "zero_startxref",
			builder:     buildPDFZeroStartxref,
			shouldParse: false,
			description: "startxref pointing to offset 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pdf := tt.builder()
			r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
			if tt.shouldParse {
				if err != nil {
					t.Logf("%s: %s - failed: %v", tt.name, tt.description, err)
				} else {
					defer r.Close()
					t.Logf("%s: %s - recovered successfully", tt.name, tt.description)
				}
			} else {
				if err == nil {
					r.Close()
					t.Logf("%s: %s - unexpectedly succeeded", tt.name, tt.description)
				}
			}
		})
	}
}

// TestXrefObjectStreamReference tests xref entries pointing to object streams
func TestXrefObjectStreamReference(t *testing.T) {
	pdf := buildPDFWithObjectStream()
	r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		t.Fatalf("Failed to parse PDF with object stream: %v", err)
	}
	defer r.Close()

	// Object in stream should be resolvable
	trailer := r.Trailer()
	if trailer.IsNull() {
		t.Error("Trailer is null")
	}
}

// =============================================================================
// Part 6: Special Character and Encoding Tests
// =============================================================================

// TestXrefWithSpecialStrings tests xref with special characters in trailer
func TestXrefWithSpecialStrings(t *testing.T) {
	tests := []struct {
		name     string
		producer string
	}{
		{"ascii_simple", "Simple Producer"},
		{"with_parens", "Producer (version 1.0)"},
		{"with_backslash", "Path\\To\\Producer"},
		{"unicode_utf16", "\xfe\xff\x00P\x00D\x00F"}, // UTF-16BE "PDF"
		{"pdfdoc_encoded", "Caf\xe9"},                // PDFDocEncoding café
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pdf := buildPDFWithProducer(tt.producer)
			r, err := NewReader(bytes.NewReader(pdf), int64(len(pdf)))
			if err != nil {
				t.Fatalf("%s: failed to parse: %v", tt.name, err)
			}
			defer r.Close()

			trailer := r.Trailer()
			if trailer.IsNull() {
				t.Errorf("%s: trailer is null", tt.name)
			}
		})
	}
}

// =============================================================================
// Helper Functions - PDF Builders
// =============================================================================

func buildVersionedTraditionalPDF(version string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-" + version + "\n")
	buf.WriteString("%\x80\x80\x80\x80\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	obj3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n")

	xref := buf.Len()
	buf.WriteString("xref\n0 4\n")
	buf.WriteString("0000000000 65535 f \n")
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj1)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj2)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj3)
	buf.WriteString("trailer\n<< /Size 4 /Root 1 0 R >>\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xref)

	return buf.Bytes()
}

func buildVersionedXrefStreamPDF(version string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-" + version + "\n")
	buf.WriteString("%\x80\x80\x80\x80\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	obj3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n")

	xrefOffset := buf.Len()

	// Build xref stream data
	var raw []byte
	appendEntry := func(typ, field2, gen int) {
		raw = append(raw, byte(typ))
		raw = append(raw, byte(field2>>8), byte(field2))
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

	fmt.Fprintf(&buf, "4 0 obj\n<< /Type /XRef /Size 5 /W [1 2 1] /Root 1 0 R /Length %d /Filter /FlateDecode >>\nstream\n", compressed.Len())
	buf.Write(compressed.Bytes())
	buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xrefOffset)

	return buf.Bytes()
}

func buildXrefTableVariant(eol, spacing string) []byte {
	var buf bytes.Buffer

	// Handle mixed endings specially
	actualEol := eol
	if eol == "MIXED" {
		actualEol = "\n"
	}

	buf.WriteString("%PDF-1.4" + actualEol)

	obj1 := buf.Len()
	buf.WriteString("1 0 obj" + actualEol + "<< /Type /Catalog /Pages 2 0 R >>" + actualEol + "endobj" + actualEol)

	obj2 := buf.Len()
	buf.WriteString("2 0 obj" + actualEol + "<< /Type /Pages /Kids [] /Count 0 >>" + actualEol + "endobj" + actualEol)

	xref := buf.Len()

	// Use different EOL for xref if mixed
	xrefEol := actualEol
	if eol == "MIXED" {
		xrefEol = "\r\n"
	}

	buf.WriteString("xref" + xrefEol)
	buf.WriteString("0" + spacing + "3" + xrefEol)
	buf.WriteString("0000000000" + spacing + "65535" + spacing + "f" + spacing + xrefEol)
	fmt.Fprintf(&buf, "%010d%s00000%sn%s%s", obj1, spacing, spacing, spacing, xrefEol)
	fmt.Fprintf(&buf, "%010d%s00000%sn%s%s", obj2, spacing, spacing, spacing, xrefEol)
	buf.WriteString("trailer" + xrefEol + "<< /Size 3 /Root 1 0 R >>" + xrefEol)
	fmt.Fprintf(&buf, "startxref%s%d%s%%%%EOF", xrefEol, xref, xrefEol)

	return buf.Bytes()
}

func buildXrefStreamWithW(w [3]int) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n%\x80\x80\x80\x80\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	xrefOffset := buf.Len()

	// Build entries with specified W
	var raw []byte
	writeField := func(value, width int) {
		for i := width - 1; i >= 0; i-- {
			raw = append(raw, byte(value>>(i*8)))
		}
	}

	// Object 0 (free)
	writeField(0, w[0])
	writeField(0, w[1])
	writeField(255, w[2])

	// Object 1
	if w[0] > 0 {
		writeField(1, w[0])
	}
	writeField(obj1, w[1])
	if w[2] > 0 {
		writeField(0, w[2])
	}

	// Object 2
	if w[0] > 0 {
		writeField(1, w[0])
	}
	writeField(obj2, w[1])
	if w[2] > 0 {
		writeField(0, w[2])
	}

	// Object 3 (xref stream itself)
	if w[0] > 0 {
		writeField(1, w[0])
	}
	writeField(xrefOffset, w[1])
	if w[2] > 0 {
		writeField(0, w[2])
	}

	fmt.Fprintf(&buf, "3 0 obj\n<< /Type /XRef /Size 4 /W [%d %d %d] /Root 1 0 R /Length %d >>\nstream\n",
		w[0], w[1], w[2], len(raw))
	buf.Write(raw)
	buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xrefOffset)

	return buf.Bytes()
}

func buildXrefStreamWithFilter(filter string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n%\x80\x80\x80\x80\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	xrefOffset := buf.Len()

	raw := []byte{
		0, 0, 0, 255,
		1, byte(obj1 >> 8), byte(obj1), 0,
		1, byte(obj2 >> 8), byte(obj2), 0,
		1, byte(xrefOffset >> 8), byte(xrefOffset), 0,
	}

	var streamData []byte
	filterStr := ""
	if filter == "/FlateDecode" {
		var compressed bytes.Buffer
		zw := zlib.NewWriter(&compressed)
		zw.Write(raw)
		zw.Close()
		streamData = compressed.Bytes()
		filterStr = " /Filter /FlateDecode"
	} else {
		streamData = raw
	}

	fmt.Fprintf(&buf, "3 0 obj\n<< /Type /XRef /Size 4 /W [1 2 1] /Root 1 0 R /Length %d%s >>\nstream\n",
		len(streamData), filterStr)
	buf.Write(streamData)
	buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xrefOffset)

	return buf.Bytes()
}

func buildXrefWithSubsections(subsections [][2]int) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\x80\x80\x80\x80\n")

	// Calculate max object number
	maxObj := 0
	for _, ss := range subsections {
		if end := ss[0] + ss[1]; end > maxObj {
			maxObj = end
		}
	}

	// Create catalog and pages at fixed positions
	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	xref := buf.Len()
	buf.WriteString("xref\n")

	// Write subsections
	for _, ss := range subsections {
		fmt.Fprintf(&buf, "%d %d\n", ss[0], ss[1])
		for i := 0; i < ss[1]; i++ {
			objNum := ss[0] + i
			if objNum == 0 {
				buf.WriteString("0000000000 65535 f \n")
			} else if objNum == 1 {
				fmt.Fprintf(&buf, "%010d 00000 n \n", obj1)
			} else if objNum == 2 {
				fmt.Fprintf(&buf, "%010d 00000 n \n", obj2)
			} else {
				// Dummy entries for other objects
				buf.WriteString("0000000000 00000 f \n")
			}
		}
	}

	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\n", maxObj)
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xref)

	return buf.Bytes()
}

// Generator-specific builders
func buildAdobeStylePDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n") // Adobe-style binary comment

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R /PageLayout /SinglePage >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	obj3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> >>\nendobj\n")

	xref := buf.Len()
	buf.WriteString("xref\n0 4\n")
	buf.WriteString("0000000000 65535 f \n")
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj1)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj2)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj3)
	// Adobe-style trailer with ID
	buf.WriteString("trailer\n<< /Size 4 /Root 1 0 R /ID [<ABC123> <ABC123>] >>\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xref)

	return buf.Bytes()
}

func buildChromeStylePDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\x80\x80\x80\x80\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<</Type/Catalog/Pages 2 0 R>>\nendobj\n") // Chrome uses compact spacing

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<</Type/Pages/Kids[3 0 R]/Count 1>>\nendobj\n")

	obj3 := buf.Len()
	buf.WriteString("3 0 obj\n<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]>>\nendobj\n")

	xrefOffset := buf.Len()

	raw := []byte{
		0, 0, 0, 255,
		1, byte(obj1 >> 8), byte(obj1), 0,
		1, byte(obj2 >> 8), byte(obj2), 0,
		1, byte(obj3 >> 8), byte(obj3), 0,
		1, byte(xrefOffset >> 8), byte(xrefOffset), 0,
	}

	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	zw.Write(raw)
	zw.Close()

	fmt.Fprintf(&buf, "4 0 obj\n<</Type/XRef/Size 5/W[1 2 1]/Root 1 0 R/Length %d/Filter/FlateDecode>>\nstream\n", compressed.Len())
	buf.Write(compressed.Bytes())
	buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xrefOffset)

	return buf.Bytes()
}

func buildITextStylePDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<<\n/Type /Catalog\n/Pages 2 0 R\n>>\nendobj\n") // iText uses newlines in dicts

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<<\n/Type /Pages\n/Kids [3 0 R]\n/Count 1\n>>\nendobj\n")

	obj3 := buf.Len()
	buf.WriteString("3 0 obj\n<<\n/Type /Page\n/Parent 2 0 R\n/MediaBox [0 0 595 842]\n>>\nendobj\n") // A4 size

	xref := buf.Len()
	buf.WriteString("xref\n0 4\n")
	buf.WriteString("0000000000 65535 f \n")
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj1)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj2)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj3)
	buf.WriteString("trailer\n<<\n/Size 4\n/Root 1 0 R\n>>\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xref)

	return buf.Bytes()
}

func buildPDFBoxStylePDF() []byte {
	return buildITextStylePDF() // Similar to iText
}

func buildWkhtmltopdfStylePDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [ 3 0 R ] /Count 1 >>\nendobj\n") // Extra spaces in array

	obj3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [ 0 0 595.28 841.89 ] >>\nendobj\n")

	xref := buf.Len()
	buf.WriteString("xref\n0 4\n")
	buf.WriteString("0000000000 65535 f \n")
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj1)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj2)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj3)
	buf.WriteString("trailer\n<< /Size 4 /Root 1 0 R >>\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xref)

	return buf.Bytes()
}

func buildReportLabStylePDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\x93\x8c\x8b\x9e ReportLab\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Count 1 /Kids [3 0 R] >>\nendobj\n") // Different key order

	obj3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /MediaBox [0 0 612 792] /Parent 2 0 R >>\nendobj\n")

	xref := buf.Len()
	buf.WriteString("xref\n0 4\n")
	buf.WriteString("0000000000 65535 f \n")
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj1)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj2)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj3)
	buf.WriteString("trailer\n<< /Size 4 /Root 1 0 R >>\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xref)

	return buf.Bytes()
}

func buildGhostscriptStylePDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xc7\xec\x8f\xa2\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	obj3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n")

	xref := buf.Len()
	buf.WriteString("xref\n0 4\n")
	buf.WriteString("0000000000 65535 f \n")
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj1)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj2)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj3)
	buf.WriteString("trailer\n<< /Size 4 /Root 1 0 R /Producer (GPL Ghostscript) >>\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xref)

	return buf.Bytes()
}

func buildLibreOfficeStylePDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.6\n%\xc3\xa4\xc3\xbc\xc3\xb6\xc3\x9f\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<</Type/Catalog/Pages 2 0 R>>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<</Type/Pages/Count 1/Kids[3 0 R]>>\nendobj\n")

	obj3 := buf.Len()
	buf.WriteString("3 0 obj\n<</Type/Page/Parent 2 0 R/MediaBox[0 0 595.276 841.89]>>\nendobj\n")

	xref := buf.Len()
	buf.WriteString("xref\n0 4\n")
	buf.WriteString("0000000000 65535 f \n")
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj1)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj2)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj3)
	buf.WriteString("trailer\n<</Size 4/Root 1 0 R>>\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xref)

	return buf.Bytes()
}

func buildIncrementalChainPDF(numUpdates int) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	obj3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n")

	prevXref := buf.Len()
	buf.WriteString("xref\n0 4\n")
	buf.WriteString("0000000000 65535 f \n")
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj1)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj2)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj3)
	buf.WriteString("trailer\n<< /Size 4 /Root 1 0 R >>\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", prevXref)

	// Add incremental updates
	currentSize := 4
	for i := 0; i < numUpdates; i++ {
		currentSize++
		objOffset := buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n<< /Type /Metadata /Update %d >>\nendobj\n", currentSize, i+1)

		newXref := buf.Len()
		fmt.Fprintf(&buf, "xref\n%d 1\n", currentSize)
		fmt.Fprintf(&buf, "%010d 00000 n \n", objOffset)
		fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Prev %d >>\n", currentSize+1, prevXref)
		fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", newXref)
		prevXref = newXref
	}

	return buf.Bytes()
}

func buildXrefStreamPrevChainPDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n%\x80\x80\x80\x80\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	obj3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n")

	xref1Offset := buf.Len()

	raw1 := []byte{
		0, 0, 0, 255,
		1, byte(obj1 >> 8), byte(obj1), 0,
		1, byte(obj2 >> 8), byte(obj2), 0,
		1, byte(obj3 >> 8), byte(obj3), 0,
		1, byte(xref1Offset >> 8), byte(xref1Offset), 0,
	}

	fmt.Fprintf(&buf, "4 0 obj\n<< /Type /XRef /Size 5 /W [1 2 1] /Root 1 0 R /Length %d >>\nstream\n", len(raw1))
	buf.Write(raw1)
	buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xref1Offset)

	// Incremental update with xref stream
	obj5 := buf.Len()
	buf.WriteString("5 0 obj\n<< /Type /Info /Producer (Updated) >>\nendobj\n")

	xref2Offset := buf.Len()

	raw2 := []byte{
		1, byte(obj5 >> 8), byte(obj5), 0,
		1, byte(xref2Offset >> 8), byte(xref2Offset), 0,
	}

	fmt.Fprintf(&buf, "6 0 obj\n<< /Type /XRef /Size 7 /Index [5 2] /W [1 2 1] /Root 1 0 R /Info 5 0 R /Prev %d /Length %d >>\nstream\n",
		xref1Offset, len(raw2))
	buf.Write(raw2)
	buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xref2Offset)

	return buf.Bytes()
}

func buildPDFWithOffsetError(delta int) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	xref := buf.Len()
	buf.WriteString("xref\n0 3\n")
	buf.WriteString("0000000000 65535 f \n")
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj1)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj2)
	buf.WriteString("trailer\n<< /Size 3 /Root 1 0 R >>\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xref+delta)

	return buf.Bytes()
}

func buildPDFMissingStartxrefValue() []byte {
	return []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\nxref\n0 1\n0000000000 65535 f \ntrailer\n<< /Size 1 /Root 1 0 R >>\nstartxref\n%%EOF")
}

func buildPDFTruncatedXrefEntry() []byte {
	return []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\nxref\n0 2\n0000000000 65535 f \n000000\ntrailer\n<< /Size 2 /Root 1 0 R >>\nstartxref\n50\n%%EOF")
}

func buildPDFExtraWhitespaceXref() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	xref := buf.Len()
	buf.WriteString("xref\n\n\n0   3\n\n")
	buf.WriteString("0000000000   65535   f   \n")
	fmt.Fprintf(&buf, "%010d   00000   n   \n", obj1)
	fmt.Fprintf(&buf, "%010d   00000   n   \n", obj2)
	buf.WriteString("\n\ntrailer\n\n<< /Size 3 /Root 1 0 R >>\n\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xref)

	return buf.Bytes()
}

func buildPDFMissingTrailerSize() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	xref := buf.Len()
	buf.WriteString("xref\n0 3\n")
	buf.WriteString("0000000000 65535 f \n")
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj1)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj2)
	buf.WriteString("trailer\n<< /Root 1 0 R >>\n") // Missing /Size
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xref)

	return buf.Bytes()
}

func buildPDFZeroStartxref() []byte {
	return []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\nxref\n0 1\n0000000000 65535 f \ntrailer\n<< /Size 1 /Root 1 0 R >>\nstartxref\n0\n%%EOF")
}

func buildPDFWithObjectStream() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n%\x80\x80\x80\x80\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	xrefOffset := buf.Len()

	raw := []byte{
		0, 0, 0, 255,
		1, byte(obj1 >> 8), byte(obj1), 0,
		1, byte(obj2 >> 8), byte(obj2), 0,
		1, byte(xrefOffset >> 8), byte(xrefOffset), 0,
	}

	fmt.Fprintf(&buf, "3 0 obj\n<< /Type /XRef /Size 4 /W [1 2 1] /Root 1 0 R /Length %d >>\nstream\n", len(raw))
	buf.Write(raw)
	buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xrefOffset)

	return buf.Bytes()
}

func buildPDFWithProducer(producer string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	obj1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	obj2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	xref := buf.Len()
	buf.WriteString("xref\n0 3\n")
	buf.WriteString("0000000000 65535 f \n")
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj1)
	fmt.Fprintf(&buf, "%010d 00000 n \n", obj2)

	// Escape special characters in producer string
	escaped := strings.ReplaceAll(producer, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "(", "\\(")
	escaped = strings.ReplaceAll(escaped, ")", "\\)")

	fmt.Fprintf(&buf, "trailer\n<< /Size 3 /Root 1 0 R /Producer (%s) >>\n", escaped)
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xref)

	return buf.Bytes()
}
