// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"runtime"
	"sort"
	"sync"
)

// OptimizedSorter provides optimized sorting algorithms for large text collections
type OptimizedSorter struct {
	parallelThreshold int // Minimum size to use parallel sorting
}

// NewOptimizedSorter creates a new optimized sorter
func NewOptimizedSorter() *OptimizedSorter {
	return &OptimizedSorter{
		parallelThreshold: 10000, // Use parallel sort for collections with 10k+ items
	}
}

// SortTexts sorts a collection of texts using the most appropriate algorithm
func (os *OptimizedSorter) SortTexts(texts []Text, less func(i, j int) bool) {
	if len(texts) <= 1 {
		return
	}

	if len(texts) < os.parallelThreshold {
		// Use optimized sort for smaller collections
		os.timSort(texts, less)
	} else {
		// Use parallel sort for larger collections
		os.parallelSort(texts, less)
	}
}

// standardSort uses an optimized version of standard sort
func (os *OptimizedSorter) standardSort(texts []Text, less func(i, j int) bool) {
	// Use Go's standard library sort, which is already highly optimized
	sort.Slice(texts, less)
}

// timSort implements an optimized Timsort-like algorithm
func (os *OptimizedSorter) timSort(texts []Text, less func(i, j int) bool) {
	const minRun = 32

	n := len(texts)
	if n <= minRun {
		os.insertionSort(texts, less)
		return
	}

	// Calculate min run length
	minRunLen := os.calcMinRun(n)

	runs := make([]run, 0, n/minRunLen+1)

	// Create initial runs
	for i := 0; i < n; {
		start := i
		end := min(i+minRunLen, n)

		// Find natural run
		for j := start + 1; j < end && !less(j, j-1); j++ {
			// Continue while ascending
		}

		// If descending, reverse
		if start+1 < end && less(end-1, start) {
			os.reverse(texts[start:end])
		}

		// Insertion sort the run
		os.insertionSort(texts[start:end], func(i, j int) bool {
			return less(start+i, start+j)
		})

		runs = append(runs, run{start: start, len: end - start})
		i = end
	}

	// Merge runs
	for len(runs) > 1 {
		i := 0
		for i < len(runs)-1 {
			if i+2 < len(runs) && runs[i+1].len < runs[i+2].len {
				// Merge runs[i] and runs[i+1]
				os.mergeRuns(texts, &runs[i], &runs[i+1], less)
				runs = append(runs[:i+1], runs[i+2:]...)
			} else {
				i++
			}
		}
	}
}

// run represents a sorted run in Timsort
type run struct {
	start, len int
}

// calcMinRun calculates the minimum run length for Timsort
func (os *OptimizedSorter) calcMinRun(n int) int {
	r := 0
	for n >= 64 {
		r |= n & 1
		n >>= 1
	}
	return n + r
}

// insertionSort performs insertion sort on a subarray
func (os *OptimizedSorter) insertionSort(texts []Text, less func(i, j int) bool) {
	for i := 1; i < len(texts); i++ {
		for j := i; j > 0 && less(j, j-1); j-- {
			texts[j], texts[j-1] = texts[j-1], texts[j]
		}
	}
}

// insertionSortRange performs insertion sort on a range
func (os *OptimizedSorter) insertionSortRange(texts []Text, low, high int, less func(i, j int) bool) {
	for i := low + 1; i <= high; i++ {
		for j := i; j > low && less(j, j-1); j-- {
			texts[j], texts[j-1] = texts[j-1], texts[j]
		}
	}
}

// reverse reverses a subarray
func (os *OptimizedSorter) reverse(texts []Text) {
	for i, j := 0, len(texts)-1; i < j; i, j = i+1, j-1 {
		texts[i], texts[j] = texts[j], texts[i]
	}
}

// mergeRuns merges two adjacent runs
func (os *OptimizedSorter) mergeRuns(texts []Text, run1, run2 *run, less func(i, j int) bool) {
	// Use galloping mode for better performance on partially sorted data
	os.gallopMerge(texts, run1.start, run1.start+run1.len, run2.start+run2.len, less)
	run1.len += run2.len
}

// gallopMerge performs a galloping merge
func (os *OptimizedSorter) gallopMerge(texts []Text, start, mid, end int, less func(i, j int) bool) {
	if mid-start <= end-mid {
		// Left run is smaller, merge right to left
		for i := mid; i < end; i++ {
			temp := texts[i]
			j := i
			for j > start && less(i, j-1) {
				texts[j] = texts[j-1]
				j--
			}
			texts[j] = temp
		}
	} else {
		// Right run is smaller, merge left to right
		for i := mid - 1; i >= start; i-- {
			temp := texts[i]
			j := i
			for j < end-1 && less(j+1, j) {
				texts[j] = texts[j+1]
				j++
			}
			texts[j] = temp
		}
	}
}

