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

func TestGlobalFontCacheBasic(t *testing.T) {
	cache := NewGlobalFontCache(10, 1*time.Hour)

	// Create a mock font
	font := &Font{}

	// Test Set and Get
	cache.Set("font1", font)
	retrieved, ok := cache.Get("font1")

	if !ok {
		t.Error("Expected to find font1 in cache")
	}

	if retrieved != font {
		t.Error("Retrieved font is not the same as stored")
	}
}

func TestGlobalFontCacheLRU(t *testing.T) {
	cache := NewGlobalFontCache(3, 1*time.Hour) // Max 3 entries

	fonts := []*Font{
		{},
		{},
		{},
		{},
	}

	// Add 3 fonts
	cache.Set("font1", fonts[0])
	cache.Set("font2", fonts[1])
	cache.Set("font3", fonts[2])

	stats := cache.GetStats()
	if stats.Entries != 3 {
		t.Errorf("Expected 3 entries, got %d", stats.Entries)
	}

	// Add 4th font, should evict font1 (oldest)
	cache.Set("font4", fonts[3])

	stats = cache.GetStats()
	if stats.Entries != 3 {
		t.Errorf("Expected 3 entries after eviction, got %d", stats.Entries)
	}

	// font1 should be evicted
	_, ok := cache.Get("font1")
	if ok {
		t.Error("font1 should have been evicted")
	}

	// font4 should be present
	_, ok = cache.Get("font4")
	if !ok {
		t.Error("font4 should be in cache")
	}
}

func TestGlobalFontCacheStats(t *testing.T) {
	cache := NewGlobalFontCache(10, 1*time.Hour)

	font := &Font{}
	cache.Set("font1", font)

	// Hit
	cache.Get("font1")
	cache.Get("font1")

	// Miss
	cache.Get("nonexistent")

	stats := cache.GetStats()

	if stats.Hits != 2 {
		t.Errorf("Expected 2 hits, got %d", stats.Hits)
	}

	if stats.Misses != 1 {
		t.Errorf("Expected 1 miss, got %d", stats.Misses)
	}

	expectedHitRate := 2.0 / 3.0
	if stats.HitRate < expectedHitRate-0.01 || stats.HitRate > expectedHitRate+0.01 {
		t.Errorf("Expected hit rate ~%.2f, got %.2f", expectedHitRate, stats.HitRate)
	}
}

func TestGlobalFontCacheExpiration(t *testing.T) {
	cache := NewGlobalFontCache(10, 100*time.Millisecond)

	font := &Font{}
	cache.Set("font1", font)

	// Should be in cache
	_, ok := cache.Get("font1")
	if !ok {
		t.Error("font1 should be in cache")
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should be expired
	_, ok = cache.Get("font1")
	if ok {
		t.Error("font1 should have expired")
	}
}

func TestGlobalFontCacheCleanup(t *testing.T) {
	cache := NewGlobalFontCache(10, 100*time.Millisecond)

	font := &Font{}
	cache.Set("font1", font)
	cache.Set("font2", font)
	cache.Set("font3", font)

	stats := cache.GetStats()
	if stats.Entries != 3 {
		t.Errorf("Expected 3 entries, got %d", stats.Entries)
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Run cleanup
	removed := cache.Cleanup()

	if removed != 3 {
		t.Errorf("Expected to remove 3 entries, removed %d", removed)
	}

	stats = cache.GetStats()
	if stats.Entries != 0 {
		t.Errorf("Expected 0 entries after cleanup, got %d", stats.Entries)
	}
}

func TestGlobalFontCacheConcurrent(t *testing.T) {
	cache := NewGlobalFontCache(100, 1*time.Hour)

	var wg sync.WaitGroup
	numGoroutines := 10
	opsPerGoroutine := 100

	// Concurrent writes and reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			font := &Font{}

			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("font%d-%d", id, j)
				cache.Set(key, font)
				cache.Get(key)
			}
		}(i)
	}

	wg.Wait()

	stats := cache.GetStats()
	if stats.Entries == 0 {
		t.Error("Expected some entries in cache")
	}

	if stats.Hits == 0 {
		t.Error("Expected some cache hits")
	}
}

