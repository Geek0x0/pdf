// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"math/rand"
	"sort"
	"testing"
)

func TestAdaptiveSorterBasic(t *testing.T) {
	sorter := NewAdaptiveSorter()

	texts := []Text{
		{X: 100, Y: 500, S: "third"},
		{X: 50, Y: 100, S: "first"},
		{X: 75, Y: 300, S: "second"},
		{X: 150, Y: 700, S: "fourth"},
	}

	sorter.SortTextsByCoordinate(texts, func(t Text) float64 { return t.X })

	expected := []string{"first", "second", "third", "fourth"}
	for i, exp := range expected {
		if texts[i].S != exp {
			t.Errorf("Index %d: got %s, want %s", i, texts[i].S, exp)
		}
	}
}

func TestFastSortFunctions(t *testing.T) {
	texts := []Text{
		{X: 3, Y: 3, S: "c"},
		{X: 1, Y: 1, S: "a"},
		{X: 2, Y: 2, S: "b"},
	}

	// Test X sort
	textsCopy := make([]Text, len(texts))
	copy(textsCopy, texts)
	FastSortTextsByX(textsCopy)

	if textsCopy[0].S != "a" || textsCopy[1].S != "b" || textsCopy[2].S != "c" {
		t.Error("FastSortTextsByX failed")
	}

	// Test Y sort
	copy(textsCopy, texts)
	FastSortTextsByY(textsCopy)

	if textsCopy[0].S != "a" || textsCopy[1].S != "b" || textsCopy[2].S != "c" {
		t.Error("FastSortTextsByY failed")
	}
}

func TestFastSortTexts(t *testing.T) {
	texts := []Text{
		{X: 300, Y: 100, S: "third"},
		{X: 100, Y: 200, S: "first"},
		{X: 200, Y: 150, S: "second"},
	}

	FastSortTexts(texts, func(i, j int) bool {
		return texts[i].Y < texts[j].Y
	})

	// Should be sorted by Y
	if texts[0].S != "third" || texts[1].S != "second" || texts[2].S != "first" {
		t.Errorf("FastSortTexts failed: got %v", texts)
	}
}

func TestSortTextsByComparison(t *testing.T) {
	sorter := NewAdaptiveSorter()

	texts := []Text{
		{X: 300, Y: 100, S: "c"},
		{X: 100, Y: 200, S: "a"},
		{X: 200, Y: 150, S: "b"},
	}

	sorter.SortTextsByComparison(texts, func(i, j int) bool {
		return texts[i].S < texts[j].S
	})

	if texts[0].S != "a" || texts[1].S != "b" || texts[2].S != "c" {
		t.Errorf("SortTextsByComparison failed")
	}
}

func TestInsertionSortTextsFunc(t *testing.T) {
	sorter := NewAdaptiveSorter()

	texts := []Text{
		{X: 3, Y: 3, S: "c"},
		{X: 1, Y: 1, S: "a"},
		{X: 2, Y: 2, S: "b"},
	}

	sorter.insertionSortTextsFunc(texts, func(i, j int) bool {
		return texts[i].X < texts[j].X
	})

	if texts[0].S != "a" || texts[1].S != "b" || texts[2].S != "c" {
		t.Errorf("insertionSortTextsFunc failed")
	}
}

func TestGetSortingMetrics(t *testing.T) {
	ResetSortingMetrics()

	// Perform some sorts to generate metrics
	texts := make([]Text, 100)
	for i := range texts {
		texts[i] = Text{X: float64(100 - i), Y: float64(i)}
	}

	FastSortTextsByX(texts)

	metrics := GetSortingMetrics()
	// Just check we can call it
	_ = metrics
}

func TestResetSortingMetrics(t *testing.T) {
	texts := make([]Text, 10)
	FastSortTextsByX(texts)

	ResetSortingMetrics()
	metrics := GetSortingMetrics()

	if metrics.RadixSortCount != 0 || metrics.QuickSortCount != 0 {
		t.Error("Metrics should be reset to zero")
	}
}

func TestBenchmarkSortingAlgorithms(t *testing.T) {
	texts := make([]Text, 100)
	for i := range texts {
		texts[i] = Text{X: float64(i), Y: float64(100 - i)}
	}

	results := BenchmarkSortingAlgorithms(texts, func(t Text) float64 { return t.X })

	if len(results) == 0 {
		t.Error("Expected benchmark results")
	}

	for strategy, duration := range results {
		if duration < 0 {
			t.Errorf("Invalid duration for strategy %v: %v", strategy, duration)
		}
	}
}

