package pdf

import (
	"testing"
	"time"
)

func TestDecodeUTF16BE(t *testing.T) {
	tests := []struct {
		input    []byte
		expected string
	}{
		{[]byte{0x00, 0x48, 0x00, 0x65, 0x00, 0x6C, 0x00, 0x6C, 0x00, 0x6F}, "Hello"},
		{[]byte{0x4E, 0x2D}, "中"}, // Chinese character "中" in UTF-16BE
		{[]byte{0x00, 0x48}, "H"},
		{[]byte{}, ""},               // Empty input
		{[]byte{0x00}, ""},           // Odd length
		{[]byte{0x00, 0x00}, "\x00"}, // Null character
	}

	for _, test := range tests {
		result := decodeUTF16BE(test.input)
		if result != test.expected {
			t.Errorf("decodeUTF16BE(%v) = %q, expected %q", test.input, result, test.expected)
		}
	}
}

func TestSetMetadata(t *testing.T) {
	// Create a mock reader (we'll use a minimal implementation)
	reader := &Reader{}

	meta := Metadata{
		Title:   "Test Document",
		Author:  "Test Author",
		Subject: "Test Subject",
	}

	err := reader.SetMetadata(meta)
	if err == nil {
		t.Error("Expected SetMetadata to return an error")
	}

	// Check that it's the expected error type
	if pdfErr, ok := err.(*PDFError); ok {
		if pdfErr.Op != "set metadata" {
			t.Errorf("Expected operation 'set metadata', got %q", pdfErr.Op)
		}
	} else {
		t.Errorf("Expected PDFError, got %T", err)
	}
}

func TestParsePDFDate(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Time
	}{
		{"D:20240318143022+08'00'", time.Date(2024, 3, 18, 14, 30, 22, 0, time.FixedZone("+0800", 8*3600))},
		{"D:20240318143022Z", time.Date(2024, 3, 18, 14, 30, 22, 0, time.UTC)},
		{"D:20240318143022", time.Date(2024, 3, 18, 14, 30, 22, 0, time.UTC)}, // No timezone
		{"", time.Time{}},
		{"invalid", time.Time{}},
		{"D:20240318", time.Time{}}, // Too short
	}

	for _, test := range tests {
		val := Value{data: test.input}
		result := parsePDFDate(val)
		if !result.Equal(test.expected) {
			t.Errorf("parsePDFDate(%q) = %v, expected %v", test.input, result, test.expected)
		}
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"123", 123},
		{"0", 0},
		{"456", 456},
		{"", 0},
		{"abc", 0},
		{"123abc", 0}, // Stops at non-digit
		{"-456", 0},   // No negative support
	}

	for _, test := range tests {
		result := parseInt(test.input)
		if result != test.expected {
			t.Errorf("parseInt(%q) = %d, expected %d", test.input, result, test.expected)
		}
	}
}
