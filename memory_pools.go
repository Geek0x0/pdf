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
var textBlockPool = sync.Pool{
	New: func() interface{} {
		return &TextBlock{
			Texts: make([]Text, 0, 16),
		}
	},
}

// GetTextBlock gets a TextBlock from pool
func GetTextBlock() *TextBlock {
	tb := textBlockPool.Get().(*TextBlock)
	tb.Texts = tb.Texts[:0]
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
	tb.Texts = tb.Texts[:0]
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
)

func init() {
	for i := 0; i < 256; i++ {
		singleCharStrings[i] = string([]byte{byte(i)})
	}
}

// InternRune converts a rune to interned string
func InternRune(r rune) string {
	if r < 256 {
		return singleCharStrings[byte(r)]
	}
	s := string(r)
	if interned, ok := stringInternPool.Load(s); ok {
		return interned.(string)
	}
	stringInternPool.Store(s, s)
	return s
}
