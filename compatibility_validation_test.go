// compatibility_validation_test.go - Comprehensive compatibility validation tests
package pdf

import (
	"strings"
	"testing"
)

func TestCompatibilityValidationSuite(t *testing.T) {
	tests := []struct {
		name     string
		pdfData  string
		expected PDFCompatibilityInfo
		hasError bool
	}{
		{
			name: "PDF 1.4 Basic",
			pdfData: `%PDF-1.4
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
>>
endobj
4 0 obj
<<
/Length 0
>>
stream
endstream
endobj
xref
0 5
0000000000 65535 f
0000000009 00000 n
0000000058 00000 n
0000000115 00000 n
0000000200 00000 n
trailer
<<
/Size 5
/Root 1 0 R
>>
startxref
250
%%EOF`,
			expected: PDFCompatibilityInfo{
				Version:         PDFVersion{1, 4},
				IsLinearized:    false,
				SubFormat:       "",
				HasTransparency: false,
				HasLayers:       false,
				HasForms:        false,
				HasJavaScript:   false,
			},
		},
		{
			name: "PDF 2.0 with Transparency",
			pdfData: `%PDF-2.0
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
/ExtGState <<
/GS1 5 0 R
>>
>>
>>
endobj
4 0 obj
<<
/Length 0
>>
stream
endstream
endobj
5 0 obj
<<
/Type /ExtGState
/BM /Multiply
/ca 0.5
>>
endobj
xref
0 6
0000000000 65535 f
0000000009 00000 n
0000000058 00000 n
0000000115 00000 n
0000000200 00000 n
0000000250 00000 n
trailer
<<
/Size 6
/Root 1 0 R
>>
startxref
300
%%EOF`,
			expected: PDFCompatibilityInfo{
				Version:         PDFVersion{2, 0},
				IsLinearized:    false,
				SubFormat:       "",
				HasTransparency: true,
				HasLayers:       false,
				HasForms:        false,
				HasJavaScript:   false,
			},
		},
		{
			name: "Linearized PDF",
			pdfData: `%PDF-1.5
1 0 obj
<<
/Linearized 1
/L 500
/O 5
/E 300
/N 1
/T 400
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
0000000150 00000 n
trailer
<<
/Size 3
/Root 2 0 R
>>
startxref
200
%%EOF`,
			expected: PDFCompatibilityInfo{
				Version:         PDFVersion{1, 5},
				IsLinearized:    true,
				SubFormat:       "",
				HasTransparency: false,
				HasLayers:       false,
				HasForms:        false,
				HasJavaScript:   false,
			},
		},
		{
			name: "PDF/A Document",
			pdfData: `%PDF-1.4
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
/Length 150
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
%%EOF`,
			expected: PDFCompatibilityInfo{
				Version:         PDFVersion{1, 4},
				IsLinearized:    false,
				SubFormat:       "PDF/A",
				HasTransparency: false,
				HasLayers:       false,
				HasForms:        false,
				HasJavaScript:   false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := CheckPDFCompatibility([]byte(tt.pdfData))
			if (err != nil) != tt.hasError {
				t.Errorf("CheckPDFCompatibility() error = %v, hasError %v", err, tt.hasError)
				return
			}
			if err != nil {
				return
			}

			if info.Version != tt.expected.Version {
				t.Errorf("Version = %v, expected %v", info.Version, tt.expected.Version)
			}
			if info.IsLinearized != tt.expected.IsLinearized {
				t.Errorf("IsLinearized = %v, expected %v", info.IsLinearized, tt.expected.IsLinearized)
			}
			if info.SubFormat != tt.expected.SubFormat {
				t.Errorf("SubFormat = %v, expected %v", info.SubFormat, tt.expected.SubFormat)
			}
			if info.HasTransparency != tt.expected.HasTransparency {
				t.Errorf("HasTransparency = %v, expected %v", info.HasTransparency, tt.expected.HasTransparency)
			}
			if info.HasLayers != tt.expected.HasLayers {
				t.Errorf("HasLayers = %v, expected %v", info.HasLayers, tt.expected.HasLayers)
			}
			if info.HasForms != tt.expected.HasForms {
				t.Errorf("HasForms = %v, expected %v", info.HasForms, tt.expected.HasForms)
			}
			if info.HasJavaScript != tt.expected.HasJavaScript {
				t.Errorf("HasJavaScript = %v, expected %v", info.HasJavaScript, tt.expected.HasJavaScript)
			}
		})
	}
}

func TestPDFAValidation(t *testing.T) {
	// Test PDF/A validation
	pdfAData := []byte(`%PDF-1.4
1 0 obj
<<
/Type /Catalog
/Metadata 2 0 R
>>
endobj
2 0 obj
<<
/Type /Metadata
/Subtype /XML
/Length 100
>>
stream
<pdfaid:part>1</pdfaid:part>
<pdfaid:conformance>A</pdfaid:conformance>
endstream
endobj
xref
0 3
0000000000 65535 f
0000000009 00000 n
0000000050 00000 n
trailer
<<
/Size 3
/Root 1 0 R
>>
startxref
150
%%EOF`)

	warnings, err := ValidatePDFA(pdfAData)
	if err != nil {
		t.Fatalf("ValidatePDFA failed: %v", err)
	}

	// Should have warnings about missing fonts
	foundFontWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "fonts") {
			foundFontWarning = true
			break
		}
	}
	if !foundFontWarning {
		t.Errorf("Expected font warning in PDF/A validation")
	}
}

func TestPDFXValidation(t *testing.T) {
	// Test PDF/X validation
	pdfXData := []byte(`%PDF-1.4
1 0 obj
<<
/Type /Catalog
/OutputIntents [2 0 R]
>>
endobj
2 0 obj
<<
/Type /OutputIntent
/S /GTS_PDFX
/OutputConditionIdentifier (CGATS TR 001)
/RegistryName (http://www.color.org)
/Info (CGATS TR 001)
>>
endobj
xref
0 3
0000000000 65535 f
0000000009 00000 n
0000000080 00000 n
trailer
<<
/Size 3
/Root 1 0 R
>>
startxref
180
%%EOF`)

	warnings, err := ValidatePDFX(pdfXData)
	if err != nil {
		t.Fatalf("ValidatePDFX failed: %v", err)
	}

	// Should pass basic validation
	if len(warnings) > 0 {
		t.Logf("PDF/X validation warnings: %v", warnings)
	}
}

func TestUnsupportedVersion(t *testing.T) {
	// Test unsupported PDF version
	unsupportedPDF := `%PDF-3.0
1 0 obj
<<
/Type /Catalog
>>
endobj
xref
0 2
0000000000 65535 f
0000000009 00000 n
trailer
<<
/Size 2
/Root 1 0 R
>>
startxref
80
%%EOF`

	_, err := CheckPDFCompatibility([]byte(unsupportedPDF))
	if err != nil {
		t.Logf("Expected error for unsupported version: %v", err)
	} else {
		t.Errorf("Expected error for unsupported PDF version 3.0")
	}
}

func TestMalformedPDF(t *testing.T) {
	// Test malformed PDF detection
	malformedData := []byte("not a pdf file")

	_, err := CheckPDFCompatibility(malformedData)
	if err == nil {
		t.Errorf("Expected error for malformed PDF data")
	}
}
