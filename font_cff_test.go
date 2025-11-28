// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"io"
	"testing"
)

func TestNewCFFFont(t *testing.T) {
	// Test with minimal valid CFF data
	// This is a very basic test - real CFF parsing would need actual font data
	data := []byte{
		1, 0, 4, 4, // header: major=1, minor=0, hdrSize=4, offSize=4
		0, 0, 0, 1, // Name INDEX: count=0, so no names
		0, 0, 0, 1, // Top DICT INDEX: count=0, so no top dict
		0, 0, 0, 1, // String INDEX: count=0, so no strings
		0, 0, 0, 1, // Global Subr INDEX: count=0, so no subrs
	}

	font, err := NewCFFFont(data)
	if err != nil {
		t.Fatalf("NewCFFFont failed: %v", err)
	}
	if font == nil {
		t.Fatal("NewCFFFont returned nil")
	}

	if font.Header.Major != 1 {
		t.Errorf("Expected major version 1, got %d", font.Header.Major)
	}
	if font.Header.Minor != 0 {
		t.Errorf("Expected minor version 0, got %d", font.Header.Minor)
	}
}

func TestCFFCharStringDecoder(t *testing.T) {
	// Test basic CharString decoding
	data := []byte{
		139,      // integer 139-139 = 0
		247, 108, // integer (247-247)*256 + 108 + 108 = 108
		251, 108, // integer -(251-251)*256 -108 -108 = -108
		28, 0, 1, // 16-bit integer 1
		14, // endchar
	}

	decoder := NewCFFCharStringDecoder(data)
	commands, err := decoder.Decode()
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if len(commands) == 0 {
		t.Fatal("Expected some commands")
	}

	// Check that we have an endchar command
	found := false
	for _, cmd := range commands {
		if cmd == "endchar" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected endchar command")
	}
}

func TestCFFDictParsing(t *testing.T) {
	// Test basic DICT parsing with version operator
	data := []byte{
		139, // integer operand 139
		0,   // version operator (0)
	}

	font := &CFFFont{}
	dict, err := font.parseDict(data)
	if err != nil {
		t.Fatalf("parseDict failed: %v", err)
	}

	// Just check that parsing doesn't crash and returns a dict
	if dict == nil {
		t.Error("Expected non-nil dict")
	}
	if len(dict.Data) == 0 {
		t.Error("Expected some data in dict")
	}
}

func TestCFFIndexParsing(t *testing.T) {
	// Test INDEX parsing
	data := []byte{
		0, 0, 0, 1, // count = 0
	}

	r := &bytesReader{data: data}
	font := &CFFFont{}
	index, err := font.parseIndex(r)
	if err != nil {
		t.Fatalf("parseIndex failed: %v", err)
	}

	if index.Count != 0 {
		t.Errorf("Expected count 0, got %d", index.Count)
	}
	if len(index.Data) != 0 {
		t.Errorf("Expected 0 data objects, got %d", len(index.Data))
	}
}

func BenchmarkCFFCharStringDecoder_Decode(b *testing.B) {
	data := []byte{
		139, 247, 108, 251, 108, 28, 0, 1, 14,
	}

	decoder := NewCFFCharStringDecoder(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoder.pos = 0
		_, _ = decoder.Decode()
	}
}

func BenchmarkCFFDictParsing(b *testing.B) {
	data := []byte{
		0, 139, 1, 139, 2, 139, 12, 0, 139,
	}

	font := &CFFFont{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = font.parseDict(data)
	}
}

// bytesReader implements io.Reader for testing
type bytesReader struct {
	data []byte
	pos  int
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *bytesReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		r.pos = int(offset)
	case io.SeekCurrent:
		r.pos += int(offset)
	case io.SeekEnd:
		r.pos = len(r.data) + int(offset)
	}
	if r.pos < 0 {
		r.pos = 0
	}
	if r.pos > len(r.data) {
		r.pos = len(r.data)
	}
	return int64(r.pos), nil
}
