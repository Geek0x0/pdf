package pdf

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// TestParallelProcessorInitialization tests the initialization of the parallel processor
func TestParallelProcessorInitialization(t *testing.T) {
	// Test with specific number of workers
	pp := NewParallelProcessor(4)
	if pp.numWorkers != 4 {
		t.Errorf("Expected 4 workers, got %d", pp.numWorkers)
	}

	// Test with 0 workers (should default to CPU count)
	pp2 := NewParallelProcessor(0)
	expectedWorkers := runtime.NumCPU()
	if pp2.numWorkers != expectedWorkers {
		t.Errorf("Expected %d workers for 0 input, got %d", expectedWorkers, pp2.numWorkers)
	}

	// Test with negative workers (should default to CPU count)
	pp3 := NewParallelProcessor(-1)
	if pp3.numWorkers != expectedWorkers {
		t.Errorf("Expected %d workers for -1 input, got %d", expectedWorkers, pp3.numWorkers)
	}
}

// TestProcessPagesParallel tests parallel page processing
func TestProcessPagesParallel(t *testing.T) {
	ctx := context.Background()

	// Create test pages - we'll use mock pages with simple text extraction
	pages := make([]Page, 3)

	// Mock processor function that simulates some work
	processorFunc := func(page Page) ([]Text, error) {
		// Simulate processing time
		time.Sleep(1 * time.Millisecond)
		// Return a simple text result
		return []Text{{S: "processed", X: 10, Y: 20, FontSize: 12}}, nil
	}

	pp := NewParallelProcessor(2) // Use 2 workers
	results, err := pp.ProcessPages(ctx, pages, processorFunc)

	if err != nil {
		t.Fatalf("ProcessPages returned error: %v", err)
	}

	if len(results) != len(pages) {
		t.Errorf("Expected %d results, got %d", len(pages), len(results))
	}

	// Verify that all results contain the processed text
	for _, result := range results {
		if len(result) == 0 || result[0].S != "processed" {
			t.Error("Expected result to contain 'processed' text")
		}
	}
}

// TestProcessPagesWithCancellation tests cancellation functionality
func TestProcessPagesWithCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Create many pages to ensure we have work in progress
	pages := make([]Page, 10)

	// Slow processor function to make sure cancellation happens
	processorFunc := func(page Page) ([]Text, error) {
		time.Sleep(10 * time.Millisecond) // Slower than timeout
		return []Text{{S: "slow_result", X: 10, Y: 20, FontSize: 12}}, nil
	}

	pp := NewParallelProcessor(3)
	_, err := pp.ProcessPages(ctx, pages, processorFunc)

	// Should return context error because of timeout
	if err == nil {
		t.Error("Expected context cancellation error, got nil")
	} else if err != context.DeadlineExceeded && err != context.Canceled {
		t.Errorf("Expected context cancellation error, got: %v", err)
	}
}

// TestProcessTextBlocksParallel tests parallel text block processing
func TestProcessTextBlocksParallel(t *testing.T) {
	ctx := context.Background()

	// Create test text blocks
	textBlocks := []*TextBlock{
		{Texts: []Text{{S: "block1", X: 10, Y: 20, FontSize: 12}}},
		{Texts: []Text{{S: "block2", X: 30, Y: 40, FontSize: 12}}},
		{Texts: []Text{{S: "block3", X: 50, Y: 60, FontSize: 12}}},
	}

	// Processor function that modifies the block
	processorFunc := func(block *TextBlock) (*TextBlock, error) {
		// Simulate processing work
		modifiedBlock := *block
		modifiedBlock.Texts[0].S = "processed_" + block.Texts[0].S
		return &modifiedBlock, nil
	}

	pp := NewParallelProcessor(2)
	results, err := pp.ProcessTextBlocks(ctx, textBlocks, processorFunc)

	if err != nil {
		t.Fatalf("ProcessTextBlocks returned error: %v", err)
	}

	if len(results) != len(textBlocks) {
		t.Errorf("Expected %d results, got %d", len(textBlocks), len(results))
	}

	// Verify that all results are modified correctly
	for i, result := range results {
		expectedText := "processed_" + textBlocks[i].Texts[0].S
		if result.Texts[0].S != expectedText {
			t.Errorf("Expected '%s', got '%s'", expectedText, result.Texts[0].S)
		}
	}
}

