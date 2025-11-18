// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"testing"
)

// BenchmarkSimpleTextOrdering benchmarks the original simple text ordering
func BenchmarkSimpleTextOrdering(b *testing.B) {
	texts := generateTestTexts(100, false) // single column
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = textRunsToPlain(texts)
	}
}

// BenchmarkSmartTextOrdering benchmarks the smart text ordering algorithm
func BenchmarkSmartTextOrdering(b *testing.B) {
	texts := generateTestTexts(100, false) // single column
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SmartTextRunsToPlain(texts)
	}
}

// BenchmarkSimpleTextOrderingMultiColumn benchmarks simple ordering with multi-column layout
func BenchmarkSimpleTextOrderingMultiColumn(b *testing.B) {
	texts := generateTestTexts(100, true) // multi-column
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = textRunsToPlain(texts)
	}
}

// BenchmarkSmartTextOrderingMultiColumn benchmarks smart ordering with multi-column layout
func BenchmarkSmartTextOrderingMultiColumn(b *testing.B) {
	texts := generateTestTexts(100, true) // multi-column
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SmartTextRunsToPlain(texts)
	}
}

// BenchmarkClusterTextBlocks benchmarks just the clustering algorithm
func BenchmarkClusterTextBlocks(b *testing.B) {
	texts := generateTestTexts(100, true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = clusterTextBlocks(texts)
	}
}

// BenchmarkDetectColumns benchmarks column detection
func BenchmarkDetectColumns(b *testing.B) {
	texts := generateTestTexts(100, true)
	blocks := clusterTextBlocks(texts)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = detectColumns(blocks)
	}
}

// BenchmarkSmartTextOrderingLarge benchmarks with large document (1000 text runs)
func BenchmarkSmartTextOrderingLarge(b *testing.B) {
	texts := generateTestTexts(1000, true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SmartTextRunsToPlain(texts)
	}
}

// BenchmarkSimpleTextOrderingLarge benchmarks simple ordering with large document
func BenchmarkSimpleTextOrderingLarge(b *testing.B) {
	texts := generateTestTexts(1000, true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = textRunsToPlain(texts)
	}
}

// generateTestTexts creates test text runs
// count: number of text runs to generate
// multiColumn: if true, generates a two-column layout
func generateTestTexts(count int, multiColumn bool) []Text {
	texts := make([]Text, count)

	if multiColumn {
		// Create two-column layout
		for i := 0; i < count; i++ {
			column := i % 2
			row := i / 2

			x := 100.0
			if column == 1 {
				x = 300.0
			}

			y := 700.0 - float64(row)*15.0

			texts[i] = Text{
				Font:     "Arial",
				FontSize: 12,
				X:        x,
				Y:        y,
				W:        50,
				S:        "Sample",
			}
		}
	} else {
		// Create single-column layout
		for i := 0; i < count; i++ {
			texts[i] = Text{
				Font:     "Arial",
				FontSize: 12,
				X:        100.0,
				Y:        700.0 - float64(i)*15.0,
				W:        50,
				S:        "Sample",
			}
		}
	}

	return texts
}
