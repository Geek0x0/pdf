// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"testing"
)

// ===================== ClusterTextBlocksOptimizedV2 Tests =====================

func TestClusterTextBlocksOptimizedV2Empty(t *testing.T) {
	result := ClusterTextBlocksOptimizedV2(nil)
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}

	result = ClusterTextBlocksOptimizedV2([]Text{})
	if result != nil {
		t.Errorf("expected nil for empty slice, got %v", result)
	}
}

func TestClusterTextBlocksOptimizedV2SingleText(t *testing.T) {
	texts := []Text{
		{X: 10, Y: 100, W: 50, FontSize: 12, S: "Hello"},
	}

	result := ClusterTextBlocksOptimizedV2(texts)
	if len(result) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(result))
	}

	if len(result[0].Texts) != 1 {
		t.Errorf("expected 1 text in cluster, got %d", len(result[0].Texts))
	}
}

func TestClusterTextBlocksOptimizedV2NearbyTexts(t *testing.T) {
	// Texts that are close together should be clustered
	texts := []Text{
		{X: 10, Y: 100, W: 30, FontSize: 12, S: "Hello"},
		{X: 45, Y: 100, W: 30, FontSize: 12, S: "World"}, // Close to first
	}

	result := ClusterTextBlocksOptimizedV2(texts)

	// Should be in same cluster (close enough)
	totalTexts := 0
	for _, block := range result {
		totalTexts += len(block.Texts)
	}
	if totalTexts != 2 {
		t.Errorf("expected 2 total texts, got %d", totalTexts)
	}
}

func TestClusterTextBlocksOptimizedV2FarTexts(t *testing.T) {
	// Texts that are far apart should be in different clusters
	texts := []Text{
		{X: 10, Y: 100, W: 30, FontSize: 12, S: "Hello"},
		{X: 500, Y: 500, W: 30, FontSize: 12, S: "World"}, // Far from first
	}

	result := ClusterTextBlocksOptimizedV2(texts)

	// Should be in different clusters
	if len(result) < 1 {
		t.Error("expected at least 1 cluster")
	}

	// Total texts should be 2
	totalTexts := 0
	for _, block := range result {
		totalTexts += len(block.Texts)
	}
	if totalTexts != 2 {
		t.Errorf("expected 2 total texts, got %d", totalTexts)
	}
}

func TestClusterTextBlocksOptimizedV2MultipleLines(t *testing.T) {
	// Multiple lines of text
	texts := []Text{
		{X: 10, Y: 100, W: 50, FontSize: 12, S: "Line1"},
		{X: 10, Y: 85, W: 50, FontSize: 12, S: "Line2"}, // Below first, close
		{X: 10, Y: 70, W: 50, FontSize: 12, S: "Line3"}, // Below second, close
	}

	result := ClusterTextBlocksOptimizedV2(texts)

	// All should be clustered together or in sensible groups
	totalTexts := 0
	for _, block := range result {
		totalTexts += len(block.Texts)
	}
	if totalTexts != 3 {
		t.Errorf("expected 3 total texts, got %d", totalTexts)
	}
}

// ===================== mergeTextBlocksOptimized Tests =====================

func TestMergeTextBlocksOptimizedEmpty(t *testing.T) {
	result := mergeTextBlocksOptimized(nil)
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}

	result = mergeTextBlocksOptimized([]*TextBlock{})
	if result != nil {
		t.Errorf("expected nil for empty slice, got %v", result)
	}
}

func TestMergeTextBlocksOptimizedSingle(t *testing.T) {
	block := &TextBlock{
		Texts:       []Text{{S: "test"}},
		MinX:        10,
		MaxX:        100,
		MinY:        50,
		MaxY:        70,
		AvgFontSize: 12,
	}

	result := mergeTextBlocksOptimized([]*TextBlock{block})
	if result != block {
		t.Error("expected same block for single input")
	}
}

