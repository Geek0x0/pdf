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

func TestShardedCacheBasic(t *testing.T) {
	cache := NewShardedCache(100, time.Minute)

	// 测试基本的 Set 和 Get
	cache.Set("key1", "value1", 10)
	val, ok := cache.Get("key1")
	if !ok || val != "value1" {
		t.Errorf("Expected value1, got %v, ok=%v", val, ok)
	}

	// 测试不存在的键
	_, ok = cache.Get("nonexistent")
	if ok {
		t.Error("Expected false for nonexistent key")
	}
}

func TestShardedCacheLRU(t *testing.T) {
	cache := NewShardedCache(500, 0) // 总共 500 个条目，分片后每个分片约 2 个

	// 填充缓存到超过容量
	for i := 0; i < 1000; i++ {
		cache.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i), 10)
	}

	stats := cache.GetStats()
	if stats.Entries > 500 {
		t.Errorf("Expected at most 500 entries, got %d", stats.Entries)
	}

	// 验证淘汰发生了
	if stats.Evictions == 0 {
		t.Error("Expected evictions to occur")
	}
}

func TestShardedCacheExpiration(t *testing.T) {
	cache := NewShardedCache(100, 50*time.Millisecond)

	cache.Set("key1", "value1", 10)

	// 立即获取应该成功
	val, ok := cache.Get("key1")
	if !ok || val != "value1" {
		t.Error("Expected to get value before expiration")
	}

	// 等待过期
	time.Sleep(100 * time.Millisecond)

	_, ok = cache.Get("key1")
	if ok {
		t.Error("Expected value to be expired")
	}
}

func TestShardedCacheConcurrent(t *testing.T) {
	cache := NewShardedCache(1000, time.Minute)

	var wg sync.WaitGroup
	numGoroutines := 100
	opsPerGoroutine := 100

	// 并发写入
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				cache.Set(key, fmt.Sprintf("value-%d-%d", id, j), 10)
			}
		}(i)
	}
	wg.Wait()

	// 并发读取
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				cache.Get(key)
			}
		}(i)
	}
	wg.Wait()

	stats := cache.GetStats()
	if stats.Entries == 0 {
		t.Error("Expected some entries in cache")
	}
}

func TestShardedCacheDelete(t *testing.T) {
	cache := NewShardedCache(100, 0)

	cache.Set("key1", "value1", 10)
	cache.Set("key2", "value2", 10)

	cache.Delete("key1")

	_, ok := cache.Get("key1")
	if ok {
		t.Error("Expected key1 to be deleted")
	}

	val, ok := cache.Get("key2")
	if !ok || val != "value2" {
		t.Error("Expected key2 to still exist")
	}
}

func TestShardedCacheClear(t *testing.T) {
	cache := NewShardedCache(100, 0)

	for i := 0; i < 10; i++ {
		cache.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i), 10)
	}

	cache.Clear()

	stats := cache.GetStats()
	if stats.Entries != 0 {
		t.Errorf("Expected 0 entries after clear, got %d", stats.Entries)
	}
}

func BenchmarkShardedCacheSet(b *testing.B) {
	cache := NewShardedCache(10000, 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key%d", i%1000)
		cache.Set(key, i, 10)
	}
}

func BenchmarkShardedCacheGet(b *testing.B) {
	cache := NewShardedCache(10000, 0)

	// 预填充
	for i := 0; i < 1000; i++ {
		cache.Set(fmt.Sprintf("key%d", i), i, 10)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key%d", i%1000)
		cache.Get(key)
	}
}

func BenchmarkShardedCacheConcurrent(b *testing.B) {
	cache := NewShardedCache(10000, 0)

	// 预填充
	for i := 0; i < 1000; i++ {
		cache.Set(fmt.Sprintf("key%d", i), i, 10)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key%d", i%1000)
			if i%2 == 0 {
				cache.Get(key)
			} else {
				cache.Set(key, i, 10)
			}
			i++
		}
	})
}