// parallelSort performs parallel merge sort for large collections
func (os *OptimizedSorter) parallelSort(texts []Text, less func(i, j int) bool) {
	if len(texts) <= 1 {
		return
	}

	// Determine number of goroutines to use
	numWorkers := runtime.NumCPU()
	if len(texts) < numWorkers*1000 {
		// For collections that aren't extremely large, use fewer workers
		numWorkers = (len(texts) + 999) / 1000 // Ceiling division
		if numWorkers < 2 {
			numWorkers = 2
		}
	}

	// Divide the slice into chunks for each worker (rounded up to avoid zero-sized chunks)
	chunkSize := (len(texts) + numWorkers - 1) / numWorkers
	if chunkSize < 100 && len(texts) >= 100 {
		chunkSize = 100
	}
	numWorkers = (len(texts) + chunkSize - 1) / chunkSize
	if numWorkers < 1 {
		numWorkers = 1
	}

	// Channel to receive sorted chunks
	results := make(chan struct {
		start, end int
	}, numWorkers)

	var wg sync.WaitGroup

	// Launch sorting goroutines
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			start := workerID * chunkSize
			end := (workerID + 1) * chunkSize
			if workerID == numWorkers-1 { // Last worker takes any remainder
				end = len(texts)
			}

			// Sort this chunk sequentially. Chunk-level parallelism is already provided by goroutines.
			sort.Slice(texts[start:end], func(i, j int) bool {
				return less(start+i, start+j)
			})

			results <- struct {
				start, end int
			}{start: start, end: end}
		}(i)
	}

	// Close the results channel when all workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Wait for all chunks to be sorted
	for range results {
		// Just receive to ensure all workers complete
	}

	// Merge sorted chunks
	os.mergeChunks(texts, chunkSize, less)
}

// mergeChunks merges sorted chunks in the array
func (os *OptimizedSorter) mergeChunks(texts []Text, chunkSize int, less func(i, j int) bool) {
	// For multiple chunks, perform multi-way merge
	// This implementation will handle merging in pairs until one sorted array remains
	numChunks := (len(texts) + chunkSize - 1) / chunkSize // Ceiling division
	if numChunks <= 1 {
		return
	}

	// Create a temporary array for merging
	temp := make([]Text, len(texts))

	// Multi-way merge of all chunks
	os.multiWayMerge(texts, temp, chunkSize, less)
}

// multiWayMerge performs a multi-way merge of sorted chunks
func (os *OptimizedSorter) multiWayMerge(texts, temp []Text, chunkSize int, less func(i, j int) bool) {
	// Use a simpler approach: iteratively merge pairs of chunks
	for currentSize := chunkSize; currentSize < len(texts); currentSize *= 2 {
		// Merge pairs of chunks of currentSize
		for start := 0; start < len(texts); start += 2 * currentSize {
			mid := start + currentSize
			end := start + 2*currentSize
			if mid > len(texts) {
				mid = len(texts)
			}
			if end > len(texts) {
				end = len(texts)
			}

			if mid < end { // Only merge if both halves exist
				os.merge(texts, temp, start, mid, end, less)
				// Copy merged result back to original
				copy(texts[start:end], temp[start:end])
			}
		}
	}
}

// merge merges two sorted subarrays
func (os *OptimizedSorter) merge(texts, temp []Text, start, mid, end int, less func(i, j int) bool) {
	i, j, k := start, mid, start

	for i < mid && j < end {
		if less(i, j) || !less(j, i) { // Equal case: take from left to maintain stability
			temp[k] = texts[i]
			i++
		} else {
			temp[k] = texts[j]
			j++
		}
		k++
	}

	// Copy remaining elements
	for i < mid {
		temp[k] = texts[i]
		i++
		k++
	}
	for j < end {
		temp[k] = texts[j]
		j++
		k++
	}
}

// SortTextVerticalByOptimized sorts TextVertical using optimized algorithm
func (os *OptimizedSorter) SortTextVerticalByOptimized(tv TextVertical) {
	os.SortTexts(tv, func(i, j int) bool {
		if tv[i].Y != tv[j].Y {
			return tv[i].Y > tv[j].Y
		}
		return tv[i].X < tv[j].X
	})
}

