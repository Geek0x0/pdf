// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"
)

// ShardedCache 实现了一个高性能分片缓存，具有以下特性：
// - 256 个分片以最小化锁竞争
// - 每个分片独立的锁和 LRU 链表
// - 原子操作实现的统计信息
// - 自适应淘汰策略
type ShardedCache struct {
	shards    [256]*CacheShard
	shardMask uint64
	maxSize   int
	ttl       time.Duration

	// 清理控制
	stopChan chan struct{}
	stopped  atomic.Value // bool
}

// CacheShard 代表缓存的单个分片
type CacheShard struct {
	mu      sync.RWMutex
	items   map[string]*ShardedCacheEntry
	head    *ShardedCacheEntry // LRU 链表头（最近使用）
	tail    *ShardedCacheEntry // LRU 链表尾（最久未使用）
	maxSize int

	// 统计信息（原子操作）
	hits      uint64
	misses    uint64
	evictions uint64
	size      int64
}

// ShardedCacheEntry 代表缓存条目
type ShardedCacheEntry struct {
	key        string
	value      interface{}
	expiration time.Time
	size       int64

	// LRU 双向链表
	prev *ShardedCacheEntry
	next *ShardedCacheEntry

	// 访问跟踪（原子操作）
	accessCount uint64
	lastAccess  int64 // Unix nano
}

// NewShardedCache 创建新的分片缓存
func NewShardedCache(maxSize int, ttl time.Duration) *ShardedCache {
	if maxSize <= 0 {
		maxSize = 10000
	}

	sc := &ShardedCache{
		shardMask: 255, // 256 个分片
		maxSize:   maxSize,
		ttl:       ttl,
		stopChan:  make(chan struct{}),
	}
	sc.stopped.Store(false)

	// 初始化每个分片
	sizePerShard := maxSize / 256
	if sizePerShard < 1 {
		sizePerShard = 1
	}

	for i := 0; i < 256; i++ {
		sc.shards[i] = &CacheShard{
			items:   make(map[string]*ShardedCacheEntry, sizePerShard),
			maxSize: sizePerShard,
		}
	}

	// 启动定期清理 goroutine
	if ttl > 0 {
		go sc.cleanupExpired()
	}

	return sc
}

// getShard 获取键对应的分片
func (sc *ShardedCache) getShard(key string) *CacheShard {
	h := fnv.New64a()
	h.Write([]byte(key))
	hash := h.Sum64()
	return sc.shards[hash&sc.shardMask]
}

// Get 从缓存获取值
func (sc *ShardedCache) Get(key string) (interface{}, bool) {
	shard := sc.getShard(key)
	shard.mu.RLock()

	entry, ok := shard.items[key]
	if !ok {
		shard.mu.RUnlock()
		atomic.AddUint64(&shard.misses, 1)
		return nil, false
	}

	// 检查过期
	if !entry.expiration.IsZero() && time.Now().After(entry.expiration) {
		shard.mu.RUnlock()
		atomic.AddUint64(&shard.misses, 1)
		return nil, false
	}

	value := entry.value
	shard.mu.RUnlock()

	// 更新访问信息（原子操作）
	atomic.AddUint64(&entry.accessCount, 1)
	atomic.StoreInt64(&entry.lastAccess, time.Now().UnixNano())
	atomic.AddUint64(&shard.hits, 1)

	// 移到 LRU 链表前端
	shard.mu.Lock()
	shard.moveToFront(entry)
	shard.mu.Unlock()

	return value, true
}

// Set 设置缓存值
func (sc *ShardedCache) Set(key string, value interface{}, size int64) {
	shard := sc.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	now := time.Now()
	expiration := time.Time{}
	if sc.ttl > 0 {
		expiration = now.Add(sc.ttl)
	}

	// 检查是否已存在
	if entry, ok := shard.items[key]; ok {
		// 更新现有条目
		oldSize := entry.size
		entry.value = value
		entry.size = size
		entry.expiration = expiration
		atomic.StoreInt64(&entry.lastAccess, now.UnixNano())
		atomic.AddUint64(&entry.accessCount, 1)

		// 更新大小统计
		atomic.AddInt64(&shard.size, size-oldSize)

		// 移到前端
		shard.moveToFront(entry)
		return
	}

	// 检查是否需要淘汰
	for len(shard.items) >= shard.maxSize && shard.tail != nil {
		shard.evictLRU()
	}

	// 创建新条目
	entry := &ShardedCacheEntry{
		key:        key,
		value:      value,
		size:       size,
		expiration: expiration,
	}
	atomic.StoreInt64(&entry.lastAccess, now.UnixNano())
	atomic.StoreUint64(&entry.accessCount, 1)

	shard.items[key] = entry
	shard.addToFront(entry)
	atomic.AddInt64(&shard.size, size)
}

