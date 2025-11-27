// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"sync"
	"testing"
)

// ===================== TextBlock Pool Tests =====================

func TestGetTextBlock(t *testing.T) {
	tb := GetTextBlock()
	if tb == nil {
		t.Fatal("GetTextBlock returned nil")
	}
	if len(tb.Texts) != 0 {
		t.Errorf("expected empty Texts slice, got len=%d", len(tb.Texts))
	}
	if tb.MinX != 0 || tb.MaxX != 0 || tb.MinY != 0 || tb.MaxY != 0 {
		t.Error("expected all bounds to be 0")
	}
	if tb.AvgFontSize != 0 {
		t.Errorf("expected AvgFontSize=0, got %f", tb.AvgFontSize)
	}
}

func TestPutTextBlock(t *testing.T) {
	// Test nil handling
	PutTextBlock(nil) // Should not panic

	// Test normal case
	tb := GetTextBlock()
	tb.Texts = append(tb.Texts, Text{S: "test"})
	tb.MinX = 10
	tb.MaxX = 100
	PutTextBlock(tb)

	// Get another block - may or may not be the same one
	tb2 := GetTextBlock()
	if tb2 == nil {
		t.Fatal("GetTextBlock returned nil after PutTextBlock")
	}
	if len(tb2.Texts) != 0 {
		t.Errorf("expected reset Texts slice, got len=%d", len(tb2.Texts))
	}
}

func TestPutTextBlockOversized(t *testing.T) {
	// Create a TextBlock with oversized capacity
	tb := &TextBlock{
		Texts: make([]Text, 0, 2000), // > 1024
	}
	PutTextBlock(tb) // Should not add to pool due to size limit
}

func TestPutTextBlocks(t *testing.T) {
	blocks := make([]*TextBlock, 5)
	for i := range blocks {
		blocks[i] = GetTextBlock()
		blocks[i].Texts = append(blocks[i].Texts, Text{S: "test"})
	}

	PutTextBlocks(blocks) // Should not panic

	// Verify pool still works
	tb := GetTextBlock()
	if tb == nil {
		t.Fatal("GetTextBlock returned nil after PutTextBlocks")
	}
}

func TestTextBlockPoolConcurrency(t *testing.T) {
	var wg sync.WaitGroup
	iterations := 100

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				tb := GetTextBlock()
				tb.Texts = append(tb.Texts, Text{S: "concurrent"})
				tb.MinX = float64(j)
				PutTextBlock(tb)
			}
		}()
	}
	wg.Wait()
}

// ===================== Text Slice Pool Tests =====================

func TestGetTextSlice(t *testing.T) {
	// Test with small capacity
	s := GetTextSlice(10)
	if s == nil {
		t.Fatal("GetTextSlice returned nil")
	}
	if len(s) != 0 {
		t.Errorf("expected len=0, got %d", len(s))
	}
	if cap(s) < 10 {
		t.Errorf("expected cap >= 10, got %d", cap(s))
	}

	// Test with capacity larger than pool default
	s2 := GetTextSlice(500)
	if s2 == nil {
		t.Fatal("GetTextSlice with large cap returned nil")
	}
	if cap(s2) < 500 {
		t.Errorf("expected cap >= 500, got %d", cap(s2))
	}
}

func TestPutTextSlice(t *testing.T) {
	s := GetTextSlice(100)
	s = append(s, Text{S: "test1"}, Text{S: "test2"})
	PutTextSlice(s)

	// Get another slice
	s2 := GetTextSlice(50)
	if s2 == nil {
		t.Fatal("GetTextSlice returned nil after PutTextSlice")
	}
	if len(s2) != 0 {
		t.Errorf("expected reset slice, got len=%d", len(s2))
	}
}

func TestPutTextSliceOversized(t *testing.T) {
	// Create an oversized slice
	s := make([]Text, 0, 5000) // > 4096
	PutTextSlice(s)            // Should not add to pool
}

// ===================== Int Slice Pool Tests =====================

func TestGetIntSlice(t *testing.T) {
	s := GetIntSlice(100)
	if s == nil {
		t.Fatal("GetIntSlice returned nil")
	}
	if len(s) != 100 {
		t.Errorf("expected len=100, got %d", len(s))
	}

	// Test with size larger than pool default
	s2 := GetIntSlice(500)
	if len(s2) != 500 {
		t.Errorf("expected len=500, got %d", len(s2))
	}
}