// SortTextHorizontalByOptimized sorts TextHorizontal using optimized algorithm
func (os *OptimizedSorter) SortTextHorizontalByOptimized(th TextHorizontal) {
	os.SortTexts(th, func(i, j int) bool {
		if th[i].X != th[j].X {
			return th[i].X < th[j].X
		}
		return th[i].Y > th[j].Y
	})
}

// QuickSortTexts implements quicksort for text collections
func (os *OptimizedSorter) QuickSortTexts(texts []Text, less func(i, j int) bool) {
	if len(texts) <= 1 {
		return
	}
	os.quickSortRange(texts, 0, len(texts)-1, less)
}

// quickSortRange sorts a range of texts using quicksort
func (os *OptimizedSorter) quickSortRange(texts []Text, low, high int, less func(i, j int) bool) {
	if low < high {
		// For very small ranges, use insertion sort
		if high-low < 10 {
			os.insertionSortRange(texts, low, high, less)
			return
		}

		// Partition and recursively sort
		pivot := os.partition(texts, low, high, less)
		os.quickSortRange(texts, low, pivot-1, less)
		os.quickSortRange(texts, pivot+1, high, less)
	}
}

// partition partitions the array for quicksort
func (os *OptimizedSorter) partition(texts []Text, low, high int, less func(i, j int) bool) int {
	// Use median-of-three to choose pivot
	mid := (low + high) / 2
	if less(mid, low) {
		texts[low], texts[mid] = texts[mid], texts[low]
	}
	if less(high, low) {
		texts[low], texts[high] = texts[high], texts[low]
	}
	if less(high, mid) {
		texts[mid], texts[high] = texts[high], texts[mid]
	}

	// Move median to end
	texts[mid], texts[high] = texts[high], texts[mid]

	pivot := high
	i := low - 1

	for j := low; j < high; j++ {
		if less(j, pivot) || !less(pivot, j) {
			i++
			texts[i], texts[j] = texts[j], texts[i]
		}
	}

	texts[i+1], texts[high] = texts[high], texts[i+1]
	return i + 1
}

// OptimizedTextClusterSorter provides optimized sorting for text clusters
type OptimizedTextClusterSorter struct {
	sorter *OptimizedSorter
}

// NewOptimizedTextClusterSorter creates a new optimized cluster sorter
func NewOptimizedTextClusterSorter() *OptimizedTextClusterSorter {
	return &OptimizedTextClusterSorter{
		sorter: NewOptimizedSorter(),
	}
}

// SortTextBlocks sorts text blocks by various criteria
func (otcs *OptimizedTextClusterSorter) SortTextBlocks(blocks []*TextBlock, sortBy string) {
	switch sortBy {
	case "position":
		otcs.sortByPosition(blocks)
	case "size":
		otcs.sortBySize(blocks)
	case "alphabetical":
		otcs.sortAlphabetically(blocks)
	default:
		otcs.sortByPosition(blocks) // Default to position-based sorting
	}
}

// sortByPosition sorts text blocks by position (top to bottom, then left to right)
func (otcs *OptimizedTextClusterSorter) sortByPosition(blocks []*TextBlock) {
	// Sort by Y descending (top to bottom in PDF coordinates), then X ascending
	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].Center().Y != blocks[j].Center().Y {
			return blocks[i].Center().Y > blocks[j].Center().Y // Higher Y is top in PDF
		}
		return blocks[i].Center().X < blocks[j].Center().X
	})
}

// sortBySize sorts text blocks by size
func (otcs *OptimizedTextClusterSorter) sortBySize(blocks []*TextBlock) {
	sort.Slice(blocks, func(i, j int) bool {
		area1 := blocks[i].Width() * blocks[i].Height()
		area2 := blocks[j].Width() * blocks[j].Height()
		return area1 > area2
	})
}

// sortAlphabetically sorts text blocks by their text content
func (otcs *OptimizedTextClusterSorter) sortAlphabetically(blocks []*TextBlock) {
	sort.Slice(blocks, func(i, j int) bool {
		if len(blocks[i].Texts) == 0 {
			return len(blocks[j].Texts) > 0
		}
		if len(blocks[j].Texts) == 0 {
			return false
		}
		return blocks[i].Texts[0].S < blocks[j].Texts[0].S
	})
}

// SortTextsWithAlgorithm allows choosing a specific sorting algorithm
func (os *OptimizedSorter) SortTextsWithAlgorithm(texts []Text, less func(i, j int) bool, algorithm string) {
	switch algorithm {
	case "quicksort":
		os.QuickSortTexts(texts, less)
	case "mergesort", "parallel":
		os.parallelSort(texts, less)
	case "stdsort":
		sort.Slice(texts, less)
	default:
		// Use adaptive approach based on size
		os.SortTexts(texts, less)
	}
}
