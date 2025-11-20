package pdf

import (
	"strings"
	"testing"
)

func TestBufferSeek(t *testing.T) {
	input := "Hello World"
	reader := strings.NewReader(input)
	buf := newBuffer(reader, 0)

	// Test initial state
	if buf.offset != 0 {
		t.Errorf("Expected initial offset 0, got %d", buf.offset)
	}

	// Seek to position 5
	buf.seek(5)
	if buf.offset != 5 {
		t.Errorf("Expected offset 5 after seek, got %d", buf.offset)
	}
	if buf.pos != 0 {
		t.Errorf("Expected pos 0 after seek, got %d", buf.pos)
	}
	if len(buf.buf) != 0 {
		t.Errorf("Expected empty buf after seek, got len %d", len(buf.buf))
	}
}

func TestBufferSeekForward(t *testing.T) {
	input := "Hello World Test"
	reader := strings.NewReader(input)
	buf := newBuffer(reader, 0)

	// Read a few bytes first
	b1 := buf.readByte()
	b2 := buf.readByte()
	if b1 != 'H' || b2 != 'e' {
		t.Errorf("Expected 'H','e', got %c,%c", b1, b2)
	}

	// Seek forward to position 5
	buf.seekForward(5)
	if buf.offset < 5 {
		t.Errorf("Expected offset >= 5, got %d", buf.offset)
	}

	// Read next byte should be at position 5
	b3 := buf.readByte()
	if b3 != ' ' {
		t.Errorf("Expected ' ' at position 5, got %c", b3)
	}
}

func TestBufferReadOffset(t *testing.T) {
	input := "Hello"
	reader := strings.NewReader(input)
	buf := newBuffer(reader, 0)

	// Initially at offset 0
	offset := buf.readOffset()
	if offset != 0 {
		t.Errorf("Expected read offset 0, got %d", offset)
	}

	// Read one byte
	buf.readByte()
	offset = buf.readOffset()
	if offset != 1 {
		t.Errorf("Expected read offset 1 after reading one byte, got %d", offset)
	}
}

func TestBufferUnreadByte(t *testing.T) {
	input := "Hello"
	reader := strings.NewReader(input)
	buf := newBuffer(reader, 0)

	// Read two bytes
	b1 := buf.readByte()
	b2 := buf.readByte()
	if b1 != 'H' || b2 != 'e' {
		t.Errorf("Expected 'H','e', got %c,%c", b1, b2)
	}

	// Unread one byte
	buf.unreadByte()
	offset := buf.readOffset()
	if offset != 1 {
		t.Errorf("Expected read offset 1 after unread, got %d", offset)
	}

	// Read again should get 'e' again
	b3 := buf.readByte()
	if b3 != 'e' {
		t.Errorf("Expected 'e' after unread, got %c", b3)
	}
}

func TestReadLiteralStringWithInvalidEscape(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "invalid escape sequence with special char",
			input:    "(test\\óstring)",
			expected: "testóstring",
		},
		{
			name:     "valid escape sequences",
			input:    "(test\\nline\\ttab)",
			expected: "test\nline\ttab",
		},
		{
			name:     "mixed valid and invalid escapes",
			input:    "(hello\\nworld\\ó)",
			expected: "hello\nworldó",
		},
		{
			name:     "backslash before parenthesis",
			input:    "(test\\(paren\\))",
			expected: "test(paren)",
		},
		{
			name:     "octal escape",
			input:    "(test\\101)",
			expected: "testA",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			buf := newBuffer(reader, 0)
			buf.allowEOF = true

			// Skip the opening parenthesis
			tok := buf.readToken()
			if str, ok := tok.(string); ok {
				if str != tt.expected {
					t.Errorf("Expected %q, got %q", tt.expected, str)
				}
			} else {
				t.Errorf("Expected string token, got %T", tok)
			}
		})
	}
}
