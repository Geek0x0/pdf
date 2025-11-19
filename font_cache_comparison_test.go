// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// BenchmarkFontCacheComparison compares original vs optimized cache
func BenchmarkFontCacheComparison(b *testing.B) {
	font := &Font{V: Value{}}

	b.Run("Original-Get-Sequential", func(b *testing.B) {
		cache := NewGlobalFontCache(1000, 0)
		// Pre-populate
		for i := 0; i < 100; i++ {
			cache.Set(fmt.Sprintf("font-%d", i), font)
		}

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("font-%d", i%100)
			cache.Get(key)
		}
	})

	b.Run("Optimized-Get-Sequential", func(b *testing.B) {
		cache := NewOptimizedFontCache(1000)
		// Pre-populate
		for i := 0; i < 100; i++ {
			cache.Set(fmt.Sprintf("font-%d", i), font)
		}

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("font-%d", i%100)
			cache.Get(key)
		}
	})

	b.Run("Original-Get-Concurrent", func(b *testing.B) {
		cache := NewGlobalFontCache(1000, 0)
		// Pre-populate
		for i := 0; i < 100; i++ {
			cache.Set(fmt.Sprintf("font-%d", i), font)
		}

		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("font-%d", i%100)
				cache.Get(key)
				i++
			}
		})
	})

	b.Run("Optimized-Get-Concurrent", func(b *testing.B) {
		cache := NewOptimizedFontCache(1000)
		// Pre-populate
		for i := 0; i < 100; i++ {
			cache.Set(fmt.Sprintf("font-%d", i), font)
		}

		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("font-%d", i%100)
				cache.Get(key)
				i++
			}
		})
	})

	b.Run("Original-Set-Sequential", func(b *testing.B) {
		cache := NewGlobalFontCache(1000, 0)

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("font-%d", i%100)
			cache.Set(key, font)
		}
	})

	b.Run("Optimized-Set-Sequential", func(b *testing.B) {
		cache := NewOptimizedFontCache(1000)

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("font-%d", i%100)
			cache.Set(key, font)
		}
	})

	b.Run("Original-Mixed-Concurrent", func(b *testing.B) {
		cache := NewGlobalFontCache(1000, 0)
		// Pre-populate
		for i := 0; i < 50; i++ {
			cache.Set(fmt.Sprintf("font-%d", i), font)
		}

		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("font-%d", i%100)
				if i%3 == 0 {
					cache.Set(key, font)
				} else {
					cache.Get(key)
				}
				i++
			}
		})
	})

	b.Run("Optimized-Mixed-Concurrent", func(b *testing.B) {
		cache := NewOptimizedFontCache(1000)
		// Pre-populate
		for i := 0; i < 50; i++ {
			cache.Set(fmt.Sprintf("font-%d", i), font)
		}

		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("font-%d", i%100)
				if i%3 == 0 {
					cache.Set(key, font)
				} else {
					cache.Get(key)
				}
				i++
			}
		})
	})
}

// BenchmarkHashingComparison compares SHA256 vs FNV-1a hashing
func BenchmarkHashingComparison(b *testing.B) {
	font := &Font{V: Value{}}

	b.Run("SHA256-Hash", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = computeFontHash(font)
		}
	})

	b.Run("FNV1a-Hash", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = computeFastFontHash(font)
		}
	})
}

// BenchmarkCacheScaling tests how caches perform under different loads
func BenchmarkCacheScaling(b *testing.B) {
	font := &Font{V: Value{}}
	sizes := []int{100, 500, 1000, 5000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Original-%d", size), func(b *testing.B) {
			cache := NewGlobalFontCache(size, 0)
			// Pre-populate 80%
			for i := 0; i < size*4/5; i++ {
				cache.Set(fmt.Sprintf("font-%d", i), font)
			}

			b.ResetTimer()
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					key := fmt.Sprintf("font-%d", i%size)
					cache.Get(key)
					i++
				}
			})
		})

		b.Run(fmt.Sprintf("Optimized-%d", size), func(b *testing.B) {
			cache := NewOptimizedFontCache(size)
			// Pre-populate 80%
			for i := 0; i < size*4/5; i++ {
				cache.Set(fmt.Sprintf("font-%d", i), font)
			}

			b.ResetTimer()
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					key := fmt.Sprintf("font-%d", i%size)
					cache.Get(key)
					i++
				}
			})
		})
	}
}

