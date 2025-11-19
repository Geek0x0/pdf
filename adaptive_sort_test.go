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
