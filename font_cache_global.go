// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// GlobalFontCache implements an enhanced global font cache with:
// - LRU eviction for memory control
// - Hit/miss statistics for monitoring
// - Content-based hashing for accurate cache keys
type GlobalFontCache struct {
	mu sync.RWMutex

	// Main cache storage
	fonts map[string]*cachedFont

	// LRU tracking
	accessList *fontAccessList

	// Configuration
	maxEntries int
	maxAge     time.Duration

	// Statistics
	hits   uint64
	misses uint64
}

// cachedFont wraps a Font with metadata
type cachedFont struct {
	font        *Font
	key         string
	hash        string
	lastAccess  time.Time
	accessCount uint64
}

// fontAccessList implements a simple LRU list with O(1) operations
type fontAccessList struct {
	mu    sync.Mutex
	head  *lruNode            // Most recently used
	tail  *lruNode            // Least recently used
	index map[string]*lruNode // key -> node
	size  int
}

// lruNode represents a node in the doubly-linked list
type lruNode struct {
	key  string
	prev *lruNode
	next *lruNode
}

// newFontAccessList creates a new access list
func newFontAccessList() *fontAccessList {
	return &fontAccessList{
		index: make(map[string]*lruNode),
	}
}

// touch marks a key as recently accessed - O(1) operation
func (fal *fontAccessList) touch(key string) {
	fal.mu.Lock()
	defer fal.mu.Unlock()

	// If already exists, move to front
	if node, exists := fal.index[key]; exists {
		// Remove from current position
		fal.removeNode(node)
		// Add to front
		fal.addToFront(node)
		return
	}

	// Create new node and add to front
	node := &lruNode{key: key}
	fal.index[key] = node
	fal.addToFront(node)
	fal.size++
}

// addToFront adds a node to the front of the list
func (fal *fontAccessList) addToFront(node *lruNode) {
	node.next = fal.head
	node.prev = nil

	if fal.head != nil {
		fal.head.prev = node
	}
	fal.head = node

	if fal.tail == nil {
		fal.tail = node
	}
}

// removeNode removes a node from the list
func (fal *fontAccessList) removeNode(node *lruNode) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		fal.head = node.next
	}

	if node.next != nil {
		node.next.prev = node.prev
	} else {
		fal.tail = node.prev
	}
}

// oldest returns the oldest key (for eviction) - O(1) operation
func (fal *fontAccessList) oldest() (string, bool) {
	fal.mu.Lock()
	defer fal.mu.Unlock()

	if fal.tail == nil {
		return "", false
	}

	return fal.tail.key, true
}

// remove removes a key from tracking - O(1) operation
func (fal *fontAccessList) remove(key string) {
	fal.mu.Lock()
	defer fal.mu.Unlock()

	node, exists := fal.index[key]
	if !exists {
		return
	}

	fal.removeNode(node)
	delete(fal.index, key)
	fal.size--
}

// Global font cache instance
var (
	globalEnhancedFontCache     *GlobalFontCache
	globalEnhancedFontCacheOnce sync.Once
)

// GetGlobalFontCache returns the global font cache instance
func GetGlobalFontCache() *GlobalFontCache {
	globalEnhancedFontCacheOnce.Do(func() {
		globalEnhancedFontCache = NewGlobalFontCache(1000, 1*time.Hour)
	})
	return globalEnhancedFontCache
}

// NewGlobalFontCache creates a new global font cache
func NewGlobalFontCache(maxEntries int, maxAge time.Duration) *GlobalFontCache {
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	if maxAge <= 0 {
		maxAge = 1 * time.Hour
	}

	return &GlobalFontCache{
		fonts:      make(map[string]*cachedFont),
		accessList: newFontAccessList(),
		maxEntries: maxEntries,
		maxAge:     maxAge,
	}
}

