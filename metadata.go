// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"strings"
	"time"
)

// Metadata represents PDF document metadata
type Metadata struct {
	Title        string            // Document title
	Author       string            // Author name
	Subject      string            // Document subject
	Keywords     []string          // Keywords
	Creator      string            // Application that created the document
	Producer     string            // PDF producer (converter)
	CreationDate time.Time         // Creation date
	ModDate      time.Time         // Last modification date
	Trapped      string            // Trapping information (True/False/Unknown)
	Custom       map[string]string // Custom metadata fields
}

// GetMetadata extracts metadata from the PDF document
func (r *Reader) GetMetadata() (Metadata, error) {
	meta := Metadata{
		Custom: make(map[string]string),
	}

	// Get Info dictionary from trailer
	info := r.Trailer().Key("Info")
	if info.Kind() == Null {
		// No metadata available
		return meta, nil
	}

	// Extract standard metadata fields
	meta.Title = decodeMetadataString(info.Key("Title"))
	meta.Author = decodeMetadataString(info.Key("Author"))
	meta.Subject = decodeMetadataString(info.Key("Subject"))
	meta.Creator = decodeMetadataString(info.Key("Creator"))
	meta.Producer = decodeMetadataString(info.Key("Producer"))
	meta.Trapped = info.Key("Trapped").Name()

	// Parse keywords (may be comma-separated)
	keywordsStr := decodeMetadataString(info.Key("Keywords"))
	if keywordsStr != "" {
		// Split by comma or semicolon
		keywords := strings.FieldsFunc(keywordsStr, func(r rune) bool {
			return r == ',' || r == ';'
		})
		for _, kw := range keywords {
			kw = strings.TrimSpace(kw)
			if kw != "" {
				meta.Keywords = append(meta.Keywords, kw)
			}
		}
	}

	// Parse dates
	meta.CreationDate = parsePDFDate(info.Key("CreationDate"))
	meta.ModDate = parsePDFDate(info.Key("ModDate"))

	// Extract custom fields (any key not in standard set)
	standardKeys := map[string]bool{
		"Title":        true,
		"Author":       true,
		"Subject":      true,
		"Keywords":     true,
		"Creator":      true,
		"Producer":     true,
		"CreationDate": true,
		"ModDate":      true,
		"Trapped":      true,
	}

	if info.Kind() == Dict {
		for _, key := range info.Keys() {
			if !standardKeys[key] {
				meta.Custom[key] = decodeMetadataString(info.Key(key))
			}
		}
	}

	// Try to get XMP metadata if available
	catalog := r.Trailer().Key("Root")
	xmpMetadata := catalog.Key("Metadata")
	if xmpMetadata.Kind() == Stream {
		// XMP metadata is available but requires XML parsing
		// For now, we'll just note its presence in custom fields
		meta.Custom["_HasXMP"] = "true"
	}

	return meta, nil
}

// decodeMetadataString decodes a PDF string value to Go string
func decodeMetadataString(v Value) string {
	if v.Kind() == Null {
		return ""
	}

	s := v.RawString()
	if s == "" {
		return ""
	}

	// Check for UTF-16 BOM
	if len(s) >= 2 && s[0] == 0xFE && s[1] == 0xFF {
		// UTF-16 BE
		return decodeUTF16BE([]byte(s[2:]))
	}

	// Check for UTF-8 BOM
	if len(s) >= 3 && s[0] == 0xEF && s[1] == 0xBB && s[2] == 0xBF {
		return s[3:]
	}

	// Assume PDFDocEncoding (similar to Latin-1 but with some differences)
	return decodePDFDocEncoding(s)
}

// decodePDFDocEncoding decodes PDFDocEncoding string
func decodePDFDocEncoding(s string) string {
	// PDFDocEncoding is identical to ISO Latin 1 for codes 0-127
	// and uses a specific mapping for 128-255
	// For simplicity, we'll treat it as Latin-1 for common cases
	// A full implementation would use the PDFDocEncoding table
	return s
}

