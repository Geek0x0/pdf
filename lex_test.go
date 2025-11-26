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

func TestReadArrayStopsOnEOF(t *testing.T) {
	input := "[1 2 3" // missing closing bracket should not spin forever
	reader := strings.NewReader(input)
	buf := newBuffer(reader, 0)
	buf.allowEOF = true

	// consume the opening bracket to mirror readObject behavior
	if tok := buf.readToken(); tok != keyword("[") {
		t.Fatalf("expected opening bracket, got %v", tok)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for unterminated array")
		}
	}()

	buf.readArray()
}

func TestReadArrayTooLarge(t *testing.T) {
	var bld strings.Builder
	bld.WriteByte('[')
	for i := 0; i <= maxArrayElements; i++ { // limit + 1 elements
		bld.WriteString("0 ")
	}
	bld.WriteByte(']')

	buf := newBuffer(strings.NewReader(bld.String()), 0)
	buf.allowEOF = true

	// consume the opening bracket
	if tok := buf.readToken(); tok != keyword("[") {
		t.Fatalf("expected opening bracket, got %v", tok)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for oversized array")
		}
	}()

	buf.readArray()
}

func TestReadDictStreamWithoutNewline(t *testing.T) {
	// Data follows stream directly without newline, should be tolerated and not panic
	src := "<< /Length 3 >>stream ABC endstream"
	buf := newBuffer(strings.NewReader(src), 0)
	buf.allowEOF = true

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()

	obj := buf.readObject()
	strm, ok := obj.(stream)
	if !ok {
		t.Fatalf("expected stream, got %T", obj)
	}
	if strm.hdr["Length"] != int64(3) {
		t.Fatalf("expected Length 3, got %v", strm.hdr["Length"])
	}
	if strm.offset <= 0 {
		t.Fatalf("unexpected stream offset: %d", strm.offset)
	}
}

func TestReadKeywordInvalidNumberFallback(t *testing.T) {
	// When parsing oversized integer fails, should fallback to keyword instead of panic
	src := "999999999999999999999999999999"
	buf := newBuffer(strings.NewReader(src), 0)
	buf.allowEOF = true
	tok := buf.readKeyword()
	if _, ok := tok.(keyword); !ok {
		t.Fatalf("expected keyword fallback, got %T", tok)
	}
}

func TestReadDictWithNonNameKey(t *testing.T) {
	// Invalid key should be tolerated and end parsing, not panic
	src := "<< /A 1 123 456 >>"
	buf := newBuffer(strings.NewReader(src), 0)
	buf.allowEOF = true

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()

	obj := buf.readObject()
	d, ok := obj.(dict)
	if !ok {
		t.Fatalf("expected dict, got %T", obj)
	}
	if d[name("A")] != int64(1) {
		t.Fatalf("expected key /A = 1, got %v", d[name("A")])
	}
}

func TestReadHexStringOddLength(t *testing.T) {
	// Per PDF spec, if there's an odd number of hex digits,
	// the final digit is assumed to be followed by 0.
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "odd hex digits",
			input:    "<3110D3FB9D508FF7453BB3254E18B10>",
			expected: "\x31\x10\xD3\xFB\x9D\x50\x8F\xF7\x45\x3B\xB3\x25\x4E\x18\xB1\x00",
		},
		{
			name:     "single hex digit",
			input:    "<A>",
			expected: "\xA0",
		},
		{
			name:     "even hex digits",
			input:    "<48656C6C6F>",
			expected: "Hello",
		},
		{
			name:     "empty hex string",
			input:    "<>",
			expected: "",
		},
		{
			name:     "hex with spaces odd",
			input:    "<4 8 6 5 6 C 6 C 6 F 0>",
			expected: "Hello\x00",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := newBuffer(strings.NewReader(tc.input), 0)
			buf.allowEOF = true

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("unexpected panic: %v", r)
				}
			}()

			// Skip the initial '<'
			buf.readByte()
			result := buf.readHexString()
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}
