package pdf

import (
	"testing"
)

// TestPDFVersionBoundaryIssues tests edge cases in PDF version parsing
func TestPDFVersionBoundaryIssues(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		shouldErr bool
		expected  PDFVersion
	}{
		{
			name:      "normal version with trailing content",
			data:      []byte("%PDF-1.7\n%%EOF"),
			shouldErr: false,
			expected:  PDFVersion{1, 7},
		},
		{
			name:      "version at exact end of buffer",
			data:      []byte("%PDF-1.5"),
			shouldErr: false,
			expected:  PDFVersion{1, 5},
		},
		{
			name:      "version with no trailing data",
			data:      []byte("%PDF-2.0"),
			shouldErr: false,
			expected:  PDFVersion{2, 0},
		},
		{
			name:      "truncated version (missing minor)",
			data:      []byte("%PDF-1."),
			shouldErr: true,
		},
		{
			name:      "truncated version (missing dot and minor)",
			data:      []byte("%PDF-1"),
			shouldErr: true,
		},
		{
			name:      "too short",
			data:      []byte("%PDF-"),
			shouldErr: true,
		},
		{
			name:      "version 1.4 at end",
			data:      []byte("%PDF-1.4"),
			shouldErr: false,
			expected:  PDFVersion{1, 4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := parsePDFVersion(tt.data)

			if tt.shouldErr {
				if err == nil {
					t.Errorf("Expected error but got none, version: %v", version)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				} else if version != tt.expected {
					t.Errorf("Expected version %v, got %v", tt.expected, version)
				}
			}
		})
	}
}

// TestPDFVersionInHeader tests version parsing in full header context
func TestPDFVersionInHeader(t *testing.T) {
	tests := []struct {
		name      string
		header    []byte
		shouldErr bool
		expected  PDFVersion
	}{
		{
			name:      "minimal valid header",
			header:    []byte("%PDF-1.7\n"),
			shouldErr: false,
			expected:  PDFVersion{1, 7},
		},
		{
			name:      "header with binary marker",
			header:    []byte("%PDF-1.5\n%âãÏÓ\n"),
			shouldErr: false,
			expected:  PDFVersion{1, 5},
		},
		{
			name:      "version exactly at position 8",
			header:    []byte("%PDF-1.4"),
			shouldErr: false,
			expected:  PDFVersion{1, 4},
		},
		{
			name:      "BOM before PDF marker",
			header:    []byte("\xef\xbb\xbf%PDF-2.0\n"),
			shouldErr: false,
			expected:  PDFVersion{2, 0},
		},
		{
			name:      "whitespace before PDF marker",
			header:    []byte("  %PDF-1.6\n"),
			shouldErr: false,
			expected:  PDFVersion{1, 6},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := parsePDFVersion(tt.header)

			if tt.shouldErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v\nData: %q", err, tt.header)
				} else if version != tt.expected {
					t.Errorf("Expected version %v, got %v", tt.expected, version)
				}
			}
		})
	}
}

// TestPDFHeaderBoundaryCheck tests the header validation in NewReaderEncrypted
func TestPDFHeaderBoundaryCheck(t *testing.T) {
	// This test ensures that the boundary check in read.go doesn't fail
	// when the PDF version is at the exact end of the initial buffer read

	// Create a minimal PDF structure with version at specific position
	// The key is to make the version end exactly at a critical boundary

	tests := []struct {
		name        string
		description string
		buildPDF    func() []byte
	}{
		{
			name:        "version_near_end",
			description: "PDF version near the end of header buffer",
			buildPDF: func() []byte {
				// Create a PDF where %PDF-1.7 appears and ends at position 8
				return []byte("%PDF-1.7")
			},
		},
		{
			name:        "version_with_minimal_trailing",
			description: "PDF version with just a newline after",
			buildPDF: func() []byte {
				return []byte("%PDF-1.5\n")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.buildPDF()
			t.Logf("Test: %s", tt.description)
			t.Logf("Data length: %d, content: %q", len(data), data)

			version, err := parsePDFVersion(data)
			if err != nil {
				t.Errorf("parsePDFVersion failed: %v", err)
			} else {
				t.Logf("Successfully parsed version: %s", version.String())
			}
		})
	}
}