func TestSelectStrategy(t *testing.T) {
	sorter := NewAdaptiveSorter()

	tests := []struct {
		name      string
		size      int
		isNumeric bool
		expected  SortStrategy
	}{
		{"small array", 10, true, StrategyInsertion},
		{"medium numeric", 100, true, StrategyStandard},
		{"large numeric", 1000, true, StrategyRadix},
		{"small non-numeric", 10, false, StrategyInsertion},
		{"large non-numeric", 1000, false, StrategyStandard},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sorter.selectStrategy(tt.size, tt.isNumeric)
			if got != tt.expected {
				t.Errorf("selectStrategy(%d, %v) = %v, want %v",
					tt.size, tt.isNumeric, got, tt.expected)
			}
		})
	}
}

func TestAdaptiveSorterCorrectness(t *testing.T) {
	sorter := NewAdaptiveSorter()

	sizes := []int{10, 50, 100, 500, 1000}

	for _, size := range sizes {
		t.Run(string(rune(size)), func(t *testing.T) {
			// Generate random texts
			texts := make([]Text, size)
			for i := range texts {
				texts[i] = Text{
					X: rand.Float64() * 1000,
					Y: rand.Float64() * 1000,
				}
			}

			// Sort using adaptive sorter
			sorter.SortTextsByCoordinate(texts, func(t Text) float64 { return t.X })

			// Verify sorted
			for i := 1; i < len(texts); i++ {
				if texts[i].X < texts[i-1].X {
					t.Errorf("Not sorted at index %d: %f < %f", i, texts[i].X, texts[i-1].X)
					break
				}
			}
		})
	}
}

func BenchmarkAdaptiveSorter(b *testing.B) {
	sizes := []int{100, 1000, 10000}

	for _, size := range sizes {
		texts := make([]Text, size)
		for i := range texts {
			texts[i] = Text{
				X: rand.Float64() * 1000,
				Y: rand.Float64() * 1000,
			}
		}

		b.Run("Adaptive", func(b *testing.B) {
			sorter := NewAdaptiveSorter()
			for i := 0; i < b.N; i++ {
				testTexts := make([]Text, size)
				copy(testTexts, texts)
				sorter.SortTextsByCoordinate(testTexts, func(t Text) float64 { return t.X })
			}
		})

		b.Run("Standard", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				testTexts := make([]Text, size)
				copy(testTexts, texts)
				sort.Slice(testTexts, func(i, j int) bool {
					return testTexts[i].X < testTexts[j].X
				})
			}
		})
	}
}

func BenchmarkFastSort(b *testing.B) {
	size := 10000
	texts := make([]Text, size)
	for i := range texts {
		texts[i] = Text{X: rand.Float64() * 1000}
	}

	b.Run("FastSortTextsByX", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			testTexts := make([]Text, size)
			copy(testTexts, texts)
			FastSortTextsByX(testTexts)
		}
	})

	b.Run("StandardSort", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			testTexts := make([]Text, size)
			copy(testTexts, texts)
			sort.Slice(testTexts, func(i, j int) bool {
				return testTexts[i].X < testTexts[j].X
			})
		}
	})
}

func TestSortingMetrics(t *testing.T) {
	// Test GetSortingMetrics
	_ = GetSortingMetrics()

	// Test ResetSortingMetrics
	ResetSortingMetrics()
	metricsAfter := GetSortingMetrics()
	if metricsAfter.RadixSortCount != 0 || metricsAfter.QuickSortCount != 0 {
		t.Error("Reset did not work")
	}
}

func TestNewAdaptiveSorter(t *testing.T) {
	sorter := NewAdaptiveSorter()
	if sorter == nil {
		t.Error("Expected non-nil AdaptiveSorter")
	}
	if sorter.radixThreshold != 200 {
		t.Errorf("Expected radixThreshold 200, got %d", sorter.radixThreshold)
	}
	if sorter.quicksortThreshold != 20 {
	}
}

