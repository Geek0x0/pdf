package pdf

import (
	"reflect"
	"sort"
	"testing"
)

// TestNewOptimizedSorter tests the optimized sorter initialization
func TestNewOptimizedSorter(t *testing.T) {
	os := NewOptimizedSorter()

	if os.parallelThreshold != 10000 {
		t.Errorf("Expected parallelThreshold 10000, got %d", os.parallelThreshold)
	}
}

// TestOptimizedSorterSortTextsEmpty tests sorting empty slice
func TestOptimizedSorterSortTextsEmpty(t *testing.T) {
	os := NewOptimizedSorter()

	texts := []Text{}

	less := func(i, j int) bool {
		return texts[i].X < texts[j].X
	}

	// Should not panic
	os.SortTexts(texts, less)

	if len(texts) != 0 {
		t.Errorf("Expected empty slice, got length %d", len(texts))
	}
}

// TestOptimizedSorterSortTextsSingle tests sorting single element
func TestOptimizedSorterSortTextsSingle(t *testing.T) {
	os := NewOptimizedSorter()

	texts := []Text{{S: "test", X: 100, Y: 200, FontSize: 12}}

	less := func(i, j int) bool {
		return texts[i].X < texts[j].X
	}

	os.SortTexts(texts, less)

	if len(texts) != 1 {
		t.Errorf("Expected 1 element, got %d", len(texts))
	}
	if texts[0].S != "test" {
		t.Error("Expected text to remain unchanged")
	}
}

// TestOptimizedSorterSortTextsByX tests sorting by X coordinate
func TestOptimizedSorterSortTextsByX(t *testing.T) {
	os := NewOptimizedSorter()

	texts := []Text{
		{S: "right", X: 300, Y: 100, FontSize: 12},
		{S: "left", X: 100, Y: 200, FontSize: 12},
		{S: "middle", X: 200, Y: 150, FontSize: 12},
	}

	// Sort by X coordinate (ascending)
	less := func(i, j int) bool {
		return texts[i].X < texts[j].X
	}

	os.SortTexts(texts, less)

	// Verify the order is left (100), middle (200), right (300)
	if len(texts) != 3 {
		t.Fatalf("Expected 3 elements, got %d", len(texts))
	}

	if texts[0].S != "left" || texts[1].S != "middle" || texts[2].S != "right" {
		t.Errorf("Expected [left, middle, right], got [%s, %s, %s]",
			texts[0].S, texts[1].S, texts[2].S)
	}

	// Verify X values are in ascending order
	if texts[0].X > texts[1].X || texts[1].X > texts[2].X {
		t.Error("Texts are not sorted by X in ascending order")
	}
}

// TestOptimizedSorterSortTextsByY tests sorting by Y coordinate
func TestOptimizedSorterSortTextsByY(t *testing.T) {
	os := NewOptimizedSorter()

	texts := []Text{
		{S: "top", X: 100, Y: 300, FontSize: 12},
		{S: "bottom", X: 200, Y: 100, FontSize: 12},
		{S: "middle", X: 150, Y: 200, FontSize: 12},
	}

	// Sort by Y coordinate (ascending)
	less := func(i, j int) bool {
		return texts[i].Y < texts[j].Y
	}

	os.SortTexts(texts, less)

	// Verify the order is bottom (100), middle (200), top (300)
	if len(texts) != 3 {
		t.Fatalf("Expected 3 elements, got %d", len(texts))
	}

	if texts[0].S != "bottom" || texts[1].S != "middle" || texts[2].S != "top" {
		t.Errorf("Expected [bottom, middle, top], got [%s, %s, %s]",
			texts[0].S, texts[1].S, texts[2].S)
	}

	// Verify Y values are in ascending order
	if texts[0].Y > texts[1].Y || texts[1].Y > texts[2].Y {
		t.Error("Texts are not sorted by Y in ascending order")
	}
}

// TestOptimizedSorterParallelSort tests parallel sorting with larger dataset
func TestOptimizedSorterParallelSort(t *testing.T) {
	os := NewOptimizedSorter()
	// Temporarily lower the threshold to trigger parallel sorting
	originalThreshold := os.parallelThreshold
	os.parallelThreshold = 10 // Force parallel sort for 20+ elements
	defer func() { os.parallelThreshold = originalThreshold }()

	// Create 20 texts with random X values
	texts := make([]Text, 20)
	for i := 0; i < 20; i++ {
		texts[i] = Text{
			X:        float64(20 - i), // Reverse order initially
			FontSize: 12,
		}
	}

	less := func(i, j int) bool {
		return texts[i].X < texts[j].X
	}

	os.SortTexts(texts, less)

	// Verify that the slice is sorted in ascending order
	for i := 1; i < len(texts); i++ {
		if texts[i-1].X > texts[i].X {
			t.Errorf("Texts not sorted at index %d: %f > %f", i, texts[i-1].X, texts[i].X)
		}
	}
}

