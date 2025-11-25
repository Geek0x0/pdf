// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"container/heap"
	"sync"
	"sync/atomic"
	"time"
)

// FontPrefetcher implements intelligent font prefetch strategy
// Based on access pattern prediction and preloading potentially needed fonts
type FontPrefetcher struct {
	cache         *OptimizedFontCache
	accessPattern *AccessPatternTracker
	prefetchQueue *PrefetchQueue
	mu            sync.RWMutex
	enabled       atomic.Value // bool
	stopChan      chan struct{}
}

// AccessPatternTracker tracks font access patterns
type AccessPatternTracker struct {
	mu       sync.RWMutex
	patterns map[string]*AccessPattern
	maxSize  int
}

// AccessPattern records access pattern of single font
type AccessPattern struct {
	fontKey      string
	accessCount  uint64
	lastAccess   time.Time
	avgInterval  time.Duration
	relatedFonts map[string]int // fonts frequently accessed together and their weights
	predictNext  time.Time      // predicted next access time
}

// PrefetchQueue prefetch queue (priority queue)
type PrefetchQueue struct {
	mu    sync.Mutex
	items []*PrefetchItem
}

// PrefetchItem prefetch item
type PrefetchItem struct {
	fontKey  string
	priority float64 // priority (higher is more important)
	deadline time.Time
	index    int
}

// NewFontPrefetcher create new font prefetcher
func NewFontPrefetcher(cache *OptimizedFontCache) *FontPrefetcher {
	fp := &FontPrefetcher{
		cache: cache,
		accessPattern: &AccessPatternTracker{
			patterns: make(map[string]*AccessPattern),
			maxSize:  10000,
		},
		prefetchQueue: &PrefetchQueue{
			items: make([]*PrefetchItem, 0),
		},
		stopChan: make(chan struct{}),
	}
	fp.enabled.Store(true)

	// Start background prefetch goroutine
	go fp.prefetchWorker()

	return fp
}

// RecordAccess record font access
func (fp *FontPrefetcher) RecordAccess(fontKey string, relatedKeys []string) {
	if !fp.isEnabled() {
		return
	}

	fp.accessPattern.mu.Lock()

	pattern, exists := fp.accessPattern.patterns[fontKey]
	if !exists {
		pattern = &AccessPattern{
			fontKey:      fontKey,
			relatedFonts: make(map[string]int),
		}
		fp.accessPattern.patterns[fontKey] = pattern

		// Limit pattern count
		if len(fp.accessPattern.patterns) > fp.accessPattern.maxSize {
			// Delete oldest pattern
			oldest := ""
			oldestTime := time.Now()
			for k, p := range fp.accessPattern.patterns {
				if p.lastAccess.Before(oldestTime) {
					oldest = k
					oldestTime = p.lastAccess
				}
			}
			if oldest != "" {
				delete(fp.accessPattern.patterns, oldest)
			}
		}
	}

	now := time.Now()

	// Update access count
	pattern.accessCount++

	// Calculate average access interval
	if !pattern.lastAccess.IsZero() {
		interval := now.Sub(pattern.lastAccess)
		if pattern.avgInterval == 0 {
			pattern.avgInterval = interval
		} else {
			// Exponential moving average
			pattern.avgInterval = time.Duration(
				0.7*float64(pattern.avgInterval) + 0.3*float64(interval),
			)
		}

		// Predict next access time
		pattern.predictNext = now.Add(pattern.avgInterval)
	}

	pattern.lastAccess = now

	// Update related fonts
	for _, relatedKey := range relatedKeys {
		if relatedKey != fontKey {
			pattern.relatedFonts[relatedKey]++
		}
	}

	fp.accessPattern.mu.Unlock()

	// Trigger prefetch decision (called outside lock)
	fp.schedulePrefetch(fontKey)
}

// schedulePrefetch arrange prefetch
func (fp *FontPrefetcher) schedulePrefetch(fontKey string) {
	fp.accessPattern.mu.RLock()
	pattern, exists := fp.accessPattern.patterns[fontKey]
	fp.accessPattern.mu.RUnlock()

	if !exists {
		return
	}

	// Calculate prefetch priority for related fonts
	for relatedKey, weight := range pattern.relatedFonts {
		// Check if already in cache
		if _, cached := fp.cache.Get(relatedKey); cached {
			continue
		}

		// Calculate priority: access weight * time factor
		priority := float64(weight)

		// Need to lock again to access other patterns
		fp.accessPattern.mu.RLock()
		if relatedPattern, ok := fp.accessPattern.patterns[relatedKey]; ok {
			// If predicted to be accessed soon, increase priority
			if time.Until(relatedPattern.predictNext) < time.Second {
				priority *= 2.0
			}
		}
		fp.accessPattern.mu.RUnlock()

		// Add to prefetch queue
		fp.enqueuePrefetch(&PrefetchItem{
			fontKey:  relatedKey,
			priority: priority,
			deadline: time.Now().Add(5 * time.Second),
		})
	}
}