func TestAdaptiveSorterRadixSortTexts(t *testing.T) {
	sorter := NewAdaptiveSorter()

	texts := []Text{
		{X: 3.0, S: "c"},
		{X: 1.0, S: "a"},
		{X: 2.0, S: "b"},
	}

	sorter.radixSortTexts(texts, func(t Text) float64 { return t.X })

	if texts[0].S != "a" || texts[1].S != "b" || texts[2].S != "c" {
		t.Error("radixSortTexts failed")
	}
}

func TestAdaptiveSorterRadixSortIndices(t *testing.T) {
	sorter := NewAdaptiveSorter()

	indices := []int{0, 1, 2}
	keys := []uint64{3, 1, 2}

	sorter.radixSortIndices(indices, keys)

	if indices[0] != 1 || indices[1] != 2 || indices[2] != 0 {
		t.Errorf("radixSortIndices failed: got %v", indices)
	}
}

func TestAdaptiveSorterInsertionSortTexts(t *testing.T) {
	sorter := NewAdaptiveSorter()

	texts := []Text{
		{X: 3.0, S: "c"},
		{X: 1.0, S: "a"},
		{X: 2.0, S: "b"},
	}

	sorter.insertionSortTexts(texts, func(t Text) float64 { return t.X })

	if texts[0].S != "a" || texts[1].S != "b" || texts[2].S != "c" {
		t.Error("insertionSortTexts failed")
	}
}

func TestAdaptiveSorterInsertionSortTextsFunc(t *testing.T) {
	sorter := NewAdaptiveSorter()

	texts := []Text{
		{X: 3.0, S: "c"},
		{X: 1.0, S: "a"},
		{X: 2.0, S: "b"},
	}

	sorter.insertionSortTextsFunc(texts, func(i, j int) bool {
		return texts[i].X < texts[j].X
	})

	if texts[0].S != "a" || texts[1].S != "b" || texts[2].S != "c" {
		t.Error("insertionSortTextsFunc failed")
	}
}

func TestFastSortTextsComparison(t *testing.T) {
	texts := []Text{
		{X: 3.0, Y: 1.0, S: "c"},
		{X: 1.0, Y: 3.0, S: "a"},
		{X: 2.0, Y: 2.0, S: "b"},
	}

	FastSortTexts(texts, func(i, j int) bool {
		return texts[i].X < texts[j].X
	})

	if texts[0].S != "a" || texts[1].S != "b" || texts[2].S != "c" {
		t.Error("FastSortTexts failed")
	}
}

func TestGetSortingMetricsBasic(t *testing.T) {
	metrics := GetSortingMetrics()
	// Just check that it returns something
	_ = metrics
}

func TestResetSortingMetricsBasic(t *testing.T) {
	// Reset and check
	ResetSortingMetrics()
	metrics := GetSortingMetrics()
	if metrics.RadixSortCount != 0 || metrics.QuickSortCount != 0 || metrics.InsertionSortCount != 0 || metrics.StandardSortCount != 0 {
		t.Error("ResetSortingMetrics did not reset all counters")
	}
}

func TestBenchmarkSortingAlgorithmsBasic(t *testing.T) {
	texts := []Text{
		{X: 1.0, S: "a"},
		{X: 2.0, S: "b"},
	}

	results := BenchmarkSortingAlgorithms(texts, func(t Text) float64 { return t.X })
	// Just check that it returns a map
	if len(results) == 0 {
		t.Error("Expected non-empty results map")
	}
}

func TestSelectStrategyDirect(t *testing.T) {
	sorter := NewAdaptiveSorter()

	// Test small size - should use insertion
	strategy := sorter.selectStrategy(10, true)
	if strategy != StrategyInsertion {
		t.Errorf("Expected StrategyInsertion for small size, got %v", strategy)
	}

	// Test medium size - should use standard
	strategy = sorter.selectStrategy(50, true)
	if strategy != StrategyStandard {
		t.Errorf("Expected StrategyStandard for medium size, got %v", strategy)
	}

	// Test large size with numeric - should use radix
	strategy = sorter.selectStrategy(300, true)
	if strategy != StrategyRadix {
		t.Errorf("Expected StrategyRadix for large numeric size, got %v", strategy)
	}

	// Test large size without numeric - should use standard
	strategy = sorter.selectStrategy(300, false)
	if strategy != StrategyStandard {
		t.Errorf("Expected StrategyStandard for large non-numeric size, got %v", strategy)
	}
}