// TestProcessTextInParallel tests parallel text element processing
func TestProcessTextInParallel(t *testing.T) {
	ctx := context.Background()

	// Create test texts
	texts := []Text{
		{S: "text1", X: 10, Y: 20, FontSize: 12},
		{S: "text2", X: 30, Y: 40, FontSize: 12},
		{S: "text3", X: 50, Y: 60, FontSize: 12},
	}

	// Processor function that modifies the text
	processorFunc := func(text Text) (Text, error) {
		processedText := text
		processedText.S = "processed_" + text.S
		return processedText, nil
	}

	pp := NewParallelProcessor(2)
	results, err := pp.ProcessTextInParallel(ctx, texts, processorFunc)

	if err != nil {
		t.Fatalf("ProcessTextInParallel returned error: %v", err)
	}

	if len(results) != len(texts) {
		t.Errorf("Expected %d results, got %d", len(texts), len(results))
	}

	// Verify that all results are modified correctly
	for i, result := range results {
		expectedText := "processed_" + texts[i].S
		if result.S != expectedText {
			t.Errorf("Expected '%s', got '%s'", expectedText, result.S)
		}
	}
}

// TestParallelTextExtractor tests the ParallelTextExtractor functionality
func TestParallelTextExtractor(t *testing.T) {
	// This test would need a mock reader, so we'll create a simpler test
	// For now, test the initialization and basic functionality

	extractor := NewParallelTextExtractor(2)
	if extractor.processor.numWorkers != 2 {
		t.Errorf("Expected 2 workers, got %d", extractor.processor.numWorkers)
	}
}

// TestParallelSort tests the parallel sorting functionality
func TestParallelSort(t *testing.T) {
	ctx := context.Background()

	// Create a large slice of texts to sort
	texts := make([]Text, 1000)
	for i := range texts {
		texts[i] = Text{
			S:        string(rune('z' - (i % 26))), // Varying characters
			X:        float64(1000 - i),            // Descending X values
			FontSize: float64(i%20 + 10),           // Varying font sizes
		}
	}

	// Create a processor for testing
	pte := &ParallelTextExtractor{processor: NewParallelProcessor(2)}

	// Define a less function to sort by X in ascending order
	less := func(i, j int) bool {
		return texts[i].X < texts[j].X
	}

	// Test parallel sort
	err := pte.ParallelSort(ctx, texts, less)
	if err != nil {
		t.Fatalf("ParallelSort returned error: %v", err)
	}

	// Verify that the slice is sorted correctly
	isSorted := true
	for i := 1; i < len(texts); i++ {
		if texts[i-1].X > texts[i].X {
			isSorted = false
			break
		}
	}

	if !isSorted {
		t.Error("Slice was not sorted correctly")
	}
}

// TestParallelSortWithCancellation tests cancellation during parallel sort
func TestParallelSortWithCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Create a large slice to ensure the sort takes some time
	texts := make([]Text, 10000)
	for i := range texts {
		texts[i] = Text{
			X: float64(i),
		}
	}

	pte := &ParallelTextExtractor{processor: NewParallelProcessor(4)}

	// Sorting should be fast, but the cancellation test ensures the function responds to context
	err := pte.ParallelSort(ctx, texts, func(i, j int) bool {
		return texts[i].X < texts[j].X
	})

	// Should not get an error for timeout since sort is fast
	if err != nil && err != context.DeadlineExceeded {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestMergeFunctionParallelProcessing tests the merge functionality used in parallel sorting
func TestMergeFunctionParallelProcessing(t *testing.T) {
	// Create two sorted slices
	slice1 := []Text{{X: 1}, {X: 3}, {X: 5}}
	slice2 := []Text{{X: 2}, {X: 4}, {X: 6}}

	// Combine them in order in a target slice
	all := make([]Text, 6)
	copy(all[0:3], slice1)
	copy(all[3:6], slice2)

	// Create a temporary slice for merging
	temp := make([]Text, 6)

	// Use our merge function
	os := NewOptimizedSorter()
	os.merge(all, temp, 0, 3, 6, func(i, j int) bool {
		return all[i].X < all[j].X
	})

	// Verify the result is sorted
	expectedX := []float64{1, 2, 3, 4, 5, 6}
	for i, x := range expectedX {
		if temp[i].X != x {
			t.Errorf("Expected X[%d] = %f, got %f", i, x, temp[i].X)
		}
	}
}

// BenchmarkParallelProcessPages benchmarks parallel page processing
func BenchmarkParallelProcessPages(b *testing.B) {
	ctx := context.Background()

	pages := make([]Page, 10)
	processorFunc := func(page Page) ([]Text, error) {
		return []Text{{S: "result", X: 10, Y: 20, FontSize: 12}}, nil
	}

	pp := NewParallelProcessor(runtime.NumCPU())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := pp.ProcessPages(ctx, pages, processorFunc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParallelProcessTextInParallel benchmarks parallel text processing
func BenchmarkParallelProcessTextInParallel(b *testing.B) {
	ctx := context.Background()

	texts := make([]Text, 100)
	for i := range texts {
		texts[i] = Text{S: "text", X: float64(i), Y: float64(i), FontSize: 12}
	}

	processorFunc := func(text Text) (Text, error) {
		return text, nil
	}

	pp := NewParallelProcessor(runtime.NumCPU())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := pp.ProcessTextInParallel(ctx, texts, processorFunc)
		if err != nil {
			b.Fatal(err)
		}
	}
}