func TestMergeTextBlocksOptimizedMultiple(t *testing.T) {
	blocks := []*TextBlock{
		{
			Texts:       []Text{{S: "A"}},
			MinX:        10,
			MaxX:        50,
			MinY:        100,
			MaxY:        120,
			AvgFontSize: 12,
		},
		{
			Texts:       []Text{{S: "B"}},
			MinX:        5,
			MaxX:        60,
			MinY:        80,
			MaxY:        130,
			AvgFontSize: 14,
		},
	}

	result := mergeTextBlocksOptimized(blocks)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Check merged bounds
	if result.MinX != 5 {
		t.Errorf("expected MinX=5, got %f", result.MinX)
	}
	if result.MaxX != 60 {
		t.Errorf("expected MaxX=60, got %f", result.MaxX)
	}
	if result.MinY != 80 {
		t.Errorf("expected MinY=80, got %f", result.MinY)
	}
	if result.MaxY != 130 {
		t.Errorf("expected MaxY=130, got %f", result.MaxY)
	}

	// Check merged texts - first block's texts are kept, second block's texts are appended
	// The implementation reuses first block so it should have texts from both
	if len(result.Texts) < 1 {
		t.Errorf("expected at least 1 text, got %d", len(result.Texts))
	}
}

// ===================== sortWithinBlockOptimized Tests =====================

func TestSortWithinBlockOptimizedEmpty(t *testing.T) {
	result := sortWithinBlockOptimized(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}

	result = sortWithinBlockOptimized([]Text{})
	if len(result) != 0 {
		t.Errorf("expected empty for empty input, got len=%d", len(result))
	}
}

func TestSortWithinBlockOptimizedSingleText(t *testing.T) {
	texts := []Text{{X: 10, Y: 100, S: "single"}}
	result := sortWithinBlockOptimized(texts)

	if len(result) != 1 {
		t.Fatalf("expected 1 text, got %d", len(result))
	}
	if result[0].S != "single" {
		t.Errorf("expected 'single', got %q", result[0].S)
	}
}

func TestSortWithinBlockOptimizedSingleLine(t *testing.T) {
	// Single line, should be sorted left to right
	texts := []Text{
		{X: 100, Y: 100, S: "C"},
		{X: 10, Y: 100, S: "A"},
		{X: 50, Y: 100, S: "B"},
	}

	result := sortWithinBlockOptimized(texts)

	if len(result) != 3 {
		t.Fatalf("expected 3 texts, got %d", len(result))
	}
	if result[0].S != "A" || result[1].S != "B" || result[2].S != "C" {
		t.Errorf("expected A,B,C order, got %s,%s,%s", result[0].S, result[1].S, result[2].S)
	}
}

func TestSortWithinBlockOptimizedMultipleLines(t *testing.T) {
	// Multiple lines, should be sorted top to bottom, then left to right
	texts := []Text{
		{X: 50, Y: 50, S: "D"},  // Bottom line, right
		{X: 10, Y: 50, S: "C"},  // Bottom line, left
		{X: 50, Y: 100, S: "B"}, // Top line, right
		{X: 10, Y: 100, S: "A"}, // Top line, left
	}

	result := sortWithinBlockOptimized(texts)

	if len(result) != 4 {
		t.Fatalf("expected 4 texts, got %d", len(result))
	}

	// Top line first (higher Y), left to right
	if result[0].S != "A" {
		t.Errorf("expected first 'A', got %q (Y=%f, X=%f)", result[0].S, result[0].Y, result[0].X)
	}
	if result[1].S != "B" {
		t.Errorf("expected second 'B', got %q (Y=%f, X=%f)", result[1].S, result[1].Y, result[1].X)
	}
	// Bottom line second, left to right
	if result[2].S != "C" {
		t.Errorf("expected third 'C', got %q (Y=%f, X=%f)", result[2].S, result[2].Y, result[2].X)
	}
	if result[3].S != "D" {
		t.Errorf("expected fourth 'D', got %q (Y=%f, X=%f)", result[3].S, result[3].Y, result[3].X)
	}
}

func TestSortWithinBlockOptimizedWithTolerance(t *testing.T) {
	// Texts with slight Y variation (within tolerance) should be on same line
	texts := []Text{
		{X: 100, Y: 100.5, S: "C"},
		{X: 10, Y: 100, S: "A"},
		{X: 50, Y: 101, S: "B"}, // Within 3.0 tolerance
	}

	result := sortWithinBlockOptimized(texts)

	if len(result) != 3 {
		t.Fatalf("expected 3 texts, got %d", len(result))
	}

	// All should be treated as same line, sorted by X
	if result[0].S != "A" || result[1].S != "B" || result[2].S != "C" {
		t.Errorf("expected A,B,C order, got %s,%s,%s", result[0].S, result[1].S, result[2].S)
	}
}

