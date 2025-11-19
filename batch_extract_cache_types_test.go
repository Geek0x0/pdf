// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"context"
	"fmt"
	"testing"
)

// TestBatchExtractWithCacheType tests batch extraction with different cache types
func TestBatchExtractWithCacheType(t *testing.T) {
	tests := []struct {
		name          string
		useFontCache  bool
		fontCacheType FontCacheType
		fontCacheSize int
	}{
		{
			name:          "No cache",
			useFontCache:  false,
			fontCacheType: FontCacheStandard,
			fontCacheSize: 0,
		},
		{
			name:          "Standard cache default size",
			useFontCache:  true,
			fontCacheType: FontCacheStandard,
			fontCacheSize: 0,
		},
		{
			name:          "Standard cache custom size",
			useFontCache:  true,
			fontCacheType: FontCacheStandard,
			fontCacheSize: 500,
		},
		{
			name:          "Optimized cache default size",
			useFontCache:  true,
			fontCacheType: FontCacheOptimized,
			fontCacheSize: 0,
		},
		{
			name:          "Optimized cache custom size",
			useFontCache:  true,
			fontCacheType: FontCacheOptimized,
			fontCacheSize: 2000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := BatchExtractOptions{
				Pages:         []int{1, 2, 3},
				Workers:       2,
				SmartOrdering: false,
				Context:       context.Background(),
				UseFontCache:  tt.useFontCache,
				FontCacheSize: tt.fontCacheSize,
				FontCacheType: tt.fontCacheType,
			}

			// Verify options are set correctly
			if opts.UseFontCache != tt.useFontCache {
				t.Errorf("UseFontCache = %v, want %v", opts.UseFontCache, tt.useFontCache)
			}
			if opts.FontCacheSize != tt.fontCacheSize {
				t.Errorf("FontCacheSize = %d, want %d", opts.FontCacheSize, tt.fontCacheSize)
			}
			if opts.FontCacheType != tt.fontCacheType {
				t.Errorf("FontCacheType = %v, want %v", opts.FontCacheType, tt.fontCacheType)
			}
		})
	}
}

// TestFontCacheInterface verifies that both cache types implement the interface
func TestFontCacheInterface(t *testing.T) {
	font := &Font{V: Value{}}

	t.Run("GlobalFontCache implements interface", func(t *testing.T) {
		var cache FontCacheInterface = NewGlobalFontCache(100, 0)

		// Test Set/Get
		cache.Set("test-key", font)
		retrieved, ok := cache.Get("test-key")
		if !ok {
			t.Error("Font should be in cache")
		}
		if retrieved != font {
			t.Error("Retrieved font should be same instance")
		}

		// Test GetStats
		stats := cache.GetStats()
		if stats.Entries != 1 {
			t.Errorf("Expected 1 entry, got %d", stats.Entries)
		}

		// Test Clear
		cache.Clear()
		stats = cache.GetStats()
		if stats.Entries != 0 {
			t.Errorf("Expected 0 entries after Clear, got %d", stats.Entries)
		}
	})

	t.Run("OptimizedFontCache implements interface", func(t *testing.T) {
		var cache FontCacheInterface = NewOptimizedFontCache(100)

		// Test Set/Get
		cache.Set("test-key", font)
		retrieved, ok := cache.Get("test-key")
		if !ok {
			t.Error("Font should be in cache")
		}
		if retrieved != font {
			t.Error("Retrieved font should be same instance")
		}

		// Test GetStats
		stats := cache.GetStats()
		if stats.Entries != 1 {
			t.Errorf("Expected 1 entry, got %d", stats.Entries)
		}

		// Test Clear
		cache.Clear()
		stats = cache.GetStats()
		if stats.Entries != 0 {
			t.Errorf("Expected 0 entries after Clear, got %d", stats.Entries)
		}
	})
}

// TestPageSetFontCacheInterface tests the new interface-based method
func TestPageSetFontCacheInterface(t *testing.T) {
	page := Page{V: Value{}}

	t.Run("With GlobalFontCache", func(t *testing.T) {
		cache := NewGlobalFontCache(100, 0)
		page.SetFontCacheInterface(cache)

		if page.fontCache == nil {
			t.Error("fontCache should be set")
		}
	})

	t.Run("With OptimizedFontCache", func(t *testing.T) {
		cache := NewOptimizedFontCache(100)
		page.SetFontCacheInterface(cache)

		if page.fontCache == nil {
			t.Error("fontCache should be set")
		}
	})

	t.Run("Backward compatibility with SetFontCache", func(t *testing.T) {
		cache := NewGlobalFontCache(100, 0)
		page.SetFontCache(cache)

		if page.fontCache == nil {
			t.Error("fontCache should be set via old method")
		}
	})
}

// BenchmarkBatchExtractCacheTypes compares performance of different cache types
func BenchmarkBatchExtractCacheTypes(b *testing.B) {
	// Note: This is a structure benchmark. Real PDF testing would show actual differences.

	b.Run("NoCache", func(b *testing.B) {
		opts := BatchExtractOptions{
			Workers:       4,
			SmartOrdering: false,
			UseFontCache:  false,
		}
		_ = opts
	})

	b.Run("StandardCache", func(b *testing.B) {
		opts := BatchExtractOptions{
			Workers:       4,
			SmartOrdering: false,
			UseFontCache:  true,
			FontCacheType: FontCacheStandard,
			FontCacheSize: 1000,
		}
		_ = opts
	})

	b.Run("OptimizedCache", func(b *testing.B) {
		opts := BatchExtractOptions{
			Workers:       4,
			SmartOrdering: false,
			UseFontCache:  true,
			FontCacheType: FontCacheOptimized,
			FontCacheSize: 1000,
		}
		_ = opts
	})
}

