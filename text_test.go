package pdf

import "testing"

func TestIdentityCMapDecode(t *testing.T) {
	enc := &identityCMap{width: 2}
	got := enc.Decode(string([]byte{0x00, 0x41, 0x00, 0x42}))
	if got != "AB" {
		t.Fatalf("Decode mismatch: got %q, want %q", got, "AB")
	}

	enc = &identityCMap{width: 1}
	got = enc.Decode(string([]byte{0x61, 0x62}))
	if got != "ab" {
		t.Fatalf("Decode mismatch for width=1: got %q, want %q", got, "ab")
	}
}

func TestIsPDFDocEncoded(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"hello", true},     // ASCII characters
		{"\x00", false},     // Control character mapped to noRune
		{"\xfe\xff", false}, // UTF-16 BOM
		{"", true},          // Empty string
	}

	for _, tt := range tests {
		result := isPDFDocEncoded(tt.input)
		if result != tt.expected {
			t.Errorf("isPDFDocEncoded(%q) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}

func TestPDFDocDecode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"}, // No encoding needed
		{"\x80", "\u2022"}, // Bullet symbol
		{"\x81", "\u2020"}, // Dagger symbol
	}

	for _, tt := range tests {
		result := pdfDocDecode(tt.input)
		if result != tt.expected {
			t.Errorf("pdfDocDecode(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

func TestIsUTF16(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"\xfe\xff", true},         // BOM with even length
		{"\xfe\xff\x00\x41", true}, // Valid UTF-16 BE with content
		{"hello", false},           // ASCII
		{"", false},                // Empty
		{"\xfe", false},            // Partial BOM
	}

	for _, tt := range tests {
		result := isUTF16(tt.input)
		if result != tt.expected {
			t.Errorf("isUTF16(%q) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}

func TestUTF16Decode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"\xfe\xff\x00\x41\x00\x42", "\ufeffAB"},                            // UTF-16 BE "AB" with BOM
		{"\xfe\xff\x00\x48\x00\x65\x00\x6c\x00\x6c\x00\x6f", "\ufeffHello"}, // UTF-16 BE "Hello" with BOM
	}

	for _, tt := range tests {
		result := utf16Decode(tt.input)
		if result != tt.expected {
			t.Errorf("utf16Decode(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

func TestUTF16DecodeOddLength(t *testing.T) {
	// Test that odd-length strings don't cause panic
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "single byte",
			input:    "\x00",
			expected: "",
		},
		{
			name:     "three bytes",
			input:    "\x00\x41\x00",
			expected: "A",
		},
		{
			name:     "five bytes",
			input:    "\x00\x41\x00\x42\x00",
			expected: "AB",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("unexpected panic: %v", r)
				}
			}()

			result := utf16Decode(tc.input)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}
