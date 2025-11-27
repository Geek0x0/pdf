// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"sync"
)

// ===================== TextBlock Object Pool =====================
// Reduces GC pressure by reusing TextBlock objects

// textBlockPool provides reusable TextBlock objects
// Note: Do NOT pre-allocate Texts slice capacity here.
// The pool reuses objects, so pre-allocation only wastes memory on first use.
// After first use, the slice will have grown capacity that gets reused.
var textBlockPool = sync.Pool{
	New: func() interface{} {
		return &TextBlock{
			// Start with nil slice - it will grow as needed and be reused
			Texts: nil,
		}
	},
}

// GetTextBlock gets a TextBlock from pool
func GetTextBlock() *TextBlock {
	tb := textBlockPool.Get().(*TextBlock)
	// Safe reset - nil slice is valid for append
	if tb.Texts != nil {
		tb.Texts = tb.Texts[:0]
	}
	tb.MinX = 0
	tb.MaxX = 0
	tb.MinY = 0
	tb.MaxY = 0
	tb.AvgFontSize = 0
	tb.clusterIdx = -1 // Reset cluster index
	return tb
}

// PutTextBlock returns a TextBlock to pool
func PutTextBlock(tb *TextBlock) {
	if tb == nil {
		return
	}
	if cap(tb.Texts) > 1024 {
		return
	}
	if tb.Texts != nil {
		tb.Texts = tb.Texts[:0]
	}
	textBlockPool.Put(tb)
}

// PutTextBlocks returns multiple TextBlocks to pool
func PutTextBlocks(blocks []*TextBlock) {
	for _, tb := range blocks {
		PutTextBlock(tb)
	}
}

// ===================== Text Slice Pool =====================

var textSlicePool = sync.Pool{
	New: func() interface{} {
		s := make([]Text, 0, 256)
		return &s
	},
}

// GetTextSlice gets a Text slice from pool
func GetTextSlice(minCap int) []Text {
	sp := textSlicePool.Get().(*[]Text)
	s := *sp
	if cap(s) < minCap {
		return make([]Text, 0, minCap)
	}
	return s[:0]
}

// PutTextSlice returns a Text slice to pool
func PutTextSlice(s []Text) {
	if cap(s) > 4096 {
		return
	}
	s = s[:0]
	textSlicePool.Put(&s)
}

// ===================== Int Slice Pool (for union-find) =====================

var intSlicePool = sync.Pool{
	New: func() interface{} {
		s := make([]int, 0, 256)
		return &s
	},
}

// GetIntSlice gets an int slice from pool
func GetIntSlice(size int) []int {
	sp := intSlicePool.Get().(*[]int)
	s := *sp
	if cap(s) < size {
		return make([]int, size)
	}
	s = s[:size]
	return s
}

// PutIntSlice returns an int slice to pool
func PutIntSlice(s []int) {
	if cap(s) > 4096 {
		return
	}
	intSlicePool.Put(&s)
}

// ===================== String Intern Pool =====================
// Reduces string allocation for common characters

var (
	stringInternPool  = sync.Map{}
	singleCharStrings [256]string
	// Pre-allocate common CJK and Unicode characters
	commonUnicodeStrings = make(map[rune]string, 8192)
	unicodeInitOnce      sync.Once
)

func init() {
	for i := 0; i < 256; i++ {
		singleCharStrings[i] = string([]byte{byte(i)})
	}
}

// initCommonUnicode pre-allocates strings for common Unicode ranges
func initCommonUnicode() {
	// Common CJK Unified Ideographs (most frequently used Chinese characters)
	// Range: U+4E00 to U+9FFF (20,992 characters, but we only pre-alloc common ones)
	// Only allocate the most common ~3000 characters to balance memory vs allocation
	commonRanges := [][2]rune{
		{0x4E00, 0x5200}, // Common CJK block 1 (~512 chars)
		{0x5200, 0x5600}, // Common CJK block 2
		{0x5600, 0x5A00}, // Common CJK block 3
		{0x5A00, 0x5E00}, // Common CJK block 4
		{0x5E00, 0x6200}, // Common CJK block 5
		{0x6200, 0x6600}, // Common CJK block 6
		{0x3000, 0x3100}, // CJK Symbols and Punctuation
		{0xFF00, 0xFF70}, // Fullwidth ASCII variants
		{0x2000, 0x2070}, // General Punctuation
	}
	for _, r := range commonRanges {
		for c := r[0]; c < r[1]; c++ {
			commonUnicodeStrings[c] = string(c)
		}
	}
}

// InternRune converts a rune to interned string
func InternRune(r rune) string {
	if r < 256 {
		return singleCharStrings[byte(r)]
	}

	// Lazy init common Unicode characters
	unicodeInitOnce.Do(initCommonUnicode)

	// Check pre-allocated common Unicode
	if s, ok := commonUnicodeStrings[r]; ok {
		return s
	}

	// Fallback to sync.Map for other characters
	if interned, ok := stringInternPool.Load(r); ok {
		return interned.(string)
	}
	s := string(r)
	stringInternPool.Store(r, s)
	return s
}
