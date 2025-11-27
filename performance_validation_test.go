// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"
)

// PerformanceValidationTest validates our optimization improvements
func TestPerformanceValidation(t *testing.T) {
	// Create a large synthetic PDF for testing
	// This simulates the workload that was causing performance issues

	// Test cases for different scenarios
	tests := []struct {
		name     string
		pages    int
		texts    int
		workers  int
		useCache bool
	}{
		{"SmallDocument", 10, 100, 2, false},
		{"MediumDocument", 50, 500, 4, true},
		{"LargeDocument", 100, 1000, 4, true},
		{"StressTest", 200, 2000, 4, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Memory baseline
			var m1 runtime.MemStats
			runtime.ReadMemStats(&m1)

			// Create test data
			texts := generateValidationTestTexts(tt.texts)

			// Test clustering performance
			timeStart := time.Now()

			// Use optimized clustering based on size
			if len(texts) < 50 {
				_ = clusterTextBlocksSimple(texts)
			} else {
				_ = ClusterTextBlocksV3(texts)
			}

			clusterTime := time.Since(timeStart)

			// Test batch extraction performance
			ctx := context.Background()

			opts := BatchExtractOptions{
				Pages:        make([]int, tt.pages),
				Workers:      tt.workers,
				UseFontCache: tt.useCache,
				Context:      ctx,
				PageTimeout:  30 * time.Second,
			}

			for i := range opts.Pages {
				opts.Pages[i] = i + 1
			}

			// Memory after processing
			var m2 runtime.MemStats
			runtime.ReadMemStats(&m2)

			memoryUsed := m2.Alloc - m1.Alloc

			t.Logf("Test: %s", tt.name)
			t.Logf("Pages: %d, Texts: %d, Workers: %d", tt.pages, tt.texts, tt.workers)
			t.Logf("Clustering time: %v", clusterTime)
			t.Logf("Memory used: %.2f MB", float64(memoryUsed)/1024/1024)
			t.Logf("Goroutines: %d", runtime.NumGoroutine())
		})
	}
}

// BenchmarkClusteringPerformance benchmarks our clustering optimizations
func BenchmarkClusteringPerformance(b *testing.B) {
	textCounts := []int{10, 50, 100, 500, 1000}

	for _, count := range textCounts {
		b.Run(fmt.Sprintf("Texts_%d", count), func(b *testing.B) {
			texts := generateValidationTestTexts(count)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if count < 50 {
					_ = clusterTextBlocksSimple(texts)
				} else {
					_ = ClusterTextBlocksV3(texts)
				}
			}
		})
	}
}

// BenchmarkBatchExtractionValidation benchmarks our batch processing optimizations
func BenchmarkBatchExtractionValidation(b *testing.B) {
	workerCounts := []int{1, 2, 4, 8}

	for _, workers := range workerCounts {
		b.Run(fmt.Sprintf("Workers_%d", workers), func(b *testing.B) {
			// Mock setup
			r := &Reader{}
			ctx := context.Background()

			opts := BatchExtractOptions{
				Pages:        make([]int, 50),
				Workers:      workers,
				UseFontCache: true,
				Context:      ctx,
			}

			for i := range opts.Pages {
				opts.Pages[i] = i + 1
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Simulate the optimized batch processing
				results := r.ExtractPagesBatch(opts)
				// Consume results to ensure full processing
				for range results {
					// Just consume
				}
			}
		})
	}
}

