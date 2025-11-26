package pdf

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestASCIIHexDecoder tests the ASCIIHexDecode filter implementation
func TestASCIIHexDecoder(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple hex string",
			input:    "48656C6C6F>",
			expected: "Hello",
		},
		{
			name:     "hex with spaces",
			input:    "48 65 6C 6C 6F>",
			expected: "Hello",
		},
		{
			name:     "hex with newlines",
			input:    "48\n65\n6C\n6C\n6F>",
			expected: "Hello",
		},
		{
			name:     "odd number of digits",
			input:    "48656C6C6F0>",
			expected: "Hello\x00",
		},
		{
			name:     "single digit padded",
			input:    "4>",
			expected: "@", // 0x40
		},
		{
			name:     "empty string",
			input:    ">",
			expected: "",
		},
		{
			name:     "lowercase hex",
			input:    "48656c6c6f>",
			expected: "Hello",
		},
		{
			name:     "mixed case",
			input:    "48656C6c6F>",
			expected: "Hello",
		},
		{
			name:     "with tabs",
			input:    "48\t65\t6C\t6C\t6F>",
			expected: "Hello",
		},
		{
			name:     "PDF binary marker",
			input:    "25E2E3CFD3>", // %â㏓
			expected: "\x25\xE2\xE3\xCF\xD3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoder := newASCIIHexDecoder(strings.NewReader(tt.input))
			result, err := io.ReadAll(decoder)
			if err != nil {
				t.Fatalf("ReadAll failed: %v", err)
			}
			if string(result) != tt.expected {
				t.Errorf("expected %q (% X), got %q (% X)", tt.expected, []byte(tt.expected), string(result), result)
			}
		})
	}
}

// TestASCIIHexDecoderInFilter tests ASCIIHexDecode through applyFilter
func TestASCIIHexDecoderInFilter(t *testing.T) {
	input := "48656C6C6F>" // "Hello"
	rd := applyFilter(strings.NewReader(input), "ASCIIHexDecode", Value{})
	if rd == nil {
		t.Fatal("applyFilter returned nil")
	}

	result, err := io.ReadAll(rd)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	expected := "Hello"
	if string(result) != expected {
		t.Errorf("expected %q, got %q", expected, string(result))
	}
}

// TestASCIIHexDecoderLarge tests with larger data
func TestASCIIHexDecoderLarge(t *testing.T) {
	// Create a large hex string
	var buf bytes.Buffer
	expectedBytes := make([]byte, 1000)
	for i := range expectedBytes {
		expectedBytes[i] = byte(i % 256)
	}

	for _, b := range expectedBytes {
		buf.WriteString(strings.ToUpper(string([]byte{hexDigit(b >> 4), hexDigit(b & 0xF)})))
	}
	buf.WriteByte('>')

	decoder := newASCIIHexDecoder(strings.NewReader(buf.String()))
	result, err := io.ReadAll(decoder)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if !bytes.Equal(result, expectedBytes) {
		t.Errorf("result mismatch, got %d bytes, expected %d bytes", len(result), len(expectedBytes))
	}
}

// TestASCIIHexDecoderBuffered tests reading in chunks
func TestASCIIHexDecoderBuffered(t *testing.T) {
	input := "48656C6C6F20576F726C64>" // "Hello World"
	decoder := newASCIIHexDecoder(strings.NewReader(input))

	// Read in small chunks
	var result bytes.Buffer
	buf := make([]byte, 3)
	for {
		n, err := decoder.Read(buf)
		if n > 0 {
			result.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
	}

	expected := "Hello World"
	if result.String() != expected {
		t.Errorf("expected %q, got %q", expected, result.String())
	}
}

// Helper function to convert nibble to hex digit
func hexDigit(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'A' + (n - 10)
}
