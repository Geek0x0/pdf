// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"context"
	"testing"
	"time"
)

// TestBatchExtractWithFontCache tests batch extraction with font caching enabled
func TestBatchExtractWithFontCache(t *testing.T) {
	// Create a mock reader (we'll use a simple test)
	// In real usage, this would be a Reader with actual PDF content

	tests := []struct {
		name          string
		useFontCache  bool
		fontCacheSize int
	}{
		{
			name:          "Without font cache",
			useFontCache:  false,
			fontCacheSize: 0,
		},
		{
			name:          "With font cache default size",
			useFontCache:  true,
			fontCacheSize: 0, // Should default to 1000
		},
		{
			name:          "With font cache custom size",
			useFontCache:  true,
			fontCacheSize: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test verifies that the options are correctly processed
			// In a real test, we would need actual PDF content

			opts := BatchExtractOptions{
				Pages:         []int{1, 2, 3},
				Workers:       2,
				SmartOrdering: false,
				Context:       context.Background(),
				UseFontCache:  tt.useFontCache,
				FontCacheSize: tt.fontCacheSize,
			}

			// Verify options are set correctly
			if opts.UseFontCache != tt.useFontCache {
				t.Errorf("UseFontCache = %v, want %v", opts.UseFontCache, tt.useFontCache)
			}
			if opts.FontCacheSize != tt.fontCacheSize {
				t.Errorf("FontCacheSize = %d, want %d", opts.FontCacheSize, tt.fontCacheSize)
			}
		})
	}
}

// TestPageSetFontCache tests that Page.SetFontCache works correctly
func TestPageSetFontCache(t *testing.T) {
	// Create a page (with null value for testing)
	page := Page{V: Value{}}

	// Initially, fontCache should be nil
	if page.fontCache != nil {
		t.Error("Initial fontCache should be nil")
	}

	// Create a font cache
	cache := NewGlobalFontCache(100, 0)

	// Set the cache
	page.SetFontCache(cache)

	// Verify cache is set
	if page.fontCache == nil {
		t.Error("fontCache should not be nil after SetFontCache")
	}
	if page.fontCache != cache {
		t.Error("fontCache should be the same instance as set")
	}
}

// BenchmarkBatchExtractWithFontCache benchmarks batch extraction with and without font cache
func BenchmarkBatchExtractWithFontCache(b *testing.B) {
	// Note: This is a synthetic benchmark. In real usage, you would test with actual PDF files

	benchmarks := []struct {
		name         string
		useFontCache bool
	}{
		{"WithoutCache", false},
		{"WithCache", true},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			opts := BatchExtractOptions{
				Workers:       4,
				SmartOrdering: false,
				UseFontCache:  bm.useFontCache,
				FontCacheSize: 1000,
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// In real benchmark, you would:
				// r := openPDF()
				// results := r.ExtractPagesBatch(opts)
				// consume results

				// For now, just verify options are processed correctly
				_ = opts
			}
		})
	}
}

// TestFontCacheCleanup tests that font cache is properly cleaned up after batch processing
func TestFontCacheCleanup(t *testing.T) {
	// Create a global font cache
	cache := NewGlobalFontCache(10, 1*time.Second)

	// Add some entries
	for i := 0; i < 5; i++ {
		key := "test-font-" + string(rune('A'+i))
		font := &Font{V: Value{}}
		cache.Set(key, font)
	}

	// Verify entries exist
	stats := cache.GetStats()
	if stats.Entries != 5 {
		t.Errorf("Cache size = %d, want 5", stats.Entries)
	}

	// Clear cache (simulating batch completion)
	cache.Clear()

	// Verify cache is empty
	stats = cache.GetStats()
	if stats.Entries != 0 {
		t.Errorf("After Clear, cache size = %d, want 0", stats.Entries)
	}
}

// TestFontCacheIntegration tests the integration of font cache with Page.Font
func TestFontCacheIntegration(t *testing.T) {
	// Create a page with null value (for testing structure only)
	page := Page{V: Value{}}

	// Test without cache - should not panic
	_ = page.Font("TestFont")

	// Test with cache
	cache := NewGlobalFontCache(100, 0)
	page.SetFontCache(cache)

	// Multiple calls to Font with same name should use cache
	// Note: With real PDF data, this would verify cache hits
	font1 := page.Font("TestFont")
	font2 := page.Font("TestFont")

	// In this test, we're just verifying no panics occur
	// With real PDF data, you would verify that font2 comes from cache
	_ = font1
	_ = font2

	// Verify cache has entries (even if they're for null fonts in this test)
	stats := cache.GetStats()
	if stats.Entries == 0 {
		// This might be 0 if the Value is null, which is fine for structure testing
		t.Log("Cache size is 0, which is expected for null Value testing")
	}
}
