// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"bytes"
	"strings"
	"testing"
)

// TestCheckIntegrity tests the PDF integrity checking functionality
func TestCheckIntegrity(t *testing.T) {
	tests := []struct {
		name           string
		data           string
		wantValid      bool
		wantHeader     bool
		wantEOF        bool
		wantStartxref  bool
		wantTruncated  bool
		wantIssueCount int
	}{
		{
			name:           "valid minimal PDF",
			data:           "%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\nxref\n0 2\n0000000000 65535 f \n0000000009 00000 n \ntrailer\n<< /Root 1 0 R /Size 2 >>\nstartxref\n82\n%%EOF",
			wantValid:      true,
			wantHeader:     true,
			wantEOF:        true,
			wantStartxref:  true,
			wantTruncated:  false,
			wantIssueCount: 0,
		},
		{
			name:           "missing header",
			data:           "Not a PDF file",
			wantValid:      false,
			wantHeader:     false,
			wantEOF:        false,
			wantStartxref:  false,
			wantTruncated:  false,
			wantIssueCount: 1,
		},
		{
			name:           "truncated PDF (missing EOF)",
			data:           "%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\nxref",
			wantValid:      true,
			wantHeader:     true,
			wantEOF:        false,
			wantStartxref:  false,
			wantTruncated:  true,
			wantIssueCount: 2, // missing EOF and startxref
		},
		{
			name:           "file too small",
			data:           "%PDF-1.4",
			wantValid:      false, // too small to be valid
			wantHeader:     false, // can't check header properly on tiny file
			wantEOF:        false,
			wantStartxref:  false,
			wantTruncated:  false, // marked as invalid first
			wantIssueCount: 1,     // file too small
		},
		{
			name:           "PDF with BOM prefix",
			data:           "\xef\xbb\xbf%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\nxref\n0 2\n0000000000 65535 f \n0000000009 00000 n \ntrailer\n<< /Root 1 0 R /Size 2 >>\nstartxref\n85\n%%EOF",
			wantValid:      true,
			wantHeader:     true,
			wantEOF:        true,
			wantStartxref:  true,
			wantTruncated:  false,
			wantIssueCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(tt.data)
			reader := bytes.NewReader(data)
			status := CheckIntegrity(reader, int64(len(data)))

			if status.HasValidHeader != tt.wantHeader {
				t.Errorf("HasValidHeader = %v, want %v", status.HasValidHeader, tt.wantHeader)
			}
			if status.HasValidEOF != tt.wantEOF {
				t.Errorf("HasValidEOF = %v, want %v", status.HasValidEOF, tt.wantEOF)
			}
			if status.HasStartxref != tt.wantStartxref {
				t.Errorf("HasStartxref = %v, want %v", status.HasStartxref, tt.wantStartxref)
			}
			if status.IsTruncated != tt.wantTruncated {
				t.Errorf("IsTruncated = %v, want %v", status.IsTruncated, tt.wantTruncated)
			}
			if len(status.Issues) != tt.wantIssueCount {
				t.Errorf("Issue count = %d, want %d. Issues: %v", len(status.Issues), tt.wantIssueCount, status.Issues)
			}
		})
	}
}

// TestDefaultRecoveryOptions tests that default options are sensible
func TestDefaultRecoveryOptions(t *testing.T) {
	opts := DefaultRecoveryOptions()

	if opts.MaxSearchSize <= 0 {
		t.Error("MaxSearchSize should be positive")
	}
	if opts.MaxSearchSize < 1024*1024 {
		t.Error("MaxSearchSize should be at least 1MB")
	}
	if !opts.AllowTruncated {
		t.Error("AllowTruncated should be true by default")
	}
	if !opts.AllowMissingXref {
		t.Error("AllowMissingXref should be true by default")
	}
	if !opts.AllowMissingTrailer {
		t.Error("AllowMissingTrailer should be true by default")
	}
}

// TestFindStartxrefEnhanced tests the enhanced startxref finding
func TestFindStartxrefEnhanced(t *testing.T) {
	tests := []struct {
		name       string
		data       string
		wantOffset int64
		wantErr    bool
	}{
		{
			name:       "standard startxref at end",
			data:       "... some content ...\nstartxref\n12345\n%%EOF",
			wantOffset: 12345,
			wantErr:    false,
		},
		{
			name:       "startxref with spaces",
			data:       "content\nstartxref  \n  67890  \n%%EOF",
			wantOffset: 67890,
			wantErr:    false,
		},
		{
			name:       "missing startxref",
			data:       "content without startxref\n%%EOF",
			wantOffset: 0,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(tt.data)
			reader := bytes.NewReader(data)

			_, xrefOffset, err := findStartxrefEnhanced(reader, int64(len(data)), nil)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if xrefOffset != tt.wantOffset {
					t.Errorf("xrefOffset = %d, want %d", xrefOffset, tt.wantOffset)
				}
			}
		})
	}
}