// computeFontHash computes a content-based hash for a font
func computeFontHash(f *Font) string {
	if f == nil {
		return ""
	}

	// Hash based on font properties that define uniqueness
	hasher := sha256.New()

	// Include base font name
	hasher.Write([]byte(f.BaseFont()))

	// Include subtype
	hasher.Write([]byte(f.subtype()))

	// Include first/last char
	hasher.Write([]byte{byte(f.FirstChar()), byte(f.LastChar())})

	// Include a sample of widths (first 10)
	widths := f.Widths()
	sampleSize := 10
	if len(widths) < sampleSize {
		sampleSize = len(widths)
	}
	for i := 0; i < sampleSize; i++ {
		// Convert float to bytes
		w := widths[i]
		bytes := [8]byte{}
		for j := 0; j < 8; j++ {
			bytes[j] = byte(uint64(w*1000) >> (j * 8))
		}
		hasher.Write(bytes[:])
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

// Get retrieves a font from the cache
func (gfc *GlobalFontCache) Get(key string) (*Font, bool) {
	gfc.mu.RLock()
	cached, ok := gfc.fonts[key]
	gfc.mu.RUnlock()

	if !ok {
		gfc.mu.Lock()
		gfc.misses++
		gfc.mu.Unlock()
		return nil, false
	}

	// Check age
	if time.Since(cached.lastAccess) > gfc.maxAge {
		// Too old, remove it
		gfc.Remove(key)
		gfc.mu.Lock()
		gfc.misses++
		gfc.mu.Unlock()
		return nil, false
	}

	// Update access info
	gfc.mu.Lock()
	cached.lastAccess = time.Now()
	cached.accessCount++
	gfc.hits++
	gfc.mu.Unlock()

	// Update LRU
	gfc.accessList.touch(key)

	return cached.font, true
}

// Set stores a font in the cache
func (gfc *GlobalFontCache) Set(key string, font *Font) {
	if font == nil {
		return
	}

	hash := computeFontHash(font)

	gfc.mu.Lock()
	defer gfc.mu.Unlock()

	// Check if already exists
	if _, ok := gfc.fonts[key]; ok {
		// Update existing
		gfc.fonts[key].font = font
		gfc.fonts[key].hash = hash
		gfc.fonts[key].lastAccess = time.Now()
		gfc.fonts[key].accessCount++
		gfc.accessList.touch(key)
		return
	}

	// Evict if at capacity
	if len(gfc.fonts) >= gfc.maxEntries {
		gfc.evictOldest()
	}

	// Add new entry
	gfc.fonts[key] = &cachedFont{
		font:        font,
		key:         key,
		hash:        hash,
		lastAccess:  time.Now(),
		accessCount: 1,
	}

	gfc.accessList.touch(key)
}

// evictOldest removes the least recently used font
// Caller must hold write lock
func (gfc *GlobalFontCache) evictOldest() {
	oldestKey, ok := gfc.accessList.oldest()
	if !ok {
		return
	}

	delete(gfc.fonts, oldestKey)
	gfc.accessList.remove(oldestKey)
}

// Remove removes a font from the cache
func (gfc *GlobalFontCache) Remove(key string) {
	gfc.mu.Lock()
	defer gfc.mu.Unlock()

	delete(gfc.fonts, key)
	gfc.accessList.remove(key)
}

// Clear removes all fonts from the cache
func (gfc *GlobalFontCache) Clear() {
	gfc.mu.Lock()
	defer gfc.mu.Unlock()

	gfc.fonts = make(map[string]*cachedFont)
	gfc.accessList = newFontAccessList()
}

// Stats returns cache statistics
type FontCacheStats struct {
	Entries     int
	MaxEntries  int
	Hits        uint64
	Misses      uint64
	HitRate     float64
	AvgAccesses float64
}

// GetStats returns current cache statistics
func (gfc *GlobalFontCache) GetStats() FontCacheStats {
	gfc.mu.RLock()
	defer gfc.mu.RUnlock()

	total := gfc.hits + gfc.misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(gfc.hits) / float64(total)
	}

	// Calculate average accesses
	totalAccesses := uint64(0)
	for _, cached := range gfc.fonts {
		totalAccesses += cached.accessCount
	}
	avgAccesses := 0.0
	if len(gfc.fonts) > 0 {
		avgAccesses = float64(totalAccesses) / float64(len(gfc.fonts))
	}

	return FontCacheStats{
		Entries:     len(gfc.fonts),
		MaxEntries:  gfc.maxEntries,
		Hits:        gfc.hits,
		Misses:      gfc.misses,
		HitRate:     hitRate,
		AvgAccesses: avgAccesses,
	}
}

// Cleanup removes expired entries
func (gfc *GlobalFontCache) Cleanup() int {
	gfc.mu.Lock()
	defer gfc.mu.Unlock()

	now := time.Now()
	removed := 0

	keysToRemove := make([]string, 0)
	for key, cached := range gfc.fonts {
		if now.Sub(cached.lastAccess) > gfc.maxAge {
			keysToRemove = append(keysToRemove, key)
		}
	}

	for _, key := range keysToRemove {
		delete(gfc.fonts, key)
		gfc.accessList.remove(key)
		removed++
	}

	return removed
}

// StartCleanupRoutine starts a background goroutine to periodically clean up expired entries
func (gfc *GlobalFontCache) StartCleanupRoutine(interval time.Duration) chan struct{} {
	stop := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				gfc.Cleanup()
			case <-stop:
				return
			}
		}
	}()

	return stop
}

// GetOrCompute retrieves a font from cache or computes it if not present
// This is a convenience function that combines Get and Set
func (gfc *GlobalFontCache) GetOrCompute(key string, compute func() (*Font, error)) (*Font, error) {
	// Try to get from cache first
	if font, ok := gfc.Get(key); ok {
		return font, nil
	}

	// Compute the font
	font, err := compute()
	if err != nil {
		return nil, err
	}

	// Store in cache
	gfc.Set(key, font)

	return font, nil
}
