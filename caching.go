// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"context"
	"crypto/md5"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// CacheEntry represents a cached item
type CacheEntry struct {
	Data        interface{}
	Expiration  time.Time
	AccessCount int64
	LastAccess  time.Time
	Size        int64 // Estimated size in bytes
}

// IsExpired checks if the cache entry has expired
func (ce *CacheEntry) IsExpired() bool {
	return !ce.Expiration.IsZero() && time.Now().After(ce.Expiration)
}

// CacheStats provides statistics about cache performance
type CacheStats struct {
	Hits        int64
	Misses      int64
	Evictions   int64
	CurrentSize int64
	MaxSize     int64
	Entries     int64
}

// ResultCache provides caching for parsed and classified results
type ResultCache struct {
	// Cache storage
	items   map[string]*CacheEntry
	mutex   sync.RWMutex
	stats   CacheStats
	maxSize int64
	ttl     time.Duration
	policy  string // "LRU", "LFU", "FIFO"

	// Metrics and monitoring
	hits   int64
	misses int64
}

// NewResultCache creates a new result cache with specified parameters
func NewResultCache(maxSize int64, ttl time.Duration, policy string) *ResultCache {
	if policy == "" {
		policy = "LRU" // Default to LRU
	}

	cache := &ResultCache{
		items:   make(map[string]*CacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
		policy:  policy,
	}

	// Start cleanup goroutine to remove expired entries
	go cache.cleanupExpired()

	return cache
}

// Put adds an item to the cache
func (rc *ResultCache) Put(key string, value interface{}) {
	rc.mutex.Lock()
	defer rc.mutex.Unlock()

	// Calculate entry size (rough estimation)
	size := rc.estimateSize(value)

	// Check if we need to evict items due to size constraints
	for rc.stats.CurrentSize+size > rc.maxSize && len(rc.items) > 0 {
		rc.evictOne()
	}

	// Create new entry
	expiration := time.Time{}
	if rc.ttl > 0 {
		expiration = time.Now().Add(rc.ttl)
	}

	entry := &CacheEntry{
		Data:        value,
		Expiration:  expiration,
		Size:        size,
		AccessCount: 0,
		LastAccess:  time.Now(),
	}

	// Replace existing entry or add new one
	if existing, exists := rc.items[key]; exists {
		rc.stats.CurrentSize -= existing.Size
	}

	rc.items[key] = entry
	rc.stats.CurrentSize += size
	rc.stats.Entries = int64(len(rc.items))
}

// Get retrieves an item from the cache
func (rc *ResultCache) Get(key string) (interface{}, bool) {
	rc.mutex.RLock()
	entry, exists := rc.items[key]
	rc.mutex.RUnlock()

	if !exists {
		atomic.AddInt64(&rc.misses, 1)
		atomic.AddInt64(&rc.stats.Misses, 1)
		return nil, false
	}

	if entry.IsExpired() {
		rc.mutex.Lock()
		rc.remove(key)
		rc.mutex.Unlock()
		atomic.AddInt64(&rc.misses, 1)
		atomic.AddInt64(&rc.stats.Misses, 1)
		return nil, false
	}

	// Update access stats atomically without write lock
	atomic.AddInt64(&entry.AccessCount, 1)
	// Use atomic operation for LastAccess to avoid lock
	now := time.Now()
	entry.LastAccess = now

	atomic.AddInt64(&rc.hits, 1)
	atomic.AddInt64(&rc.stats.Hits, 1)

	return entry.Data, true
}

// Has checks if a key exists in the cache (without updating access stats)
func (rc *ResultCache) Has(key string) bool {
	rc.mutex.RLock()
	entry, exists := rc.items[key]
	rc.mutex.RUnlock()

	if !exists {
		return false
	}

	return !entry.IsExpired()
}

// Remove removes an item from the cache
func (rc *ResultCache) Remove(key string) bool {
	rc.mutex.Lock()
	defer rc.mutex.Unlock()

	return rc.remove(key)
}

// remove is the internal remove function (caller must hold mutex)
func (rc *ResultCache) remove(key string) bool {
	if entry, exists := rc.items[key]; exists {
		delete(rc.items, key)
		rc.stats.CurrentSize -= entry.Size
		rc.stats.Entries = int64(len(rc.items))
		return true
	}
	return false
}

// evictOne removes a single item based on the cache policy
func (rc *ResultCache) evictOne() {
	if len(rc.items) == 0 {
		return
	}

	var keyToRemove string
	var minScore float64 = float64(^uint64(0) >> 1) // Max float64
	var firstKey string

	// Track relevant items based on policy
	for key, entry := range rc.items {
		if keyToRemove == "" {
			firstKey = key
		}

		var score float64
		switch rc.policy {
		case "LRU": // Least Recently Used
			score = float64(entry.LastAccess.Unix())
		case "LFU": // Least Frequently Used
			score = float64(entry.AccessCount)
		case "HYBRID": // Hybrid LRU + LFU
			// Combine recency and frequency: score = access_count / (1 + time_since_last_access)
			timeSinceAccess := time.Since(entry.LastAccess).Seconds()
			score = float64(entry.AccessCount) / (1.0 + timeSinceAccess)
		case "FIFO": // First In, First Out
			score = float64(entry.LastAccess.Unix())
		default: // Default to LRU
			score = float64(entry.LastAccess.Unix())
		}

		if score < minScore {
			minScore = score
			keyToRemove = key
		}
	}

	if keyToRemove == "" {
		keyToRemove = firstKey
	}

	if keyToRemove != "" {
		rc.remove(keyToRemove)
		rc.stats.Evictions++
	}
}

// estimateSize provides a rough estimate of the size of data in bytes
func (rc *ResultCache) estimateSize(data interface{}) int64 {
	switch v := data.(type) {
	case string:
		return int64(len(v))
	case []Text:
		// Rough estimate: 100 bytes per Text element
		return int64(len(v) * 100)
	case []ClassifiedBlock:
		// Rough estimate: 200 bytes per block
		return int64(len(v) * 200)
	case Text:
		return int64(len(v.S) + 64) // text plus metadata
	case Metadata:
		return int64(len(v.Title) + len(v.Author) + len(v.Subject) + len(v.Creator) + len(v.Producer) + len(v.Custom)*50)
	case []byte:
		return int64(len(v))
	default:
		// Conservative estimate for other types
		return 1024
	}
}

// cleanupExpired periodically removes expired entries
func (rc *ResultCache) cleanupExpired() {
	ticker := time.NewTicker(5 * time.Minute) // Clean up every 5 minutes
	defer ticker.Stop()

	for range ticker.C {
		rc.mutex.Lock()
		removed := 0

		for key, entry := range rc.items {
			if entry.IsExpired() {
				rc.remove(key)
				removed++
			}
		}

		rc.mutex.Unlock()

		// Optional: log cleanup stats if needed
		if removed > 0 {
			// In a real implementation, you might want to log this
		}
	}
}

// GetStats returns cache statistics
func (rc *ResultCache) GetStats() CacheStats {
	rc.mutex.RLock()
	defer rc.mutex.RUnlock()

	// Create a copy of stats to avoid race conditions
	stats := rc.stats
	stats.Hits = rc.hits
	stats.Misses = rc.misses
	return stats
}

// Clear removes all items from the cache
func (rc *ResultCache) Clear() {
	rc.mutex.Lock()
	defer rc.mutex.Unlock()

	rc.items = make(map[string]*CacheEntry)
	rc.stats.CurrentSize = 0
	rc.stats.Entries = 0
}

// GetHitRatio returns the cache hit ratio
func (rc *ResultCache) GetHitRatio() float64 {
	rc.mutex.RLock()
	defer rc.mutex.RUnlock()

	total := rc.stats.Hits + rc.stats.Misses
	if total == 0 {
		return 0
	}
	return float64(rc.stats.Hits) / float64(total)
}

// CacheKeyGenerator provides functions to generate cache keys
type CacheKeyGenerator struct{}

// NewCacheKeyGenerator creates a new key generator
func NewCacheKeyGenerator() *CacheKeyGenerator {
	return &CacheKeyGenerator{}
}

// GeneratePageContentKey generates a cache key for page content
func (ckg *CacheKeyGenerator) GeneratePageContentKey(pageNum int, readerHash string) string {
	return fmt.Sprintf("page_content_%s_%d", readerHash, pageNum)
}

// GenerateTextClassificationKey generates a cache key for text classification
func (ckg *CacheKeyGenerator) GenerateTextClassificationKey(pageNum int, readerHash string, processorParams string) string {
	return fmt.Sprintf("text_classification_%s_%d_%s", readerHash, pageNum, processorParams)
}

// GenerateTextOrderingKey generates a cache key for text ordering
func (ckg *CacheKeyGenerator) GenerateTextOrderingKey(pageNum int, readerHash string, orderingParams string) string {
	return fmt.Sprintf("text_ordering_%s_%d_%s", readerHash, pageNum, orderingParams)
}

// GenerateReaderHash generates a hash for the reader object (simplified)
func (ckg *CacheKeyGenerator) GenerateReaderHash(reader *Reader) string {
	// In a real implementation, you'd use a more robust method to identify the reader
	// This is just a placeholder implementation
	return fmt.Sprintf("%p", reader) // Using pointer address as a simple hash
}

// GenerateFullHash generates a hash from arbitrary data
func (ckg *CacheKeyGenerator) GenerateFullHash(data string) string {
	hash := md5.Sum([]byte(data))
	return fmt.Sprintf("%x", hash)
}

// CachedReader wraps a Reader to provide caching functionality
type CachedReader struct {
	*Reader
	cache        *ResultCache
	keyGenerator *CacheKeyGenerator
}

// NewCachedReader creates a new cached reader
func NewCachedReader(reader *Reader, cache *ResultCache) *CachedReader {
	return &CachedReader{
		Reader:       reader,
		cache:        cache,
		keyGenerator: NewCacheKeyGenerator(),
	}
}

// CachedPage returns page content with caching
func (cr *CachedReader) CachedPage(pageNum int) ([]Text, error) {
	key := cr.keyGenerator.GeneratePageContentKey(pageNum, cr.keyGenerator.GenerateReaderHash(cr.Reader))

	if cached, found := cr.cache.Get(key); found {
		if texts, ok := cached.([]Text); ok {
			return texts, nil
		}
	}

	page := cr.Reader.Page(pageNum)
	content := page.Content()

	cr.cache.Put(key, content.Text)
	return content.Text, nil
}

// CachedClassifyTextBlocks returns classified text blocks with caching
func (cr *CachedReader) CachedClassifyTextBlocks(pageNum int) ([]ClassifiedBlock, error) {
	key := cr.keyGenerator.GenerateTextClassificationKey(
		pageNum,
		cr.keyGenerator.GenerateReaderHash(cr.Reader),
		"default_params", // In a real implementation, this would include actual params
	)

	if cached, found := cr.cache.Get(key); found {
		if blocks, ok := cached.([]ClassifiedBlock); ok {
			return blocks, nil
		}
	}

	page := cr.Reader.Page(pageNum)
	blocks, err := page.ClassifyTextBlocks()
	if err != nil {
		return nil, err
	}

	cr.cache.Put(key, blocks)
	return blocks, nil
}

// Global cache for the application
var globalCache *ResultCache
var globalCacheOnce sync.Once

// GetGlobalCache returns a singleton cache instance
func GetGlobalCache() *ResultCache {
	globalCacheOnce.Do(func() {
		// Default to 100MB max size, 1 hour TTL, LRU policy
		globalCache = NewResultCache(100*1024*1024, time.Hour, "LRU")
	})
	return globalCache
}

// CacheManager provides centralized cache management
type CacheManager struct {
	pageCache           *ResultCache
	classificationCache *ResultCache
	textOrderingCache   *ResultCache
	metadataCache       *ResultCache
}

// NewCacheManager creates a new cache manager with separate caches for different data types
func NewCacheManager() *CacheManager {
	return &CacheManager{
		pageCache:           NewResultCache(50*1024*1024, time.Hour, "LRU"),
		classificationCache: NewResultCache(30*1024*1024, 2*time.Hour, "LRU"),
		textOrderingCache:   NewResultCache(20*1024*1024, 30*time.Minute, "LRU"),
		metadataCache:       NewResultCache(5*1024*1024, 24*time.Hour, "FIFO"),
	}
}

// GetPageCache returns the page content cache
func (cm *CacheManager) GetPageCache() *ResultCache {
	return cm.pageCache
}

// GetClassificationCache returns the classification cache
func (cm *CacheManager) GetClassificationCache() *ResultCache {
	return cm.classificationCache
}

// GetTextOrderingCache returns the text ordering cache
func (cm *CacheManager) GetTextOrderingCache() *ResultCache {
	return cm.textOrderingCache
}

// GetMetadataCache returns the metadata cache
func (cm *CacheManager) GetMetadataCache() *ResultCache {
	return cm.metadataCache
}

// GetTotalStats returns combined statistics for all caches
func (cm *CacheManager) GetTotalStats() CacheStats {
	pageStats := cm.pageCache.GetStats()
	classStats := cm.classificationCache.GetStats()
	orderStats := cm.textOrderingCache.GetStats()
	metaStats := cm.metadataCache.GetStats()

	return CacheStats{
		Hits:        pageStats.Hits + classStats.Hits + orderStats.Hits + metaStats.Hits,
		Misses:      pageStats.Misses + classStats.Misses + orderStats.Misses + metaStats.Misses,
		Evictions:   pageStats.Evictions + classStats.Evictions + orderStats.Evictions + metaStats.Evictions,
		CurrentSize: pageStats.CurrentSize + classStats.CurrentSize + orderStats.CurrentSize + metaStats.CurrentSize,
		MaxSize:     pageStats.MaxSize + classStats.MaxSize + orderStats.MaxSize + metaStats.MaxSize,
		Entries:     pageStats.Entries + classStats.Entries + orderStats.Entries + metaStats.Entries,
	}
}

// CacheContext provides a context-aware cache with automatic cleanup
type CacheContext struct {
	cache  *ResultCache
	ctx    context.Context
	cancel context.CancelFunc
}

// NewCacheContext creates a new context-aware cache
func NewCacheContext(parent context.Context, cache *ResultCache) *CacheContext {
	ctx, cancel := context.WithCancel(parent)

	cc := &CacheContext{
		cache:  cache,
		ctx:    ctx,
		cancel: cancel,
	}

	// Set up cleanup when context is done
	go func() {
		<-cc.ctx.Done()
		// Perform any necessary cleanup
	}()

	return cc
}

// GetWithTimeout gets a value with timeout
func (cc *CacheContext) GetWithTimeout(key string, timeout time.Duration) (interface{}, bool, error) {
	done := make(chan struct{})
	var result interface{}
	var found bool

	// Launch the get operation in a goroutine
	go func() {
		defer close(done)
		result, found = cc.cache.Get(key)
	}()

	// Wait for either the operation to complete or timeout
	select {
	case <-done:
		return result, found, nil
	case <-time.After(timeout):
		return nil, false, context.DeadlineExceeded
	case <-cc.ctx.Done():
		return nil, false, cc.ctx.Err()
	}
}

// Close releases resources used by the cache context
func (cc *CacheContext) Close() {
	cc.cancel()
}

// ConnectionPool manages a pool of connections/resources
type ConnectionPool struct {
	pool    chan interface{}
	new     func() interface{}
	close   func(interface{})
	maxSize int
	mu      sync.Mutex
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool(maxSize int, newFunc func() interface{}, closeFunc func(interface{})) *ConnectionPool {
	return &ConnectionPool{
		pool:    make(chan interface{}, maxSize),
		new:     newFunc,
		close:   closeFunc,
		maxSize: maxSize,
	}
}

// Get retrieves a connection from the pool
func (cp *ConnectionPool) Get() interface{} {
	select {
	case conn := <-cp.pool:
		return conn
	default:
		return cp.new()
	}
}

// Put returns a connection to the pool
func (cp *ConnectionPool) Put(conn interface{}) {
	select {
	case cp.pool <- conn:
	default:
		// Pool is full, close the connection
		if cp.close != nil {
			cp.close(conn)
		}
	}
}

// Close closes all connections in the pool
func (cp *ConnectionPool) Close() {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	close(cp.pool)
	for conn := range cp.pool {
		if cp.close != nil {
			cp.close(conn)
		}
	}
}