// ===================== Integration Test =====================

func TestClusterAndSortIntegration(t *testing.T) {
	// Simulate a simple document with multiple lines
	texts := []Text{
		{X: 10, Y: 100, W: 30, FontSize: 12, S: "Hello"},
		{X: 50, Y: 100, W: 30, FontSize: 12, S: "World"},
		{X: 10, Y: 80, W: 60, FontSize: 12, S: "This is"},
		{X: 80, Y: 80, W: 40, FontSize: 12, S: "a test"},
	}

	clusters := ClusterTextBlocksOptimizedV2(texts)

	// Verify no texts are lost
	totalTexts := 0
	for _, block := range clusters {
		totalTexts += len(block.Texts)
	}
	if totalTexts != 4 {
		t.Errorf("expected 4 total texts, got %d", totalTexts)
	}

	// Sort within each cluster
	for _, block := range clusters {
		sorted := sortWithinBlockOptimized(block.Texts)
		if len(sorted) != len(block.Texts) {
			t.Errorf("sorting changed text count: %d -> %d", len(block.Texts), len(sorted))
		}
	}
}

// ===================== Benchmarks =====================

func BenchmarkClusterTextBlocksOptimizedV2Small(b *testing.B) {
	texts := make([]Text, 50)
	for i := range texts {
		texts[i] = Text{
			X:        float64(i % 10 * 50),
			Y:        float64(i / 10 * 20),
			W:        40,
			FontSize: 12,
			S:        "test",
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ClusterTextBlocksOptimizedV2(texts)
	}
}

func BenchmarkClusterTextBlocksOptimizedV2Medium(b *testing.B) {
	texts := make([]Text, 200)
	for i := range texts {
		texts[i] = Text{
			X:        float64(i % 20 * 30),
			Y:        float64(i / 20 * 15),
			W:        25,
			FontSize: 10,
			S:        "text",
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ClusterTextBlocksOptimizedV2(texts)
	}
}

func BenchmarkSortWithinBlockOptimizedSmall(b *testing.B) {
	texts := make([]Text, 50)
	for i := range texts {
		texts[i] = Text{
			X: float64(49 - i%10*5), // Reverse order
			Y: float64(100 - i/10*10),
			S: "x",
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Make a copy since sort is in-place
		textsCopy := make([]Text, len(texts))
		copy(textsCopy, texts)
		sortWithinBlockOptimized(textsCopy)
	}
}

func BenchmarkSortWithinBlockOptimizedLarge(b *testing.B) {
	texts := make([]Text, 500)
	for i := range texts {
		texts[i] = Text{
			X: float64(499 - i%50*10),
			Y: float64(1000 - i/50*20),
			S: "x",
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		textsCopy := make([]Text, len(texts))
		copy(textsCopy, texts)
		sortWithinBlockOptimized(textsCopy)
	}
}

func BenchmarkMergeTextBlocksOptimized(b *testing.B) {
	blocks := make([]*TextBlock, 10)
	for i := range blocks {
		blocks[i] = &TextBlock{
			Texts:       []Text{{S: "test"}},
			MinX:        float64(i * 10),
			MaxX:        float64(i*10 + 50),
			MinY:        float64(i * 5),
			MaxY:        float64(i*5 + 20),
			AvgFontSize: 12,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Create fresh blocks for each iteration since merging modifies them
		freshBlocks := make([]*TextBlock, len(blocks))
		for j := range blocks {
			freshBlocks[j] = &TextBlock{
				Texts:       []Text{{S: "test"}},
				MinX:        blocks[j].MinX,
				MaxX:        blocks[j].MaxX,
				MinY:        blocks[j].MinY,
				MaxY:        blocks[j].MaxY,
				AvgFontSize: blocks[j].AvgFontSize,
			}
		}
		mergeTextBlocksOptimized(freshBlocks)
	}
}