// decodeUTF16BE decodes UTF-16 Big Endian bytes to string
func decodeUTF16BE(b []byte) string {
	if len(b)%2 != 0 {
		return ""
	}

	runes := make([]rune, 0, len(b)/2)
	for i := 0; i < len(b); i += 2 {
		r := rune(b[i])<<8 | rune(b[i+1])
		runes = append(runes, r)
	}
	return string(runes)
}

// parsePDFDate parses a PDF date string to time.Time
// PDF date format: D:YYYYMMDDHHmmSSOHH'mm'
// Example: D:20240318143022+08'00'
func parsePDFDate(v Value) time.Time {
	if v.Kind() == Null {
		return time.Time{}
	}

	s := v.RawString()
	if s == "" || !strings.HasPrefix(s, "D:") {
		return time.Time{}
	}

	s = s[2:] // Remove "D:" prefix

	// Extract date components
	if len(s) < 14 {
		// Not enough data for a complete date
		return time.Time{}
	}

	year := parseInt(s[0:4])
	month := parseInt(s[4:6])
	day := parseInt(s[6:8])
	hour := parseInt(s[8:10])
	minute := parseInt(s[10:12])
	second := parseInt(s[12:14])

	// Parse timezone if present
	var loc *time.Location
	if len(s) > 14 {
		tzStr := s[14:]
		loc = parsePDFTimezone(tzStr)
	}
	if loc == nil {
		loc = time.UTC
	}

	return time.Date(year, time.Month(month), day, hour, minute, second, 0, loc)
}

// parsePDFTimezone parses PDF timezone string (e.g., "+08'00'", "-05'00'", "Z")
func parsePDFTimezone(s string) *time.Location {
	if s == "" || s == "Z" {
		return time.UTC
	}

	if len(s) < 3 {
		return nil
	}

	sign := 1
	if s[0] == '-' {
		sign = -1
	} else if s[0] != '+' {
		return nil
	}

	// Parse hour offset
	hourOffset := parseInt(s[1:3])

	// Parse minute offset if present
	minuteOffset := 0
	if len(s) >= 6 && s[3] == '\'' {
		minuteOffset = parseInt(s[4:6])
	}

	totalOffset := sign * (hourOffset*3600 + minuteOffset*60)
	return time.FixedZone("PDF", totalOffset)
}

// parseInt parses a string to int, returns 0 on error
func parseInt(s string) int {
	result := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			result = result*10 + int(c-'0')
		} else {
			return 0
		}
	}
	return result
}

// SetMetadata sets metadata fields in the PDF (for future write support)
// Currently not implemented as the library is read-only
func (r *Reader) SetMetadata(meta Metadata) error {
	return &PDFError{
		Op:  "set metadata",
		Err: ErrUnsupportedVersion, // Reuse this error for "not implemented"
	}
}

// GetDocumentInfo returns a formatted string with document information
func (m Metadata) String() string {
	var b strings.Builder

	if m.Title != "" {
		b.WriteString("Title: " + m.Title + "\n")
	}
	if m.Author != "" {
		b.WriteString("Author: " + m.Author + "\n")
	}
	if m.Subject != "" {
		b.WriteString("Subject: " + m.Subject + "\n")
	}
	if len(m.Keywords) > 0 {
		b.WriteString("Keywords: " + strings.Join(m.Keywords, ", ") + "\n")
	}
	if m.Creator != "" {
		b.WriteString("Creator: " + m.Creator + "\n")
	}
	if m.Producer != "" {
		b.WriteString("Producer: " + m.Producer + "\n")
	}
	if !m.CreationDate.IsZero() {
		b.WriteString("Created: " + m.CreationDate.Format(time.RFC3339) + "\n")
	}
	if !m.ModDate.IsZero() {
		b.WriteString("Modified: " + m.ModDate.Format(time.RFC3339) + "\n")
	}
	if m.Trapped != "" {
		b.WriteString("Trapped: " + m.Trapped + "\n")
	}

	if len(m.Custom) > 0 {
		b.WriteString("Custom fields:\n")
		for key, value := range m.Custom {
			if !strings.HasPrefix(key, "_") { // Skip internal fields
				b.WriteString("  " + key + ": " + value + "\n")
			}
		}
	}

	return b.String()
}