// TestOptimizedSorterQuickSort tests quicksort functionality
func TestOptimizedSorterQuickSort(t *testing.T) {
	os := NewOptimizedSorter()

	texts := []Text{
		{X: 30, FontSize: 12},
		{X: 10, FontSize: 12},
		{X: 50, FontSize: 12},
		{X: 20, FontSize: 12},
		{X: 40, FontSize: 12},
	}

	less := func(i, j int) bool {
		return texts[i].X < texts[j].X
	}

	os.QuickSortTexts(texts, less)

	// Verify sorted order
	expectedOrder := []float64{10, 20, 30, 40, 50}
	for i, expected := range expectedOrder {
		if texts[i].X != expected {
			t.Errorf("At index %d: expected %f, got %f", i, expected, texts[i].X)
		}
	}
}

// TestOptimizedSorterQuickSortEmpty tests quicksort with empty slice
func TestOptimizedSorterQuickSortEmpty(t *testing.T) {
	os := NewOptimizedSorter()

	texts := []Text{}
	less := func(i, j int) bool {
		return texts[i].X < texts[j].X
	}

	// Should not panic
	os.QuickSortTexts(texts, less)

	if len(texts) != 0 {
		t.Errorf("Expected empty slice, got length %d", len(texts))
	}
}

// TestOptimizedSorterQuickSortSingle tests quicksort with single element
func TestOptimizedSorterQuickSortSingle(t *testing.T) {
	os := NewOptimizedSorter()

	texts := []Text{{X: 100, FontSize: 12}}
	less := func(i, j int) bool {
		return texts[i].X < texts[j].X
	}

	os.QuickSortTexts(texts, less)

	if len(texts) != 1 {
		t.Errorf("Expected 1 element, got %d", len(texts))
	}
	if texts[0].X != 100 {
		t.Error("Expected X value to remain unchanged")
	}
}

// TestOptimizedSorterQuickSortDuplicateValues tests quicksort with duplicate values
func TestOptimizedSorterQuickSortDuplicateValues(t *testing.T) {
	os := NewOptimizedSorter()

	texts := []Text{
		{X: 20, FontSize: 12, S: "A"},
		{X: 10, FontSize: 12, S: "B"},
		{X: 20, FontSize: 12, S: "C"}, // Duplicate X value
		{X: 10, FontSize: 12, S: "D"}, // Duplicate X value
	}

	less := func(i, j int) bool {
		// Primary sort by X, secondary by string for stability test
		if texts[i].X == texts[j].X {
			return texts[i].S < texts[j].S // This won't be called in current implementation
		}
		return texts[i].X < texts[j].X
	}

	os.QuickSortTexts(texts, less)

	// Should be sorted by X value
	for i := 1; i < len(texts); i++ {
		if texts[i-1].X > texts[i].X {
			t.Errorf("Not sorted: texts[%d].X (%f) > texts[%d].X (%f)",
				i-1, texts[i-1].X, i, texts[i].X)
		}
	}
}

// TestSortTextVerticalByOptimized tests the TextVertical optimized sort
func TestSortTextVerticalByOptimized(t *testing.T) {
	os := NewOptimizedSorter()

	// Create TextVertical slice
	tv := TextVertical{
		{S: "top", X: 100, Y: 300, FontSize: 12},
		{S: "middle", X: 100, Y: 200, FontSize: 12},
		{S: "bottom", X: 100, Y: 100, FontSize: 12},
		{S: "top_right", X: 200, Y: 300, FontSize: 12}, // Same Y as "top"
	}

	os.SortTextVerticalByOptimized(tv)

	// Expected order: top (Y=300), top_right (Y=300, X>100), middle (Y=200), bottom (Y=100)
	// In TextVertical sorting: Y desc, then X asc
	if len(tv) != 4 {
		t.Fatalf("Expected 4 elements, got %d", len(tv))
	}

	// Check that Y values are in descending order
	for i := 1; i < len(tv); i++ {
		if tv[i-1].Y < tv[i].Y { // Descending order
			t.Errorf("Y values not in descending order at index %d", i)
		}
		// For same Y values, X should be in ascending order
		if tv[i-1].Y == tv[i].Y && tv[i-1].X > tv[i].X {
			t.Errorf("X values not in ascending order for same Y at index %d", i)
		}
	}
}

