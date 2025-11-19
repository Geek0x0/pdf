// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"math"
	"sort"
)

// SortStrategy represents different sorting algorithms available
type SortStrategy int

const (
	StrategyAuto      SortStrategy = iota // Automatically select best algorithm
	StrategyRadix                         // Radix sort for numeric keys
	StrategyQuick                         // Quicksort for general comparison
	StrategyInsertion                     // Insertion sort for small arrays
	StrategyStandard                      // Go standard library sort
)

// AdaptiveSorter selects the best sorting algorithm based on data characteristics
type AdaptiveSorter struct {
	radixThreshold     int // Minimum size to consider radix sort
	quicksortThreshold int // Minimum size to use quicksort over insertion
}

// NewAdaptiveSorter creates a new adaptive sorter with default thresholds
func NewAdaptiveSorter() *AdaptiveSorter {
	return &AdaptiveSorter{
		radixThreshold:     200,
		quicksortThreshold: 20,
	}
}

// SortTextsByCoordinate sorts texts by a numeric coordinate using the best algorithm
func (as *AdaptiveSorter) SortTextsByCoordinate(texts []Text, getCoord func(Text) float64) {
	n := len(texts)
	if n <= 1 {
		return
	}

	strategy := as.selectStrategy(n, true)

	switch strategy {
	case StrategyRadix:
		as.radixSortTexts(texts, getCoord)
	case StrategyInsertion:
		as.insertionSortTexts(texts, getCoord)
	default:
		// Use standard sort
		sort.Slice(texts, func(i, j int) bool {
			return getCoord(texts[i]) < getCoord(texts[j])
		})
	}
}

// SortTextsByComparison sorts texts using a comparison function
func (as *AdaptiveSorter) SortTextsByComparison(texts []Text, less func(i, j int) bool) {
	n := len(texts)
	if n <= 1 {
		return
	}

	if n < as.quicksortThreshold {
		as.insertionSortTextsFunc(texts, less)
	} else {
		sort.Slice(texts, less)
	}
}

// selectStrategy chooses the best sorting strategy
func (as *AdaptiveSorter) selectStrategy(n int, isNumeric bool) SortStrategy {
	if n < as.quicksortThreshold {
		return StrategyInsertion
	}

	if isNumeric && n >= as.radixThreshold {
		return StrategyRadix
	}

	return StrategyStandard
}

// radixSortTexts sorts texts using radix sort on a numeric key
func (as *AdaptiveSorter) radixSortTexts(texts []Text, getCoord func(Text) float64) {
	n := len(texts)
	if n <= 1 {
		return
	}

	// Extract coordinates and convert to sortable uints
	keys := make([]uint64, n)
	for i, t := range texts {
		coord := getCoord(t)
		bits := math.Float64bits(coord)
		// Handle negative numbers
		mask := -uint64(int64(bits)>>63) | 0x8000000000000000
		keys[i] = bits ^ mask
	}

	// Create index array
	indices := make([]int, n)
	for i := range indices {
		indices[i] = i
	}

	// Radix sort the indices based on keys
	as.radixSortIndices(indices, keys)

	// Reorder texts
	reordered := make([]Text, n)
	for i, idx := range indices {
		reordered[i] = texts[idx]
	}

	copy(texts, reordered)
}

// radixSortIndices performs radix sort on indices array based on keys
func (as *AdaptiveSorter) radixSortIndices(indices []int, keys []uint64) {
	n := len(indices)
	if n <= 1 {
		return
	}

	temp := make([]int, n)
	tempKeys := make([]uint64, n)

	// Use 8-bit radix (256 buckets) for better cache performance
	const radix = 256
	counts := make([]int, radix)

	// Process 8 bits at a time (8 passes for 64-bit keys)
	for shift := uint(0); shift < 64; shift += 8 {
		// Clear counts
		for i := range counts {
			counts[i] = 0
		}

		// Count occurrences
		for _, key := range keys {
			bucket := (key >> shift) & 0xFF
			counts[bucket]++
		}

		// Calculate positions (prefix sum)
		pos := 0
		for i := range counts {
			oldCount := counts[i]
			counts[i] = pos
			pos += oldCount
		}

		// Distribute elements
		for i, key := range keys {
			bucket := (key >> shift) & 0xFF
			idx := counts[bucket]
			temp[idx] = indices[i]
			tempKeys[idx] = key
			counts[bucket]++
		}

		// Swap buffers
		indices, temp = temp, indices
		keys, tempKeys = tempKeys, keys
	}
}

// insertionSortTexts sorts small text arrays using insertion sort
func (as *AdaptiveSorter) insertionSortTexts(texts []Text, getCoord func(Text) float64) {
	for i := 1; i < len(texts); i++ {
		key := getCoord(texts[i])
		t := texts[i]
		j := i - 1

		for j >= 0 && getCoord(texts[j]) > key {
			texts[j+1] = texts[j]
			j--
		}
		texts[j+1] = t
	}
}

// insertionSortTextsFunc sorts using a comparison function
func (as *AdaptiveSorter) insertionSortTextsFunc(texts []Text, less func(i, j int) bool) {
	for i := 1; i < len(texts); i++ {
		t := texts[i]
		j := i - 1

		for j >= 0 && less(i, j) {
			texts[j+1] = texts[j]
			j--
		}
		texts[j+1] = t
	}
}

// Global adaptive sorter instance
var globalAdaptiveSorter = NewAdaptiveSorter()

// FastSortTextsByX sorts texts by X coordinate using the fastest algorithm
func FastSortTextsByX(texts []Text) {
	globalAdaptiveSorter.SortTextsByCoordinate(texts, func(t Text) float64 { return t.X })
}

// FastSortTextsByY sorts texts by Y coordinate using the fastest algorithm
func FastSortTextsByY(texts []Text) {
	globalAdaptiveSorter.SortTextsByCoordinate(texts, func(t Text) float64 { return t.Y })
}

// FastSortTexts sorts texts using the fastest algorithm for the comparison function
func FastSortTexts(texts []Text, less func(i, j int) bool) {
	globalAdaptiveSorter.SortTextsByComparison(texts, less)
}

// SortingMetrics tracks performance of different sorting strategies
type SortingMetrics struct {
	RadixSortCount     int
	QuickSortCount     int
	InsertionSortCount int
	StandardSortCount  int
}

var sortingMetrics SortingMetrics

// GetSortingMetrics returns current sorting metrics
func GetSortingMetrics() SortingMetrics {
	return sortingMetrics
}

// ResetSortingMetrics resets the sorting metrics
func ResetSortingMetrics() {
	sortingMetrics = SortingMetrics{}
}

// BenchmarkSortingAlgorithms compares performance of different algorithms
func BenchmarkSortingAlgorithms(texts []Text, getCoord func(Text) float64) map[string]float64 {
	results := make(map[string]float64)

	// This would require actual timing, just return placeholder
	// In real usage, implement with time measurement

	results["radix"] = 0.0
	results["standard"] = 0.0
	results["quicksort"] = 0.0

	return results
}