// TestRebuildXrefFromObjects tests object marker detection
func TestRebuildXrefFromObjects(t *testing.T) {
	// Create a simple PDF-like content with object markers
	data := []byte(`%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Count 1 /Kids [3 0 R] >>
endobj
3 0 obj
<< /Type /Page /MediaBox [0 0 612 792] >>
endobj
`)

	xrefTable, err := rebuildXrefFromObjects(data)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if len(xrefTable) < 3 {
		t.Errorf("Expected at least 3 objects, got %d", len(xrefTable))
	}

	// Verify objects 1, 2, 3 are found
	foundObjects := make(map[uint32]bool)
	for _, entry := range xrefTable {
		if entry.ptr.id > 0 {
			foundObjects[entry.ptr.id] = true
		}
	}

	for _, id := range []uint32{1, 2, 3} {
		if !foundObjects[id] {
			t.Errorf("Object %d not found in rebuilt xref table", id)
		}
	}
}

// TestFindTrailerDict tests trailer dictionary extraction
func TestFindTrailerDict(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		wantRoot bool
		wantSize bool
		wantErr  bool
	}{
		{
			name:     "standard trailer",
			data:     "...\ntrailer\n<< /Root 1 0 R /Size 10 >>\nstartxref\n100\n%%EOF",
			wantRoot: true,
			wantSize: true,
			wantErr:  false,
		},
		{
			name:     "no trailer",
			data:     "just some random content without trailer",
			wantRoot: false,
			wantSize: false,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(tt.data)
			trailer, err := findTrailerDict(data)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if tt.wantRoot && trailer["Root"] == nil {
				t.Error("Expected Root in trailer but not found")
			}
			if tt.wantSize && trailer["Size"] == nil {
				t.Error("Expected Size in trailer but not found")
			}
		})
	}
}

// TestFindRootObject tests catalog object detection
func TestFindRootObject(t *testing.T) {
	data := []byte(`%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Count 0 /Kids [] >>
endobj
`)

	rootRef := findRootObject(data)

	if rootRef.id == 0 {
		t.Error("Failed to find root object")
	}

	if rootRef.id != 1 {
		t.Errorf("Expected root object ID 1, got %d", rootRef.id)
	}
}

// TestRecoveryIntegration tests the full recovery flow
func TestRecoveryIntegration(t *testing.T) {
	// Create a minimal valid PDF
	validPDF := strings.Join([]string{
		"%PDF-1.4",
		"1 0 obj",
		"<< /Type /Catalog /Pages 2 0 R >>",
		"endobj",
		"2 0 obj",
		"<< /Type /Pages /Count 0 /Kids [] >>",
		"endobj",
		"xref",
		"0 3",
		"0000000000 65535 f ",
		"0000000009 00000 n ",
		"0000000058 00000 n ",
		"trailer",
		"<< /Root 1 0 R /Size 3 >>",
		"startxref",
		"113",
		"%%EOF",
	}, "\n")

	data := []byte(validPDF)
	reader := bytes.NewReader(data)

	// Test that valid PDF passes integrity check
	status := CheckIntegrity(reader, int64(len(data)))
	if !status.IsValid {
		t.Errorf("Valid PDF should pass integrity check. Issues: %v", status.Issues)
	}

	// Test that standard reader can parse it
	r, err := NewReader(reader, int64(len(data)))
	if err != nil {
		t.Errorf("Standard reader failed on valid PDF: %v", err)
	} else {
		if r.trailer == nil {
			t.Error("Reader should have parsed trailer")
		}
		if r.trailer["Root"] == nil {
			t.Error("Trailer should have Root reference")
		}
	}
}

// TestIntegrityStatusIssues tests that issues are correctly reported
func TestIntegrityStatusIssues(t *testing.T) {
	// PDF without EOF marker
	noEOF := []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj")

	status := CheckIntegrity(bytes.NewReader(noEOF), int64(len(noEOF)))

	if status.HasValidEOF {
		t.Error("Should detect missing EOF")
	}
	if !status.IsTruncated {
		t.Error("Should mark as truncated when EOF is missing")
	}

	hasEOFIssue := false
	for _, issue := range status.Issues {
		if strings.Contains(issue, "EOF") {
			hasEOFIssue = true
			break
		}
	}
	if !hasEOFIssue {
		t.Error("Should report EOF issue")
	}
}