// TestSortTextHorizontalByOptimized tests the TextHorizontal optimized sort
func TestSortTextHorizontalByOptimized(t *testing.T) {
	os := NewOptimizedSorter()

	// Create TextHorizontal slice
	th := TextHorizontal{
		{S: "right", X: 300, Y: 200, FontSize: 12},
		{S: "left", X: 100, Y: 200, FontSize: 12},
		{S: "middle", X: 200, Y: 200, FontSize: 12},
		{S: "left_lower", X: 100, Y: 100, FontSize: 12}, // Same X as "left", lower Y
	}

	os.SortTextHorizontalByOptimized(th)

	// Expected order: left (X=100, Y=200), left_lower (X=100, Y=100), middle (X=200), right (X=300)
	// In TextHorizontal sorting: X asc, then Y desc
	if len(th) != 4 {
		t.Fatalf("Expected 4 elements, got %d", len(th))
	}

	// Check that X values are in ascending order, and for same X, Y is descending
	for i := 1; i < len(th); i++ {
		if th[i-1].X > th[i].X { // Ascending order for X
			t.Errorf("X values not in ascending order at index %d", i)
		}
		// For same X values, Y should be in descending order
		if th[i-1].X == th[i].X && th[i-1].Y < th[i].Y {
			t.Errorf("Y values not in descending order for same X at index %d", i)
		}
	}
}

// TestInsertionSort tests the insertion sort used for small ranges
func TestInsertionSort(t *testing.T) {
	os := NewOptimizedSorter()

	texts := make([]Text, 5)
	for i := 0; i < 5; i++ {
		texts[i] = Text{X: float64(5 - i), FontSize: 12} // [5, 4, 3, 2, 1]
	}

	less := func(i, j int) bool {
		return texts[i].X < texts[j].X
	}

	os.insertionSortRange(texts, 0, len(texts)-1, less)

	// Should be sorted [1, 2, 3, 4, 5]
	expected := []float64{1, 2, 3, 4, 5}
	for i, exp := range expected {
		if texts[i].X != exp {
			t.Errorf("At index %d: expected %f, got %f", i, exp, texts[i].X)
		}
	}
}

// TestPartition tests the quicksort partition function
func TestPartition(t *testing.T) {
	os := NewOptimizedSorter()

	texts := []Text{
		{X: 5, FontSize: 12},
		{X: 2, FontSize: 12},
		{X: 8, FontSize: 12},
		{X: 1, FontSize: 12},
		{X: 9, FontSize: 12},
	}

	less := func(i, j int) bool {
		return texts[i].X < texts[j].X
	}

	// Partition the whole array
	pivotIndex := os.partition(texts, 0, len(texts)-1, less)

	// After partitioning, elements before pivot should be <= pivot,
	// elements after pivot should be >= pivot
	pivotValue := texts[pivotIndex].X

	for i := 0; i < pivotIndex; i++ {
		if texts[i].X > pivotValue {
			t.Errorf("Element at index %d (%f) should be <= pivot %f", i, texts[i].X, pivotValue)
		}
	}

	for i := pivotIndex + 1; i < len(texts); i++ {
		if texts[i].X < pivotValue {
			t.Errorf("Element at index %d (%f) should be >= pivot %f", i, texts[i].X, pivotValue)
		}
	}
}

// TestSortTextsWithAlgorithm tests the algorithm selection functionality
func TestSortTextsWithAlgorithm(t *testing.T) {
	// Start with unsorted data
	originalTexts := []Text{
		{X: 30, FontSize: 12},
		{X: 10, FontSize: 12},
		{X: 50, FontSize: 12},
		{X: 20, FontSize: 12},
		{X: 40, FontSize: 12},
	}

	os := NewOptimizedSorter()
	makeLess := func(arr []Text) func(i, j int) bool {
		return func(i, j int) bool {
			return arr[i].X < arr[j].X
		}
	}

	// Test with standard sort algorithm
	texts1 := make([]Text, len(originalTexts))
	copy(texts1, originalTexts)
	os.SortTextsWithAlgorithm(texts1, makeLess(texts1), "stdsort")

	// Test with quicksort algorithm
	texts2 := make([]Text, len(originalTexts))
	copy(texts2, originalTexts)
	os.SortTextsWithAlgorithm(texts2, makeLess(texts2), "quicksort")

	// Test with mergesort/parallel algorithm
	texts3 := make([]Text, len(originalTexts))
	copy(texts3, originalTexts)
	os.SortTextsWithAlgorithm(texts3, makeLess(texts3), "mergesort")

	expectedOrder := []float64{10, 20, 30, 40, 50}

	// Verify all three sorting approaches result in correct order
	for i, expected := range expectedOrder {
		if texts1[i].X != expected {
			t.Errorf("stdsort: At index %d, expected %f, got %f", i, expected, texts1[i].X)
		}
		if texts2[i].X != expected {
			t.Errorf("quicksort: At index %d, expected %f, got %f", i, expected, texts2[i].X)
		}
		if texts3[i].X != expected {
			t.Errorf("mergesort: At index %d, expected %f, got %f", i, expected, texts3[i].X)
		}
	}

	// All should be equal to each other
	if !reflect.DeepEqual(texts1, texts2) || !reflect.DeepEqual(texts2, texts3) {
		t.Error("All sorting algorithms should produce same result")
	}
}

