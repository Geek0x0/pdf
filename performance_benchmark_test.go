package pdf

import (
	"testing"
)

// Benchmark for appendText optimization
func BenchmarkAppendText(b *testing.B) {
	// Create a content extractor
	ce := &contentExtractor{
		text: make([]Text, 0, 1000),
	}

	// Create a minimal gstate for testing
	// Using empty Font struct which will work with nopEncoder
	g := &gstate{
		Tf:  Font{},
		Tfs: 12.0,
		Th:  1.0,
		Tm:  matrix{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}},
		CTM: matrix{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}},
	}

	testString := "这是一段测试文本，用于测试appendText函数的性能优化效果。The quick brown fox jumps over the lazy dog."

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ce.text = ce.text[:0] // Reset for each iteration
		ce.appendText(g, nil, testString)
	}
}

// Benchmark for FontPool
func BenchmarkFontPool(b *testing.B) {
	pool := NewFontPool()
	fonts := []string{
		"Helvetica",
		"Times-Roman",
		"Courier",
		"Arial",
		"Times-Bold",
		"Helvetica-Bold",
	}

	b.Run("GetID", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			pool.GetID(fonts[i%len(fonts)])
		}
	})

	b.Run("GetFont", func(b *testing.B) {
		// Pre-populate
		ids := make([]uint32, len(fonts))
		for i, font := range fonts {
			ids[i] = pool.GetID(font)
		}

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			pool.GetFont(ids[i%len(ids)])
		}
	})
}

// Benchmark for Text to TextOptimized conversion
func BenchmarkTextConversion(b *testing.B) {
	pool := NewFontPool()

	text := Text{
		Font:      "Helvetica",
		FontSize:  12.0,
		X:         100.0,
		Y:         200.0,
		W:         50.0,
		S:         "Test",
		Vertical:  false,
		Bold:      true,
		Italic:    false,
		Underline: false,
	}

	b.Run("ToOptimized", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = ConvertTextToOptimized(text, pool)
		}
	})

	optimized := ConvertTextToOptimized(text, pool)

	b.Run("FromOptimized", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = ConvertOptimizedToText(optimized, pool)
		}
	})
}

// Benchmark for KD-tree RangeSearch with buffer
func BenchmarkKDTreeRangeSearchOptimized(b *testing.B) {
	// Generate test blocks
	blocks := generateTestTextBlocks(1000)
	tree := BuildKDTree(blocks)

	b.Run("Original", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			result := tree.RangeSearch(100.0, 100.0, 25.0)
			putResultSlice(result) // Return to pool
		}
	})

	b.Run("WithBuffer", func(b *testing.B) {
		buffer := make([]*TextBlock, 0, 64)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			buffer = tree.RangeSearchWithBuffer(100.0, 100.0, 25.0, buffer)
		}
	})
}

// Benchmark for text clustering algorithms
func BenchmarkTextClustering(b *testing.B) {
	sizes := []int{100, 500, 1000, 5000}

	for _, size := range sizes {
		texts := generateBenchmarkTestTexts(size)

		b.Run("V2_"+string(rune(size)), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				blocks := ClusterTextBlocksOptimizedV2(texts)
				// Clean up
				for _, block := range blocks {
					PutTextBlock(block)
				}
			}
		})

		b.Run("V3_"+string(rune(size)), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				blocks := ClusterTextBlocksV3(texts)
				// Clean up
				for _, block := range blocks {
					PutTextBlock(block)
				}
			}
		})
	}
}

// Benchmark for SpatialGrid
func BenchmarkSpatialGrid(b *testing.B) {
	blocks := generateTestTextBlocks(5000)
	cellSize := 20.0

	b.Run("Creation", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = NewSpatialGrid(blocks, cellSize)
		}
	})

	grid := NewSpatialGrid(blocks, cellSize)

	b.Run("GetNearbyBlocks", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = grid.GetNearbyBlocks(i % len(blocks))
		}
	})
}

// Helper functions to generate test data
func generateBenchmarkTestTexts(count int) []Text {
	texts := make([]Text, count)
	for i := 0; i < count; i++ {
		texts[i] = Text{
			Font:     "Helvetica",
			FontSize: 12.0,
			X:        float64(i%100) * 10.0,
			Y:        float64(i/100) * 15.0,
			W:        50.0,
			S:        "Test",
		}
	}
	return texts
}

func generateTestTextBlocks(count int) []*TextBlock {
	blocks := make([]*TextBlock, count)
	for i := 0; i < count; i++ {
		tb := &TextBlock{
			MinX:        float64(i%100) * 10.0,
			MaxX:        float64(i%100)*10.0 + 50.0,
			MinY:        float64(i/100) * 15.0,
			MaxY:        float64(i/100)*15.0 + 12.0,
			AvgFontSize: 12.0,
		}
		blocks[i] = tb
	}
	return blocks
}

// Memory usage benchmark
func BenchmarkMemoryUsage(b *testing.B) {
	b.Run("Text", func(b *testing.B) {
		b.ReportAllocs()
		texts := make([]Text, b.N)
		for i := 0; i < b.N; i++ {
			texts[i] = Text{
				Font:     "Helvetica",
				FontSize: 12.0,
				X:        100.0,
				Y:        200.0,
				W:        50.0,
				S:        "Test",
			}
		}
	})

	b.Run("TextOptimized", func(b *testing.B) {
		pool := NewFontPool()
		fontID := pool.GetID("Helvetica")

		b.ReportAllocs()
		texts := make([]TextOptimized, b.N)
		for i := 0; i < b.N; i++ {
			texts[i] = TextOptimized{
				FontID:   fontID,
				FontSize: 12.0,
				X:        100.0,
				Y:        200.0,
				W:        50.0,
				S:        "Test",
			}
		}
	})
}

// Benchmark for batch text extraction (end-to-end)
func BenchmarkBatchExtraction(b *testing.B) {
	// This would require a real PDF file
	// Skipping implementation for now, but structure is here
	b.Skip("Requires real PDF file")
}