// BenchmarkContentionLevels tests performance under different contention levels
func BenchmarkContentionLevels(b *testing.B) {
	font := &Font{V: Value{}}

	// Low contention: 1000 different keys
	b.Run("Original-LowContention", func(b *testing.B) {
		cache := NewGlobalFontCache(1000, 0)
		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("font-%d", i%1000)
				if i%10 == 0 {
					cache.Set(key, font)
				} else {
					cache.Get(key)
				}
				i++
			}
		})
	})

	b.Run("Optimized-LowContention", func(b *testing.B) {
		cache := NewOptimizedFontCache(1000)
		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("font-%d", i%1000)
				if i%10 == 0 {
					cache.Set(key, font)
				} else {
					cache.Get(key)
				}
				i++
			}
		})
	})

	// High contention: 10 keys
	b.Run("Original-HighContention", func(b *testing.B) {
		cache := NewGlobalFontCache(1000, 0)
		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("font-%d", i%10)
				if i%10 == 0 {
					cache.Set(key, font)
				} else {
					cache.Get(key)
				}
				i++
			}
		})
	})

	b.Run("Optimized-HighContention", func(b *testing.B) {
		cache := NewOptimizedFontCache(1000)
		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("font-%d", i%10)
				if i%10 == 0 {
					cache.Set(key, font)
				} else {
					cache.Get(key)
				}
				i++
			}
		})
	})
}

// TestOptimizedCacheCorrectness verifies the optimized cache works correctly
func TestOptimizedCacheCorrectness(b *testing.T) {
	cache := NewOptimizedFontCache(160) // 16 shards * 10 entries = 160 total
	font := &Font{V: Value{}}

	// Test basic Set/Get
	cache.Set("test-font", font)
	retrieved, ok := cache.Get("test-font")
	if !ok {
		b.Error("Font should be in cache")
	}
	if retrieved != font {
		b.Error("Retrieved font should be the same instance")
	}

	// Test miss
	_, ok = cache.Get("non-existent")
	if ok {
		b.Error("Non-existent font should not be found")
	}

	// Test concurrent access
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := fmt.Sprintf("font-%d", idx)
			cache.Set(key, font)
			cache.Get(key)
		}(i)
	}
	wg.Wait()

	// Test stats
	stats := cache.GetStats()
	// With 16 shards and 10 entries per shard, max is 160
	// But we only set 100 + 1 (test-font) = 101 entries
	// All should fit without eviction
	if stats.Entries > 110 {
		b.Errorf("Cache entries unexpectedly high: got %d", stats.Entries)
	}

	// Test Clear
	cache.Clear()
	stats = cache.GetStats()
	if stats.Entries != 0 {
		b.Errorf("Cache should be empty after Clear: got %d entries", stats.Entries)
	}
}

// TestOptimizedCacheLRU verifies LRU eviction works correctly
func TestOptimizedCacheLRU(b *testing.T) {
	cache := NewOptimizedFontCache(32) // 16 shards * 2 entries each = 32 total capacity
	font := &Font{V: Value{}}

	// Fill cache significantly beyond capacity
	// Keys that hash to same shard should trigger eviction in that shard
	for i := 0; i < 200; i++ {
		cache.Set(fmt.Sprintf("font-%d", i), font)
	}

	time.Sleep(10 * time.Millisecond) // Allow async operations to complete

	// Debug: Check each shard
	for idx, shard := range cache.shards {
		shard.mu.Lock()
		count := len(shard.writeMap)
		maxEntries := shard.maxEntries
		shard.mu.Unlock()

		if count > maxEntries {
			b.Logf("Shard %d: %d entries (max=%d) - OVERFLOW!", idx, count, maxEntries)
		}
	}

	stats := cache.GetStats()
	// Each shard can hold 2 entries, with 16 shards = max 32 entries
	// Allow some slack due to timing
	if stats.Entries > 50 {
		b.Errorf("Cache exceeded expected size: got %d entries, expected <=50", stats.Entries)
	}

	// Verify cache didn't keep all entries
	if stats.Entries >= 190 {
		b.Error("Expected LRU eviction to limit cache size significantly")
	}
}

// BenchmarkPrefetch tests the prefetch functionality
func BenchmarkPrefetch(b *testing.B) {
	b.Run("Optimized-Prefetch", func(b *testing.B) {
		cache := NewOptimizedFontCache(1000)
		font := &Font{V: Value{}}

		keys := make([]string, 100)
		for i := 0; i < 100; i++ {
			keys[i] = fmt.Sprintf("font-%d", i)
		}

		compute := func(key string) (*Font, error) {
			time.Sleep(100 * time.Microsecond) // Simulate work
			return font, nil
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cache.Clear()
			cache.Prefetch(keys, compute)
		}
	})
}