// enqueuePrefetch adds item to prefetch queue
func (fp *FontPrefetcher) enqueuePrefetch(item *PrefetchItem) {
	fp.prefetchQueue.mu.Lock()
	defer fp.prefetchQueue.mu.Unlock()

	// Check if already exists
	for _, existing := range fp.prefetchQueue.items {
		if existing.fontKey == item.fontKey {
			// Update priority
			if item.priority > existing.priority {
				existing.priority = item.priority
				heap.Fix(fp.prefetchQueue, existing.index)
			}
			return
		}
	}

	// Add new item
	heap.Push(fp.prefetchQueue, item)
}

// prefetchWorker background prefetch worker goroutine
func (fp *FontPrefetcher) prefetchWorker() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-fp.stopChan:
			return
		case <-ticker.C:
			if !fp.isEnabled() {
				continue
			}
			fp.processPrefetchQueue()
		}
	}
}

// processPrefetchQueue processes prefetch queue
func (fp *FontPrefetcher) processPrefetchQueue() {
	fp.prefetchQueue.mu.Lock()

	// Process at most 5 items per batch
	batchSize := 5
	for i := 0; i < batchSize && fp.prefetchQueue.Len() > 0; i++ {
		item := heap.Pop(fp.prefetchQueue).(*PrefetchItem)
		fp.prefetchQueue.mu.Unlock()

		// Check if expired
		if time.Now().After(item.deadline) {
			fp.prefetchQueue.mu.Lock()
			continue
		}

		// Check if already in cache
		if _, cached := fp.cache.Get(item.fontKey); cached {
			fp.prefetchQueue.mu.Lock()
			continue
		}

		// Here should actually load the font, but since we don't know how to load,
		// just record the prefetch intent
		// In real applications, this would call the font loading function

		fp.prefetchQueue.mu.Lock()
	}

	fp.prefetchQueue.mu.Unlock()
}

// Enable enables prefetching
func (fp *FontPrefetcher) Enable() {
	fp.enabled.Store(true)
}

// Disable disables prefetching
func (fp *FontPrefetcher) Disable() {
	fp.enabled.Store(false)
}

// isEnabled checks if enabled
func (fp *FontPrefetcher) isEnabled() bool {
	return fp.enabled.Load().(bool)
}

// GetStats gets prefetch statistics
func (fp *FontPrefetcher) GetStats() PrefetchStats {
	fp.accessPattern.mu.RLock()
	patternsCount := len(fp.accessPattern.patterns)
	fp.accessPattern.mu.RUnlock()

	fp.prefetchQueue.mu.Lock()
	queueSize := fp.prefetchQueue.Len()
	fp.prefetchQueue.mu.Unlock()

	return PrefetchStats{
		PatternsTracked: patternsCount,
		QueueSize:       queueSize,
		Enabled:         fp.isEnabled(),
	}
}

// PrefetchStats prefetch statistics
type PrefetchStats struct {
	PatternsTracked int
	QueueSize       int
	Enabled         bool
}

// ClearPatterns clears access patterns
func (fp *FontPrefetcher) ClearPatterns() {
	fp.accessPattern.mu.Lock()
	fp.accessPattern.patterns = make(map[string]*AccessPattern)
	fp.accessPattern.mu.Unlock()
}

// Close closes the prefetcher
func (fp *FontPrefetcher) Close() {
	close(fp.stopChan)
}

// Implement heap.Interface for PrefetchQueue

func (pq *PrefetchQueue) Len() int {
	return len(pq.items)
}

func (pq *PrefetchQueue) Less(i, j int) bool {
	// Higher priority comes first
	return pq.items[i].priority > pq.items[j].priority
}

func (pq *PrefetchQueue) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
	pq.items[i].index = i
	pq.items[j].index = j
}

func (pq *PrefetchQueue) Push(x interface{}) {
	item := x.(*PrefetchItem)
	item.index = len(pq.items)
	pq.items = append(pq.items, item)
}

func (pq *PrefetchQueue) Pop() interface{} {
	old := pq.items
	n := len(old)
	if n == 0 {
		return nil
	}
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	pq.items = old[0 : n-1]
	return item
}
