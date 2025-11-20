// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"fmt"
	"testing"
	"time"
)

func TestFontPrefetcher(t *testing.T) {
	cache := NewOptimizedFontCache(1000)
	prefetcher := NewFontPrefetcher(cache)
	defer prefetcher.Close()

	// 测试基本访问记录
	prefetcher.RecordAccess("font1", []string{"font2", "font3"})
	prefetcher.RecordAccess("font1", []string{"font2", "font3"})

	stats := prefetcher.GetStats()
	if stats.PatternsTracked == 0 {
		t.Error("Expected patterns to be tracked")
	}

	if !stats.Enabled {
		t.Error("Prefetcher should be enabled by default")
	}
}

func TestPrefetcherEnableDisable(t *testing.T) {
	cache := NewOptimizedFontCache(1000)
	prefetcher := NewFontPrefetcher(cache)
	defer prefetcher.Close()

	prefetcher.Disable()
	if prefetcher.isEnabled() {
		t.Error("Prefetcher should be disabled")
	}

	prefetcher.Enable()
	if !prefetcher.isEnabled() {
		t.Error("Prefetcher should be enabled")
	}
}

func TestAccessPatternTracking(t *testing.T) {
	cache := NewOptimizedFontCache(1000)
	prefetcher := NewFontPrefetcher(cache)
	defer prefetcher.Close()

	// 模拟访问模式
	for i := 0; i < 10; i++ {
		prefetcher.RecordAccess("mainFont", []string{"relatedFont1", "relatedFont2"})
		time.Sleep(10 * time.Millisecond)
	}

	stats := prefetcher.GetStats()
	if stats.PatternsTracked == 0 {
		t.Error("Expected at least one pattern to be tracked")
	}

	// 验证访问模式被记录
	prefetcher.accessPattern.mu.RLock()
	pattern, exists := prefetcher.accessPattern.patterns["mainFont"]
	prefetcher.accessPattern.mu.RUnlock()

	if !exists {
		t.Error("Expected mainFont pattern to exist")
	}

	if pattern.accessCount != 10 {
		t.Errorf("Expected 10 accesses, got %d", pattern.accessCount)
	}

	if len(pattern.relatedFonts) != 2 {
		t.Errorf("Expected 2 related fonts, got %d", len(pattern.relatedFonts))
	}
}

func TestPrefetchQueueOperations(t *testing.T) {
	cache := NewOptimizedFontCache(1000)
	prefetcher := NewFontPrefetcher(cache)
	defer prefetcher.Close()

	// 添加一些预取项
	prefetcher.enqueuePrefetch(&PrefetchItem{
		fontKey:  "font1",
		priority: 10.0,
		deadline: time.Now().Add(time.Second),
	})

	prefetcher.enqueuePrefetch(&PrefetchItem{
		fontKey:  "font2",
		priority: 20.0,
		deadline: time.Now().Add(time.Second),
	})

	stats := prefetcher.GetStats()
	if stats.QueueSize != 2 {
		t.Errorf("Expected queue size 2, got %d", stats.QueueSize)
	}
}

func TestClearPatterns(t *testing.T) {
	cache := NewOptimizedFontCache(1000)
	prefetcher := NewFontPrefetcher(cache)
	defer prefetcher.Close()

	// 记录一些访问
	for i := 0; i < 5; i++ {
		prefetcher.RecordAccess(fmt.Sprintf("font%d", i), nil)
	}

	stats := prefetcher.GetStats()
	if stats.PatternsTracked == 0 {
		t.Error("Expected patterns to be tracked")
	}

	// 清除模式
	prefetcher.ClearPatterns()

	stats = prefetcher.GetStats()
	if stats.PatternsTracked != 0 {
		t.Errorf("Expected 0 patterns after clear, got %d", stats.PatternsTracked)
	}
}

func TestPatternMaxSize(t *testing.T) {
	cache := NewOptimizedFontCache(1000)
	prefetcher := NewFontPrefetcher(cache)
	defer prefetcher.Close()
	prefetcher.accessPattern.maxSize = 10

	// 记录超过最大数量的模式
	for i := 0; i < 20; i++ {
		prefetcher.RecordAccess(fmt.Sprintf("font%d", i), nil)
		time.Sleep(time.Millisecond)
	}

	stats := prefetcher.GetStats()
	if stats.PatternsTracked > 10 {
		t.Errorf("Expected at most 10 patterns, got %d", stats.PatternsTracked)
	}
}

func BenchmarkPrefetcherRecordAccess(b *testing.B) {
	cache := NewOptimizedFontCache(10000)
	prefetcher := NewFontPrefetcher(cache)

	relatedFonts := []string{"font1", "font2", "font3"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prefetcher.RecordAccess(fmt.Sprintf("font%d", i%1000), relatedFonts)
	}
}

func BenchmarkPrefetcherConcurrentAccess(b *testing.B) {
	cache := NewOptimizedFontCache(10000)
	prefetcher := NewFontPrefetcher(cache)

	relatedFonts := []string{"font1", "font2", "font3"}

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			prefetcher.RecordAccess(fmt.Sprintf("font%d", i%1000), relatedFonts)
			i++
		}
	})
}
