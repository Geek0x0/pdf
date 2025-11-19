// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"testing"
)

// BenchmarkPageFontWithCache benchmarks font access with and without cache
func BenchmarkPageFontWithCache(b *testing.B) {
	// Create a mock page with null value (for structure testing)
	page := Page{V: Value{}}
	fontName := "TestFont"

	b.Run("WithoutCache", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = page.Font(fontName)
		}
	})

	b.Run("WithCache", func(b *testing.B) {
		// Create and set cache
		cache := NewGlobalFontCache(100, 0)
		page.SetFontCache(cache)

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = page.Font(fontName)
		}
	})
}

// BenchmarkBatchExtractOptionsSetup benchmarks the overhead of setting up batch options
func BenchmarkBatchExtractOptionsSetup(b *testing.B) {
	b.Run("WithoutFontCache", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			opts := BatchExtractOptions{
				Workers:       4,
				SmartOrdering: true,
				UseFontCache:  false,
			}
			_ = opts
		}
	})

	b.Run("WithFontCache", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			opts := BatchExtractOptions{
				Workers:       4,
				SmartOrdering: true,
				UseFontCache:  true,
				FontCacheSize: 1000,
			}
			_ = opts
		}
	})
}

// BenchmarkFontCacheOperations benchmarks cache operations
func BenchmarkFontCacheOperations(b *testing.B) {
	cache := NewGlobalFontCache(1000, 0)
	font := &Font{V: Value{}}

	b.Run("Set", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			key := "font-" + string(rune('A'+i%26))
			cache.Set(key, font)
		}
	})

	// Pre-populate cache for Get benchmark
	for i := 0; i < 26; i++ {
		key := "font-" + string(rune('A'+i))
		cache.Set(key, font)
	}

	b.Run("Get-Hit", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			key := "font-" + string(rune('A'+i%26))
			_, _ = cache.Get(key)
		}
	})

	b.Run("Get-Miss", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			key := "missing-font-" + string(rune('A'+i%26))
			_, _ = cache.Get(key)
		}
	})
}

// BenchmarkFontCacheScalability tests cache performance with different sizes
func BenchmarkFontCacheScalability(b *testing.B) {
	sizes := []int{100, 500, 1000, 2000, 5000}
	font := &Font{V: Value{}}

	for _, size := range sizes {
		b.Run(formatInt(size), func(b *testing.B) {
			cache := NewGlobalFontCache(size, 0)

			// Pre-populate half the cache
			for i := 0; i < size/2; i++ {
				cache.Set(formatInt(i), font)
			}

			b.ResetTimer()
			b.ReportAllocs()

			// Mix of Get and Set operations
			for i := 0; i < b.N; i++ {
				if i%2 == 0 {
					cache.Set(formatInt(i%size), font)
				} else {
					_, _ = cache.Get(formatInt(i % size))
				}
			}
		})
	}
}

// Helper function to format int as string
func formatInt(n int) string {
	if n == 0 {
		return "0"
	}

	// Simple int to string conversion
	neg := n < 0
	if neg {
		n = -n
	}

	buf := make([]byte, 0, 20)
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}

	if neg {
		buf = append(buf, '-')
	}

	// Reverse
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}

	return string(buf)
}