func TestGlobalFontCacheRemove(t *testing.T) {
	cache := NewGlobalFontCache(10, 1*time.Hour)

	font := &Font{}
	cache.Set("font1", font)

	_, ok := cache.Get("font1")
	if !ok {
		t.Error("font1 should be in cache")
	}

	cache.Remove("font1")

	_, ok = cache.Get("font1")
	if ok {
		t.Error("font1 should have been removed")
	}
}

func TestGlobalFontCacheClear(t *testing.T) {
	cache := NewGlobalFontCache(10, 1*time.Hour)

	font := &Font{}
	cache.Set("font1", font)
	cache.Set("font2", font)
	cache.Set("font3", font)

	stats := cache.GetStats()
	if stats.Entries != 3 {
		t.Errorf("Expected 3 entries, got %d", stats.Entries)
	}

	cache.Clear()

	stats = cache.GetStats()
	if stats.Entries != 0 {
		t.Errorf("Expected 0 entries after clear, got %d", stats.Entries)
	}
}

func TestGetGlobalFontCache(t *testing.T) {
	// Test singleton pattern
	cache1 := GetGlobalFontCache()
	cache2 := GetGlobalFontCache()

	if cache1 != cache2 {
		t.Error("GetGlobalFontCache should return the same instance")
	}
}

func TestFontAccessList(t *testing.T) {
	list := newFontAccessList()

	// Add some keys
	list.touch("key1")
	list.touch("key2")
	list.touch("key3")

	// Oldest should be key1
	oldest, ok := list.oldest()
	if !ok || oldest != "key1" {
		t.Errorf("Expected oldest to be key1, got %s", oldest)
	}

	// Touch key1 to make it most recent
	list.touch("key1")

	// Now oldest should be key2
	oldest, ok = list.oldest()
	if !ok || oldest != "key2" {
		t.Errorf("After touching key1, expected oldest to be key2, got %s", oldest)
	}

	// Remove key2
	list.remove("key2")

	// Now oldest should be key3
	oldest, ok = list.oldest()
	if !ok || oldest != "key3" {
		t.Errorf("After removing key2, expected oldest to be key3, got %s", oldest)
	}
}

func BenchmarkGlobalFontCacheSet(b *testing.B) {
	cache := NewGlobalFontCache(10000, 1*time.Hour)
	font := &Font{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("font%d", i%1000)
		cache.Set(key, font)
	}
}

func BenchmarkGlobalFontCacheGet(b *testing.B) {
	cache := NewGlobalFontCache(10000, 1*time.Hour)
	font := &Font{}

	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		cache.Set(fmt.Sprintf("font%d", i), font)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("font%d", i%1000)
		cache.Get(key)
	}
}

func BenchmarkGlobalFontCacheConcurrent(b *testing.B) {
	cache := NewGlobalFontCache(10000, 1*time.Hour)
	font := &Font{}

	// Pre-populate
	for i := 0; i < 1000; i++ {
		cache.Set(fmt.Sprintf("font%d", i), font)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("font%d", i%1000)
			if i%2 == 0 {
				cache.Get(key)
			} else {
				cache.Set(key, font)
			}
			i++
		}
	})
}

func ExampleGlobalFontCache() {
	// Create a cache with max 100 entries and 1 hour expiration
	cache := NewGlobalFontCache(100, 1*time.Hour)

	// Store a font
	font := &Font{}
	cache.Set("MyFont", font)

	// Retrieve the font
	retrieved, ok := cache.Get("MyFont")
	if ok {
		fmt.Println("Font found in cache")
		_ = retrieved
	}

	// Get statistics
	stats := cache.GetStats()
	fmt.Printf("Cache entries: %d, Hit rate: %.2f%%\n",
		stats.Entries, stats.HitRate*100)
}

func ExampleGetGlobalFontCache() {
	// Get the global singleton instance
	cache := GetGlobalFontCache()

	font := &Font{}
	cache.Set("GlobalFont", font)

	// The same instance can be accessed from anywhere
	sameCacheInstance := GetGlobalFontCache()
	retrieved, _ := sameCacheInstance.Get("GlobalFont")
	_ = retrieved
}