// TestNewOptimizedTextClusterSorter tests the cluster sorter initialization
func TestNewOptimizedTextClusterSorter(t *testing.T) {
	otcs := NewOptimizedTextClusterSorter()

	if otcs.sorter == nil {
		t.Error("Expected sorter to be initialized")
	}
}

// TestSortTextBlocksByPosition tests sorting text blocks by position
func TestSortTextBlocksByPosition(t *testing.T) {
	otcs := NewOptimizedTextClusterSorter()

	blocks := []*TextBlock{
		{
			Texts: []Text{{S: "bottom", X: 100, Y: 100, FontSize: 12}},
			MinX:  100, MaxX: 200, MinY: 100, MaxY: 112, AvgFontSize: 12,
		},
		{
			Texts: []Text{{S: "top", X: 100, Y: 300, FontSize: 12}},
			MinX:  100, MaxX: 200, MinY: 300, MaxY: 312, AvgFontSize: 12,
		},
		{
			Texts: []Text{{S: "middle", X: 100, Y: 200, FontSize: 12}},
			MinX:  100, MaxX: 200, MinY: 200, MaxY: 212, AvgFontSize: 12,
		},
	}

	otcs.SortTextBlocks(blocks, "position")

	// In PDF coordinates, higher Y is "top"
	// So we expect: top (Y~300), middle (Y~200), bottom (Y~100)
	// Because of how center.Y is calculated and the sorting logic

	if len(blocks) != 3 {
		t.Fatalf("Expected 3 blocks, got %d", len(blocks))
	}

	// Check Y values are in expected order (top to bottom means descending Y)
	for i := 1; i < len(blocks); i++ {
		if blocks[i-1].Center().Y < blocks[i].Center().Y { // Y should be descending (top to bottom)
			t.Errorf("Blocks not sorted by Y position correctly")
		}
	}
}

// TestSortTextBlocksBySize tests sorting text blocks by size
func TestSortTextBlocksBySize(t *testing.T) {
	otcs := NewOptimizedTextClusterSorter()

	blocks := []*TextBlock{
		{
			MinX: 100, MaxX: 150, MinY: 100, MaxY: 110, // Size: 50x10 = 500
			AvgFontSize: 12,
		},
		{
			MinX: 200, MaxX: 220, MinY: 200, MaxY: 205, // Size: 20x5 = 100
			AvgFontSize: 12,
		},
		{
			MinX: 300, MaxX: 400, MinY: 300, MaxY: 320, // Size: 100x20 = 2000
			AvgFontSize: 12,
		},
	}

	otcs.SortTextBlocks(blocks, "size")

	// Should be sorted by area (descending): largest first
	// Expected order: block with area 2000, then 500, then 100
	if len(blocks) != 3 {
		t.Fatalf("Expected 3 blocks, got %d", len(blocks))
	}

	expectedAreas := []float64{2000, 500, 100} // Descending order
	for i, _ := range expectedAreas {
		calculatedArea := blocks[i].Width() * blocks[i].Height()
		// Note: May not be exact due to how areas are calculated in the sort
		// Just verify the order is correct
		if i < 2 && calculatedArea < (blocks[i+1].Width()*blocks[i+1].Height()) {
			t.Errorf("Blocks not sorted by size (descending) at index %d", i)
		}
	}
}

// TestSortTextBlocksByInvalidOption tests invalid sort option
func TestSortTextBlocksByInvalidOption(t *testing.T) {
	otcs := NewOptimizedTextClusterSorter()

	blocks := []*TextBlock{
		{
			MinX: 100, MaxX: 150, MinY: 100, MaxY: 110,
			AvgFontSize: 12,
		},
	}

	// Test with invalid sort option - should use default (position)
	originalY := blocks[0].Center().Y
	otcs.SortTextBlocks(blocks, "invalid_option")

	// Should still have the block
	if len(blocks) != 1 {
		t.Fatalf("Expected 1 block, got %d", len(blocks))
	}

	// Position shouldn't have changed for a single block
	if blocks[0].Center().Y != originalY {
		t.Error("Single block position changed unexpectedly")
	}
}

