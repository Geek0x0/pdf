// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"crypto/md5"
	"sync"
	"time"
)

// CFFCacheEntry represents a cached CFF font or decoded result
type CFFCacheEntry struct {
	Data        interface{}
	Expiration  time.Time
	LastAccess  time.Time
	AccessCount int64
}

// IsExpired checks if the cache entry has expired
func (ce *CFFCacheEntry) IsExpired() bool {
	return !ce.Expiration.IsZero() && time.Now().After(ce.Expiration)
}

// CFFCache provides caching for CFF font parsing and decoding operations
type CFFCache struct {
	fonts     map[string]*CFFCacheEntry
	decodings map[string]*CFFCacheEntry
	mutex     sync.RWMutex
	maxSize   int
	ttl       time.Duration
}

// NewCFFCache creates a new CFF cache
func NewCFFCache(maxSize int, ttl time.Duration) *CFFCache {
	cache := &CFFCache{
		fonts:     make(map[string]*CFFCacheEntry),
		decodings: make(map[string]*CFFCacheEntry),
		maxSize:   maxSize,
		ttl:       ttl,
	}

	// Start cleanup goroutine
	go cache.cleanup()

	return cache
}

// GetFont retrieves a cached CFF font
func (cc *CFFCache) GetFont(data []byte) (*CFFFont, bool) {
	key := cc.hashData(data)

	cc.mutex.RLock()
	entry, exists := cc.fonts[key]
	cc.mutex.RUnlock()

	if !exists || entry.IsExpired() {
		return nil, false
	}

	// Update access statistics
	entry.LastAccess = time.Now()
	entry.AccessCount++

	if font, ok := entry.Data.(*CFFFont); ok {
		return font, true
	}

	return nil, false
}

// PutFont caches a CFF font
func (cc *CFFCache) PutFont(data []byte, font *CFFFont) {
	key := cc.hashData(data)

	cc.mutex.Lock()
	defer cc.mutex.Unlock()

	// Evict if at capacity
	if len(cc.fonts) >= cc.maxSize {
		cc.evictOldest(cc.fonts)
	}

	expiration := time.Time{}
	if cc.ttl > 0 {
		expiration = time.Now().Add(cc.ttl)
	}

	cc.fonts[key] = &CFFCacheEntry{
		Data:        font,
		Expiration:  expiration,
		LastAccess:  time.Now(),
		AccessCount: 1,
	}
}

// GetDecoding retrieves cached character string decoding results
func (cc *CFFCache) GetDecoding(data []byte) ([]interface{}, bool) {
	key := cc.hashData(data)

	cc.mutex.RLock()
	entry, exists := cc.decodings[key]
	cc.mutex.RUnlock()

	if !exists || entry.IsExpired() {
		return nil, false
	}

	// Update access statistics
	entry.LastAccess = time.Now()
	entry.AccessCount++

	if commands, ok := entry.Data.([]interface{}); ok {
		return commands, true
	}

	return nil, false
}

// PutDecoding caches character string decoding results
func (cc *CFFCache) PutDecoding(data []byte, commands []interface{}) {
	key := cc.hashData(data)

	cc.mutex.Lock()
	defer cc.mutex.Unlock()

	// Evict if at capacity
	if len(cc.decodings) >= cc.maxSize {
		cc.evictOldest(cc.decodings)
	}

	expiration := time.Time{}
	if cc.ttl > 0 {
		expiration = time.Now().Add(cc.ttl)
	}

	cc.decodings[key] = &CFFCacheEntry{
		Data:        commands,
		Expiration:  expiration,
		LastAccess:  time.Now(),
		AccessCount: 1,
	}
}

// hashData creates a hash key for the given data
func (cc *CFFCache) hashData(data []byte) string {
	hash := md5.Sum(data)
	return string(hash[:])
}

// evictOldest removes the oldest entry from the cache
func (cc *CFFCache) evictOldest(cache map[string]*CFFCacheEntry) {
	var oldestKey string
	var oldestTime time.Time

	for key, entry := range cache {
		if oldestKey == "" || entry.LastAccess.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.LastAccess
		}
	}

	if oldestKey != "" {
		delete(cache, oldestKey)
	}
}

// cleanup periodically removes expired entries
func (cc *CFFCache) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cc.mutex.Lock()
		cc.cleanupExpired(cc.fonts)
		cc.cleanupExpired(cc.decodings)
		cc.mutex.Unlock()
	}
}

// cleanupExpired removes expired entries from a cache map
func (cc *CFFCache) cleanupExpired(cache map[string]*CFFCacheEntry) {
	for key, entry := range cache {
		if entry.IsExpired() {
			delete(cache, key)
		}
	}
}

// Global CFF cache instance
var globalCFFCache *CFFCache

// init initializes the global CFF cache
func init() {
	globalCFFCache = NewCFFCache(100, 30*time.Minute) // Cache up to 100 fonts for 30 minutes
}

// GetGlobalCFFCache returns the global CFF cache instance
func GetGlobalCFFCache() *CFFCache {
	return globalCFFCache
}
