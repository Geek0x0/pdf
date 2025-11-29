// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"crypto/md5"
	"sync"
	"time"
)

// Type1CacheEntry represents a cached Type1 font
type Type1CacheEntry struct {
	Data        *Type1Font
	Expiration  time.Time
	LastAccess  time.Time
	AccessCount int64
}

// IsExpired checks if the cache entry has expired
func (ce *Type1CacheEntry) IsExpired() bool {
	return !ce.Expiration.IsZero() && time.Now().After(ce.Expiration)
}

// Type1Cache provides caching for Type1 font parsing operations
type Type1Cache struct {
	fonts   map[string]*Type1CacheEntry
	mutex   sync.RWMutex
	maxSize int
	ttl     time.Duration
}

// NewType1Cache creates a new Type1 cache
func NewType1Cache(maxSize int, ttl time.Duration) *Type1Cache {
	cache := &Type1Cache{
		fonts:   make(map[string]*Type1CacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
	}

	// Start cleanup goroutine
	go cache.cleanup()

	return cache
}

// GetFont retrieves a cached Type1 font
func (tc *Type1Cache) GetFont(data []byte) (*Type1Font, bool) {
	key := tc.hashData(data)

	tc.mutex.RLock()
	entry, exists := tc.fonts[key]
	tc.mutex.RUnlock()

	if !exists || entry.IsExpired() {
		return nil, false
	}

	// Update access statistics
	entry.LastAccess = time.Now()
	entry.AccessCount++

	return entry.Data, true
}

// PutFont caches a Type1 font
func (tc *Type1Cache) PutFont(data []byte, font *Type1Font) {
	key := tc.hashData(data)

	tc.mutex.Lock()
	defer tc.mutex.Unlock()

	// Evict if at capacity
	if len(tc.fonts) >= tc.maxSize {
		tc.evictOldest()
	}

	expiration := time.Time{}
	if tc.ttl > 0 {
		expiration = time.Now().Add(tc.ttl)
	}

	tc.fonts[key] = &Type1CacheEntry{
		Data:        font,
		Expiration:  expiration,
		LastAccess:  time.Now(),
		AccessCount: 1,
	}
}

// hashData creates a hash key for the given data
func (tc *Type1Cache) hashData(data []byte) string {
	hash := md5.Sum(data)
	return string(hash[:])
}

// evictOldest removes the oldest entry from the cache
func (tc *Type1Cache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, entry := range tc.fonts {
		if oldestKey == "" || entry.LastAccess.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.LastAccess
		}
	}

	if oldestKey != "" {
		delete(tc.fonts, oldestKey)
	}
}

// cleanup periodically removes expired entries
func (tc *Type1Cache) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		tc.mutex.Lock()
		tc.cleanupExpired()
		tc.mutex.Unlock()
	}
}

// cleanupExpired removes expired entries
func (tc *Type1Cache) cleanupExpired() {
	for key, entry := range tc.fonts {
		if entry.IsExpired() {
			delete(tc.fonts, key)
		}
	}
}

// Global Type1 cache instance
var globalType1Cache *Type1Cache

// init initializes the global Type1 cache
func init() {
	globalType1Cache = NewType1Cache(100, 30*time.Minute) // Cache up to 100 fonts for 30 minutes
}

// GetGlobalType1Cache returns the global Type1 cache instance
func GetGlobalType1Cache() *Type1Cache {
	return globalType1Cache
}
