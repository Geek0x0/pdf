// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestParallelExtractorUsage demonstrates actual usage of ParallelExtractor
func TestParallelExtractorUsage(t *testing.T) {
	// Create a parallel extractor (using 4 worker goroutines)
	extractor := NewParallelExtractor(4)
	defer extractor.Close()

	// Verify extractor creation succeeded
	if extractor == nil {
		t.Fatal("Failed to create ParallelExtractor")
	}

	// Verify component initialization
	if extractor.processor == nil {
		t.Error("Processor not initialized")
	}
	if extractor.cache == nil {
		t.Error("Cache not initialized")
	}
	if extractor.prefetcher == nil {
		t.Error("Prefetcher not initialized")
	}

	// Test statistics retrieval
	cacheStats := extractor.GetCacheStats()
	if cacheStats.Hits < 0 || cacheStats.Misses < 0 {
		t.Error("Invalid cache stats")
	}

	prefetchStats := extractor.GetPrefetchStats()
	if !prefetchStats.Enabled {
		t.Error("Prefetcher should be enabled by default")
	}
}

// TestReaderExtractAllPagesParallel demonstrates Reader's parallel extraction method
func TestReaderExtractAllPagesParallel(t *testing.T) {
	// Note: this test requires an actual PDF file
	// In actual usage, you need to provide a valid PDF path
	t.Skip("Skipping test that requires actual PDF file")

	// Example code:
	/*
		f, r, err := Open("sample.pdf")
		if err != nil {
			t.Fatalf("Failed to open PDF: %v", err)
		}
		defer f.Close()

		// Create context (with timeout)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Use parallel extractor to extract all pages
		// workers=0 means automatically use runtime.NumCPU()
		pages, err := r.ExtractAllPagesParallel(ctx, 0)
		if err != nil {
			t.Fatalf("Failed to extract pages: %v", err)
		}

		// Process results
		for i, text := range pages {
			fmt.Printf("Page %d: %d characters\n", i+1, len(text))
		}
	*/
}

// ExampleParallelExtractor_basic basic usage example
func ExampleParallelExtractor_basic() {
	// Create parallel extractor
	extractor := NewParallelExtractor(4) // use 4 worker goroutines
	defer extractor.Close()

	// Note: actual usage requires creating Page objects
	// pages := []Page{...}

	ctx := context.Background()

	// Simulate empty page list
	var pages []Page

	// Extract all pages
	results, err := extractor.ExtractAllPages(ctx, pages)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Extracted %d pages\n", len(results))
	// Output: Extracted 0 pages
}

// ExampleReader_ExtractAllPagesParallel uses Reader's parallel extraction method
func ExampleReader_ExtractAllPagesParallel() {
	// Note: this example requires actual PDF files
	// Here only shows API usage

	/*
		// Open PDF file
		f, r, err := Open("document.pdf")
		if err != nil {
			panic(err)
		}
		defer f.Close()

		// Create context
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()

		// Parallel extract all page texts
		pages, err := r.ExtractAllPagesParallel(ctx, 0) // 0 = auto-detect CPU core count
		if err != nil {
			panic(err)
		}

		// Output text for each page
		for i, pageText := range pages {
			fmt.Printf("Page %d has %d characters\n", i+1, len(pageText))
		}
	*/
}

// BenchmarkParallelExtractorVsSequential compare performance of parallel and sequential extraction
func BenchmarkParallelExtractorVsSequential(b *testing.B) {
	// Create simulated pages
	numPages := 100
	pages := make([]Page, numPages)

	b.Run("Sequential", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ctx := context.Background()
			for _, page := range pages {
				content := page.Content()
				_ = content.Text
			}
			_ = ctx
		}
	})

	b.Run("Parallel-2Workers", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			extractor := NewParallelExtractor(2)
			ctx := context.Background()
			_, _ = extractor.ExtractAllPages(ctx, pages)
			extractor.Close()
		}
	})

	b.Run("Parallel-4Workers", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			extractor := NewParallelExtractor(4)
			ctx := context.Background()
			_, _ = extractor.ExtractAllPages(ctx, pages)
			extractor.Close()
		}
	})

	b.Run("Parallel-AutoWorkers", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			extractor := NewParallelExtractor(0) // auto-detect
			ctx := context.Background()
			_, _ = extractor.ExtractAllPages(ctx, pages)
			extractor.Close()
		}
	})
}

// TestParallelExtractorCancellation test cancellation functionality
func TestParallelExtractorCancellation(t *testing.T) {
	extractor := NewParallelExtractor(4)
	defer extractor.Close()

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	// Try to extract (should fail quickly)
	pages := make([]Page, 10)
	_, err := extractor.ExtractAllPages(ctx, pages)

	// Should return cancellation error
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}
}

// TestParallelExtractorTimeout test timeout functionality
func TestParallelExtractorTimeout(t *testing.T) {
	extractor := NewParallelExtractor(2)
	defer extractor.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Wait for timeout
	time.Sleep(10 * time.Millisecond)

	// Try to extract (should timeout)
	pages := make([]Page, 10)
	_, err := extractor.ExtractAllPages(ctx, pages)

	// Should return timeout error
	if err != context.DeadlineExceeded {
		t.Errorf("Expected context.DeadlineExceeded error, got: %v", err)
	}
}

// TestParallelExtractorStats test statistics collection
func TestParallelExtractorStats(t *testing.T) {
	extractor := NewParallelExtractor(4)
	defer extractor.Close()

	// Get initial statistics
	cacheStats := extractor.GetCacheStats()
	prefetchStats := extractor.GetPrefetchStats()

	// Verify statistics structure
	if cacheStats.Hits != 0 {
		t.Errorf("Expected 0 initial hits, got %d", cacheStats.Hits)
	}
	if cacheStats.Misses != 0 {
		t.Errorf("Expected 0 initial misses, got %d", cacheStats.Misses)
	}
	if !prefetchStats.Enabled {
		t.Error("Prefetcher should be enabled by default")
	}
}