// TestMergeFunction tests the merge function used in merge sort
func TestMergeFunction(t *testing.T) {
	os := NewOptimizedSorter()

	// Create source slice [5, 3, 8, 1, 9, 2] - first half [5,3,8], second half [1,9,2]
	// When sorted: first half should become [3,5,8], second half [1,2,9]
	// After merge should be [1,2,3,5,8,9]

	texts := []Text{
		{X: 5, FontSize: 12},
		{X: 3, FontSize: 12},
		{X: 8, FontSize: 12},
		{X: 1, FontSize: 12},
		{X: 9, FontSize: 12},
		{X: 2, FontSize: 12},
	}

	// Assume first half [0,3) and second half [3,6) are sorted
	// Sort them first manually for the test
	less := func(i, j int) bool {
		return texts[i].X < texts[j].X
	}

	// Sort first half [0,3)
	half1 := texts[0:3]
	sort.Slice(half1, func(i, j int) bool {
		return half1[i].X < half1[j].X
	})

	// Sort second half [3,6]
	half2 := texts[3:6]
	sort.Slice(half2, func(i, j int) bool {
		return half2[i].X < half2[j].X
	})

	// Now texts should be [3,5,8,1,2,9] (first half sorted, second half sorted)
	// Create a temporary array for merging
	temp := make([]Text, len(texts))

	// Perform the merge
	os.merge(texts, temp, 0, 3, 6, less)

	// Now temp should be fully sorted [1,2,3,5,8,9]
	expected := []float64{1, 2, 3, 5, 8, 9}
	for i, exp := range expected {
		if temp[i].X != exp {
			t.Errorf("After merge, at index %d: expected %f, got %f", i, exp, temp[i].X)
		}
	}

	// But texts array should still be [3,5,8,1,2,9] until copied back
	if texts[0].X != 3 || texts[5].X != 9 {
		t.Log("Source array preserves original order until explicitly copied back")
	}
}

// BenchmarkOptimizedSorterSmall benchmarks small sort operations
func BenchmarkOptimizedSorterSmall(b *testing.B) {
	os := NewOptimizedSorter()

	texts := make([]Text, 10)
	for i := 0; i < 10; i++ {
		texts[i] = Text{X: float64(10 - i), FontSize: 12}
	}

	less := func(i, j int) bool {
		return texts[i].X < texts[j].X
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testTexts := make([]Text, len(texts))
		copy(testTexts, texts)
		os.SortTexts(testTexts, less)
	}
}

// BenchmarkOptimizedSorterLarge benchmarks large sort operations
func BenchmarkOptimizedSorterLarge(b *testing.B) {
	os := NewOptimizedSorter()
	// Temporarily lower threshold to ensure parallel sort is used
	originalThreshold := os.parallelThreshold
	os.parallelThreshold = 100
	defer func() { os.parallelThreshold = originalThreshold }()

	texts := make([]Text, 1000)
	for i := 0; i < 1000; i++ {
		texts[i] = Text{X: float64(1000 - i), FontSize: 12}
	}

	less := func(i, j int) bool {
		return texts[i].X < texts[j].X
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testTexts := make([]Text, len(texts))
		copy(testTexts, texts)
		os.SortTexts(testTexts, less)
	}
}

// BenchmarkStandardSortComparison benchmarks comparison with standard sort
func BenchmarkStandardSortComparison(b *testing.B) {
	texts := make([]Text, 1000)
	for i := 0; i < 1000; i++ {
		texts[i] = Text{X: float64(1000 - i), FontSize: 12}
	}

	less := func(i, j int) bool {
		return texts[i].X < texts[j].X
	}

	b.Run("StandardSort", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			testTexts := make([]Text, len(texts))
			copy(testTexts, texts)
			sort.Slice(testTexts, less)
		}
	})

	b.Run("OptimizedSorter", func(b *testing.B) {
		os := NewOptimizedSorter()
		// Lower the threshold to use parallel sort
		originalThreshold := os.parallelThreshold
		os.parallelThreshold = 100
		defer func() { os.parallelThreshold = originalThreshold }()

		for i := 0; i < b.N; i++ {
			testTexts := make([]Text, len(texts))
			copy(testTexts, texts)
			os.SortTexts(testTexts, less)
		}
	})
}
