// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"math/rand"
	"testing"
)

// BenchmarkMemoryOptimizations memory optimization benchmark test
// Used to compare memory allocation before and after optimization

func BenchmarkSortWithinBlock(b *testing.B) {
	texts := generateRandomTexts(1000, 100) // 1000 texts, distributed in 100 lines

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Copy input to avoid affecting next iteration
		input := make([]Text, len(texts))
		copy(input, texts)
		_ = sortWithinBlock(input)
	}
}

func BenchmarkMergeTextBlocks(b *testing.B) {
	// Create 100 text blocks, each containing 10 texts
	blocks := make([]*TextBlock, 100)
	for i := range blocks {
		texts := generateRandomTexts(10, 5)
		blocks[i] = &TextBlock{
			Texts:       texts,
			MinX:        0,
			MaxX:        100,
			MinY:        float64(i * 10),
			MaxY:        float64(i*10 + 10),
			AvgFontSize: 12,
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Copy blocks to avoid affecting next iteration
		input := make([]*TextBlock, len(blocks))
		copy(input, blocks)
		_ = mergeTextBlocks(input)
	}
}

func BenchmarkSmartTextRunsToPlain(b *testing.B) {
	texts := generateRandomTexts(5000, 200) // 5000 texts, distributed in 200 lines

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = SmartTextRunsToPlain(texts)
	}
}

func BenchmarkKDTreeRangeSearchMemOpt(b *testing.B) {
	// Create KD tree containing 1000 text blocks
	texts := generateRandomTexts(1000, 100)
	blocks := make([]*TextBlock, len(texts))
	for i, t := range texts {
		blocks[i] = &TextBlock{
			Texts:       []Text{t},
			MinX:        t.X,
			MaxX:        t.X + t.W,
			MinY:        t.Y,
			MaxY:        t.Y + t.FontSize,
			AvgFontSize: t.FontSize,
		}
	}

	tree := BuildKDTree(blocks)
	target := []float64{50, 50}
	radius := 20.0

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = tree.RangeSearch(target, radius)
	}
}

// Generate random texts for testing
func generateRandomTexts(count, lineCount int) []Text {
	texts := make([]Text, count)
	textsPerLine := count / lineCount

	for i := 0; i < count; i++ {
		lineIdx := i / textsPerLine
		if lineIdx >= lineCount {
			lineIdx = lineCount - 1
		}

		texts[i] = Text{
			Font:     "Arial",
			FontSize: 12 + rand.Float64()*4, // 12-16
			X:        rand.Float64() * 500,
			Y:        float64(lineIdx*15) + rand.Float64()*2, // small offset within line
			W:        20 + rand.Float64()*50,
			S:        randomString(5 + rand.Intn(10)),
		}
	}

	return texts
}

func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// BenchmarkClusterTextBlocksMemOpt test clustering performance
func BenchmarkClusterTextBlocksMemOpt(b *testing.B) {
	texts := generateRandomTexts(500, 50)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = clusterTextBlocks(texts)
	}
}

// BenchmarkClusterTextBlocksSmall small dataset clustering
func BenchmarkClusterTextBlocksSmall(b *testing.B) {
	texts := generateRandomTexts(100, 10)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = clusterTextBlocks(texts)
	}
}

// BenchmarkClusterTextBlocksLarge large dataset clustering
func BenchmarkClusterTextBlocksLarge(b *testing.B) {
	texts := generateRandomTexts(2000, 200)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = clusterTextBlocks(texts)
	}
}