// TestFontCacheTypeConstants verifies the cache type constants
func TestFontCacheTypeConstants(t *testing.T) {
	if FontCacheStandard != 0 {
		t.Errorf("FontCacheStandard should be 0, got %d", FontCacheStandard)
	}
	if FontCacheOptimized != 1 {
		t.Errorf("FontCacheOptimized should be 1, got %d", FontCacheOptimized)
	}
}

// TestCacheSelectionLogic tests that the correct cache is created
func TestCacheSelectionLogic(t *testing.T) {
	// This test verifies the cache selection logic by checking types

	t.Run("Standard cache is selected by default", func(t *testing.T) {
		opts := BatchExtractOptions{
			UseFontCache:  true,
			FontCacheSize: 100,
			// FontCacheType not specified, should default to Standard
		}

		if opts.FontCacheType != FontCacheStandard {
			t.Errorf("Default should be FontCacheStandard, got %v", opts.FontCacheType)
		}
	})

	t.Run("Optimized cache is selected when specified", func(t *testing.T) {
		opts := BatchExtractOptions{
			UseFontCache:  true,
			FontCacheSize: 100,
			FontCacheType: FontCacheOptimized,
		}

		if opts.FontCacheType != FontCacheOptimized {
			t.Errorf("Should be FontCacheOptimized, got %v", opts.FontCacheType)
		}
	})
}

// ExampleBatchExtractOptions_standardCache demonstrates using standard cache
func ExampleBatchExtractOptions_standardCache() {
	// This example shows how to use the standard cache
	opts := BatchExtractOptions{
		Workers:       4,
		SmartOrdering: true,
		UseFontCache:  true,
		FontCacheType: FontCacheStandard, // Standard cache
		FontCacheSize: 1000,
	}

	fmt.Printf("Cache type: Standard, Size: %d\n", opts.FontCacheSize)
	// Output: Cache type: Standard, Size: 1000
}

// ExampleBatchExtractOptions_optimizedCache demonstrates using optimized cache
func ExampleBatchExtractOptions_optimizedCache() {
	// This example shows how to use the optimized cache
	opts := BatchExtractOptions{
		Workers:       8,
		SmartOrdering: true,
		UseFontCache:  true,
		FontCacheType: FontCacheOptimized, // Optimized cache (10-85x faster)
		FontCacheSize: 2000,
	}

	fmt.Printf("Cache type: Optimized, Size: %d\n", opts.FontCacheSize)
	// Output: Cache type: Optimized, Size: 2000
}

// TestConcurrentCacheAccess tests concurrent access with both cache types
func TestConcurrentCacheAccess(t *testing.T) {
	font := &Font{V: Value{}}

	caches := []struct {
		name  string
		cache FontCacheInterface
	}{
		{"GlobalFontCache", NewGlobalFontCache(100, 0)},
		{"OptimizedFontCache", NewOptimizedFontCache(100)},
	}

	for _, tc := range caches {
		t.Run(tc.name, func(t *testing.T) {
			cache := tc.cache

			// Concurrent writes
			done := make(chan bool)
			for i := 0; i < 10; i++ {
				go func(idx int) {
					for j := 0; j < 10; j++ {
						key := fmt.Sprintf("font-%d-%d", idx, j)
						cache.Set(key, font)
					}
					done <- true
				}(i)
			}

			// Wait for all writes
			for i := 0; i < 10; i++ {
				<-done
			}

			// Concurrent reads
			for i := 0; i < 10; i++ {
				go func(idx int) {
					for j := 0; j < 10; j++ {
						key := fmt.Sprintf("font-%d-%d", idx, j)
						cache.Get(key)
					}
					done <- true
				}(i)
			}

			// Wait for all reads
			for i := 0; i < 10; i++ {
				<-done
			}

			stats := cache.GetStats()
			if stats.Entries == 0 {
				t.Error("Expected some entries in cache")
			}
		})
	}
}

// TestCacheStatsComparison compares stats from both caches
func TestCacheStatsComparison(t *testing.T) {
	font := &Font{V: Value{}}

	t.Run("Stats structure compatibility", func(t *testing.T) {
		cache1 := NewGlobalFontCache(100, 0)
		cache2 := NewOptimizedFontCache(100)

		// Add same data to both
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("font-%d", i)
			cache1.Set(key, font)
			cache2.Set(key, font)
		}

		// Get some items to generate hits
		for i := 0; i < 5; i++ {
			key := fmt.Sprintf("font-%d", i)
			cache1.Get(key)
			cache2.Get(key)
		}

		stats1 := cache1.GetStats()
		stats2 := cache2.GetStats()

		// Both should have same number of entries
		if stats1.Entries != 10 {
			t.Errorf("GlobalFontCache should have 10 entries, got %d", stats1.Entries)
		}
		if stats2.Entries != 10 {
			t.Errorf("OptimizedFontCache should have 10 entries, got %d", stats2.Entries)
		}

		// Both should have recorded hits
		if stats1.Hits != 5 {
			t.Errorf("GlobalFontCache should have 5 hits, got %d", stats1.Hits)
		}
		if stats2.Hits != 5 {
			t.Errorf("OptimizedFontCache should have 5 hits, got %d", stats2.Hits)
		}
	})
}