// Delete 删除缓存条目
func (sc *ShardedCache) Delete(key string) {
	shard := sc.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	entry, ok := shard.items[key]
	if !ok {
		return
	}

	delete(shard.items, key)
	shard.removeFromList(entry)
	atomic.AddInt64(&shard.size, -entry.size)
}

// moveToFront 将条目移到 LRU 链表前端
// 调用者必须持有锁
func (shard *CacheShard) moveToFront(entry *ShardedCacheEntry) {
	if entry == shard.head {
		return
	}

	// 从当前位置移除
	if entry.prev != nil {
		entry.prev.next = entry.next
	}
	if entry.next != nil {
		entry.next.prev = entry.prev
	}
	if entry == shard.tail {
		shard.tail = entry.prev
	}

	// 添加到前端
	entry.prev = nil
	entry.next = shard.head
	if shard.head != nil {
		shard.head.prev = entry
	}
	shard.head = entry

	if shard.tail == nil {
		shard.tail = entry
	}
}

// addToFront 将条目添加到 LRU 链表前端
// 调用者必须持有锁
func (shard *CacheShard) addToFront(entry *ShardedCacheEntry) {
	entry.prev = nil
	entry.next = shard.head

	if shard.head != nil {
		shard.head.prev = entry
	}
	shard.head = entry

	if shard.tail == nil {
		shard.tail = entry
	}
}

// removeFromList 从 LRU 链表中移除条目
// 调用者必须持有锁
func (shard *CacheShard) removeFromList(entry *ShardedCacheEntry) {
	if entry.prev != nil {
		entry.prev.next = entry.next
	}
	if entry.next != nil {
		entry.next.prev = entry.prev
	}
	if entry == shard.head {
		shard.head = entry.next
	}
	if entry == shard.tail {
		shard.tail = entry.prev
	}

	entry.prev = nil
	entry.next = nil
}

// evictLRU 淘汰最久未使用的条目
// 调用者必须持有锁
func (shard *CacheShard) evictLRU() {
	if shard.tail == nil {
		return
	}

	entry := shard.tail
	delete(shard.items, entry.key)
	shard.removeFromList(entry)
	atomic.AddInt64(&shard.size, -entry.size)
	atomic.AddUint64(&shard.evictions, 1)
}

// GetStats 获取缓存统计信息
func (sc *ShardedCache) GetStats() ShardedCacheStats {
	var stats ShardedCacheStats

	for i := 0; i < 256; i++ {
		shard := sc.shards[i]
		stats.Hits += atomic.LoadUint64(&shard.hits)
		stats.Misses += atomic.LoadUint64(&shard.misses)
		stats.Evictions += atomic.LoadUint64(&shard.evictions)
		stats.Size += atomic.LoadInt64(&shard.size)

		shard.mu.RLock()
		stats.Entries += int64(len(shard.items))
		shard.mu.RUnlock()
	}

	return stats
}

// ShardedCacheStats 缓存统计信息
type ShardedCacheStats struct {
	Hits      uint64
	Misses    uint64
	Evictions uint64
	Entries   int64
	Size      int64
}

// Clear 清空所有缓存
func (sc *ShardedCache) Clear() {
	for i := 0; i < 256; i++ {
		shard := sc.shards[i]
		shard.mu.Lock()
		shard.items = make(map[string]*ShardedCacheEntry)
		shard.head = nil
		shard.tail = nil
		atomic.StoreInt64(&shard.size, 0)
		shard.mu.Unlock()
	}
}

// cleanupExpired 定期清理过期条目
func (sc *ShardedCache) cleanupExpired() {
	ticker := time.NewTicker(5 * time.Minute) // 每5分钟清理一次
	defer ticker.Stop()

	for {
		select {
		case <-sc.stopChan:
			return
		case <-ticker.C:
			if sc.stopped.Load().(bool) {
				return
			}
			sc.removeExpiredEntries()
		}
	}
}

// removeExpiredEntries 移除过期条目
func (sc *ShardedCache) removeExpiredEntries() {
	now := time.Now()
	for i := 0; i < 256; i++ {
		shard := sc.shards[i]
		shard.mu.Lock()

		var toDelete []string
		for key, entry := range shard.items {
			if !entry.expiration.IsZero() && now.After(entry.expiration) {
				toDelete = append(toDelete, key)
			}
		}

		for _, key := range toDelete {
			if entry, ok := shard.items[key]; ok {
				delete(shard.items, key)
				shard.removeFromList(entry)
				atomic.AddInt64(&shard.size, -entry.size)
				atomic.AddUint64(&shard.evictions, 1)
			}
		}

		shard.mu.Unlock()
	}
}

// Close 停止清理 goroutine 并释放资源
func (sc *ShardedCache) Close() {
	if sc.stopped.Load().(bool) {
		return // 已经关闭
	}
	sc.stopped.Store(true)
	close(sc.stopChan)
}