func TestPutIntSlice(t *testing.T) {
	s := GetIntSlice(100)
	for i := range s {
		s[i] = i
	}
	PutIntSlice(s)

	// Get another slice
	s2 := GetIntSlice(50)
	if s2 == nil {
		t.Fatal("GetIntSlice returned nil after PutIntSlice")
	}
	if len(s2) != 50 {
		t.Errorf("expected len=50, got %d", len(s2))
	}
}

func TestPutIntSliceOversized(t *testing.T) {
	s := make([]int, 5000) // > 4096
	PutIntSlice(s)         // Should not add to pool
}

func TestIntSlicePoolConcurrency(t *testing.T) {
	var wg sync.WaitGroup
	iterations := 100

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				s := GetIntSlice(50)
				for k := range s {
					s[k] = k
				}
				PutIntSlice(s)
			}
		}()
	}
	wg.Wait()
}

// ===================== String Intern Tests =====================

func TestInternRuneASCII(t *testing.T) {
	// Test ASCII characters use pre-allocated strings
	for i := 0; i < 256; i++ {
		r := rune(i)
		s := InternRune(r)
		expected := string([]byte{byte(i)})
		if s != expected {
			t.Errorf("InternRune(%d) = %q, expected %q", i, s, expected)
		}
	}
}

func TestInternRuneNonASCII(t *testing.T) {
	// Test non-ASCII characters (Unicode > 255)
	// Note: Latin-1 supplement chars (128-255) like ñ, ü, é are treated as single bytes
	testCases := []rune{'中', '文', '日', '本', '語', '😀', '🎉'}

	for _, r := range testCases {
		s1 := InternRune(r)
		s2 := InternRune(r)

		expected := string(r)
		if s1 != expected {
			t.Errorf("InternRune(%q) = %q, expected %q", string(r), s1, expected)
		}

		// Same rune should return same interned string
		if s1 != s2 {
			t.Errorf("InternRune(%q) returned different strings: %q vs %q", string(r), s1, s2)
		}
	}
}

func TestInternRuneLatin1(t *testing.T) {
	// Test Latin-1 supplement characters (128-255)
	// These are treated as single bytes for efficiency
	testCases := []rune{'ñ', 'ü', 'é'} // 241, 252, 233

	for _, r := range testCases {
		s := InternRune(r)
		// Should return single byte string
		if len(s) != 1 {
			t.Errorf("InternRune(%q) returned len=%d, expected 1", string(r), len(s))
		}
		// The byte value should match the rune value
		if s[0] != byte(r) {
			t.Errorf("InternRune(%q) byte=%d, expected %d", string(r), s[0], byte(r))
		}
	}
}

func TestInternRuneConcurrency(t *testing.T) {
	var wg sync.WaitGroup
	runes := []rune{'中', '文', '日', '本', '語', 'A', 'B', 'C'}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				for _, r := range runes {
					s := InternRune(r)
					if s != string(r) {
						t.Errorf("InternRune(%q) = %q, expected %q", string(r), s, string(r))
					}
				}
			}
		}()
	}
	wg.Wait()
}

func TestSingleCharStringsInit(t *testing.T) {
	// Verify all 256 single char strings are initialized
	for i := 0; i < 256; i++ {
		expected := string([]byte{byte(i)})
		if singleCharStrings[i] != expected {
			t.Errorf("singleCharStrings[%d] = %q, expected %q", i, singleCharStrings[i], expected)
		}
	}
}

// ===================== Benchmarks =====================

func BenchmarkGetPutTextBlock(b *testing.B) {
	for i := 0; i < b.N; i++ {
		tb := GetTextBlock()
		tb.Texts = append(tb.Texts, Text{S: "bench"})
		PutTextBlock(tb)
	}
}

func BenchmarkGetPutTextSlice(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := GetTextSlice(100)
		s = append(s, Text{S: "bench"})
		PutTextSlice(s)
	}
}

func BenchmarkGetPutIntSlice(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := GetIntSlice(100)
		s[0] = i
		PutIntSlice(s)
	}
}

func BenchmarkInternRuneASCII(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = InternRune('A')
	}
}

func BenchmarkInternRuneNonASCII(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = InternRune('中')
	}
}

func BenchmarkStringConversionBaseline(b *testing.B) {
	r := '中'
	for i := 0; i < b.N; i++ {
		_ = string(r)
	}
}