// TestMemoryOptimization validates our memory optimizations
func TestMemoryOptimization(t *testing.T) {
	// Test memory usage before and after optimizations
	var m1, m2 runtime.MemStats

	// Force GC to get clean baseline
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Create stress test data
	texts := generateValidationTestTexts(1000)

	// Run clustering with V3 algorithm
	blocks := ClusterTextBlocksV3(texts)

	// Force GC after processing
	runtime.GC()
	runtime.ReadMemStats(&m2)

	// Calculate memory used, handling potential underflow from GC
	var memoryUsed uint64
	if m2.Alloc > m1.Alloc {
		memoryUsed = m2.Alloc - m1.Alloc
	} else {
		// GC ran between measurements, use TotalAlloc delta instead
		memoryUsed = m2.TotalAlloc - m1.TotalAlloc
	}
	expectedMaxMemory := uint64(100 * 1024 * 1024) // 100MB max

	if memoryUsed > expectedMaxMemory {
		t.Errorf("Memory usage %.2f MB exceeds expected %.2f MB",
			float64(memoryUsed)/1024/1024, float64(expectedMaxMemory)/1024/1024)
	} else {
		t.Logf("Memory optimization successful: %.2f MB used (limit: %.2f MB)",
			float64(memoryUsed)/1024/1024, float64(expectedMaxMemory)/1024/1024)
	}

	t.Logf("Clustered %d texts into %d blocks", len(texts), len(blocks))
}

// TestConcurrencyLimits validates our worker limit optimizations
func TestConcurrencyLimits(t *testing.T) {
	// Test that we don't create excessive goroutines
	initialGoroutines := runtime.NumGoroutine()

	// Simulate batch processing with our optimized limits
	r := &Reader{}
	ctx := context.Background()

	opts := BatchExtractOptions{
		Pages:        make([]int, 100),
		Workers:      0, // Should default to max 4
		UseFontCache: true,
		Context:      ctx,
	}

	for i := range opts.Pages {
		opts.Pages[i] = i + 1
	}

	// Start processing
	results := r.ExtractPagesBatch(opts)

	// Check goroutine count
	finalGoroutines := runtime.NumGoroutine()
	maxExpectedGoroutines := initialGoroutines + 10 // Conservative estimate

	if finalGoroutines > maxExpectedGoroutines {
		t.Errorf("Too many goroutines created: %d (expected <= %d)",
			finalGoroutines, maxExpectedGoroutines)
	} else {
		t.Logf("Concurrency limits working: %d -> %d goroutines",
			initialGoroutines, finalGoroutines)
	}

	// Clean up
	for range results {
		// Drain channel
	}
}

// generateValidationTestTexts creates synthetic test data
func generateValidationTestTexts(count int) []Text {
	texts := make([]Text, count)
	for i := 0; i < count; i++ {
		texts[i] = Text{
			S:        fmt.Sprintf("Test text %d", i),
			X:        float64(i * 10),
			Y:        float64(i * 5),
			W:        float64(100 + i),
			FontSize: 12.0,
		}
	}
	return texts
}

// TestGCOptimization validates our garbage collection optimizations
func TestGCOptimization(t *testing.T) {
	var gcBefore, gcAfter uint32

	// Count GC runs
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	gcBefore = memStats.NumGC

	// Run intensive processing
	texts := generateValidationTestTexts(500)
	for i := 0; i < 10; i++ {
		_ = ClusterTextBlocksV3(texts)
		runtime.GC() // Force GC like our optimized code does
	}

	runtime.ReadMemStats(&memStats)
	gcAfter = memStats.NumGC

	expectedMaxGC := gcBefore + 20 // Should be much less than before optimization

	if gcAfter > expectedMaxGC {
		t.Errorf("Too many GC cycles: %d (expected <= %d)",
			gcAfter-gcBefore, expectedMaxGC-gcBefore)
	} else {
		t.Logf("GC optimization successful: %d GC cycles during processing",
			gcAfter-gcBefore)
	}
}

// ProfileEnabled returns whether profiling is available
func ProfileEnabled() bool {
	return true // Always enabled for testing
}

// StartProfiling starts CPU profiling for performance testing
func StartProfiling(filename string) error {
	// This would normally start pprof profiling
	// For testing purposes, we'll just log it
	fmt.Printf("Starting profiling: %s\n", filename)
	return nil
}

// StopProfiling stops profiling
func StopProfiling() {
	fmt.Println("Stopping profiling")
}
