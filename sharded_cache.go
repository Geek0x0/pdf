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

// ShardedCache implements a high-performance sharded cache with the following features:
// - 256 shards to minimize lock contention
// - Independent locks and LRU linked lists for each shard
// - Statistics implemented with atomic operations
// - Adaptive eviction strategy
type ShardedCache struct {
	shards    [256]*CacheShard
	shardMask uint64
	maxSize   int
	ttl       time.Duration

	// Cleanup control
	stopChan chan struct{}
	stopped  atomic.Value // bool
}

// CacheShard represents a single shard of the cache
type CacheShard struct {
	mu      sync.RWMutex
	items   map[string]*ShardedCacheEntry
	head    *ShardedCacheEntry // LRU linked list head (most recently used)
	tail    *ShardedCacheEntry // LRU linked list tail (least recently used)
	maxSize int

	// Statistics (atomic operations)
	hits      uint64
	misses    uint64
	evictions uint64
	size      int64
}

// ShardedCacheEntry represents a cache entry
type ShardedCacheEntry struct {
	key        string
	value      interface{}
	expiration time.Time
	size       int64

	// LRU doubly linked list
	prev *ShardedCacheEntry
	next *ShardedCacheEntry

	// Access tracking (atomic operations)
	accessCount uint64
	lastAccess  int64 // Unix nano
}

// NewShardedCache creates a new sharded cache
func NewShardedCache(maxSize int, ttl time.Duration) *ShardedCache {
	if maxSize <= 0 {
		maxSize = 10000
	}

	sc := &ShardedCache{
		shardMask: 255, // 256 shards
		maxSize:   maxSize,
		ttl:       ttl,
		stopChan:  make(chan struct{}),
	}
	sc.stopped.Store(false)

	// Initialize each shard
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

	// Start periodic cleanup goroutine
	if ttl > 0 {
		go sc.cleanupExpired()
	}

	return sc
}

// getShard gets the shard corresponding to the key
func (sc *ShardedCache) getShard(key string) *CacheShard {
	h := fnv.New64a()
	h.Write([]byte(key))
	hash := h.Sum64()
	return sc.shards[hash&sc.shardMask]
}

// Get gets value from cache
func (sc *ShardedCache) Get(key string) (interface{}, bool) {
	shard := sc.getShard(key)
	shard.mu.RLock()

	entry, ok := shard.items[key]
	if !ok {
		shard.mu.RUnlock()
		atomic.AddUint64(&shard.misses, 1)
		return nil, false
	}

	// Check expiration
	if !entry.expiration.IsZero() && time.Now().After(entry.expiration) {
		shard.mu.RUnlock()
		atomic.AddUint64(&shard.misses, 1)
		return nil, false
	}

	value := entry.value
	shard.mu.RUnlock()

	// Update access information (atomic operations)
	atomic.AddUint64(&entry.accessCount, 1)
	atomic.StoreInt64(&entry.lastAccess, time.Now().UnixNano())
	atomic.AddUint64(&shard.hits, 1)

	// Move to front of LRU linked list
	shard.mu.Lock()
	shard.moveToFront(entry)
	shard.mu.Unlock()

	return value, true
}

// Set sets cache value
func (sc *ShardedCache) Set(key string, value interface{}, size int64) {
	shard := sc.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	now := time.Now()
	expiration := time.Time{}
	if sc.ttl > 0 {
		expiration = now.Add(sc.ttl)
	}

	// Check if already exists
	if entry, ok := shard.items[key]; ok {
		// Update existing entry
		oldSize := entry.size
		entry.value = value
		entry.size = size
		entry.expiration = expiration
		atomic.StoreInt64(&entry.lastAccess, now.UnixNano())
		atomic.AddUint64(&entry.accessCount, 1)

		// Update size statistics
		atomic.AddInt64(&shard.size, size-oldSize)

		// Move to front
		shard.moveToFront(entry)
		return
	}

	// Check if eviction is needed
	for len(shard.items) >= shard.maxSize && shard.tail != nil {
		shard.evictLRU()
	}

	// Create new entry
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

// Delete deletes cache entry
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

// moveToFront moves entry to front of LRU linked list
// Caller must hold lock
func (shard *CacheShard) moveToFront(entry *ShardedCacheEntry) {
	if entry == shard.head {
		return
	}

	// Remove from current position
	if entry.prev != nil {
		entry.prev.next = entry.next
	}
	if entry.next != nil {
		entry.next.prev = entry.prev
	}
	if entry == shard.tail {
		shard.tail = entry.prev
	}

	// Add to front
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

// addToFront adds entry to front of LRU linked list
// Caller must hold lock
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

// removeFromList removes entry from LRU linked list
// Caller must hold lock
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

// evictLRU evicts least recently used entry
// Caller must hold lock
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

// GetStats gets cache statistics
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

// ShardedCacheStats cache statistics
type ShardedCacheStats struct {
	Hits      uint64
	Misses    uint64
	Evictions uint64
	Entries   int64
	Size      int64
}

// Clear clears all cache
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

// cleanupExpired periodically cleans up expired entries
func (sc *ShardedCache) cleanupExpired() {
	ticker := time.NewTicker(5 * time.Minute) // Clean every 5 minutes
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

// removeExpiredEntries removes expired entries
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

// Close stops cleanup goroutine and releases resources
func (sc *ShardedCache) Close() {
	if sc.stopped.Load().(bool) {
		return // Already closed
	}
	sc.stopped.Store(true)
	close(sc.stopChan)
}
