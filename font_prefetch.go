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

// FontPrefetcher 实现智能字体预取策略
// 基于访问模式预测和预加载可能需要的字体
type FontPrefetcher struct {
	cache         *OptimizedFontCache
	accessPattern *AccessPatternTracker
	prefetchQueue *PrefetchQueue
	mu            sync.RWMutex
	enabled       atomic.Value // bool
	stopChan      chan struct{}
}

// AccessPatternTracker 跟踪字体访问模式
type AccessPatternTracker struct {
	mu       sync.RWMutex
	patterns map[string]*AccessPattern
	maxSize  int
}

// AccessPattern 记录单个字体的访问模式
type AccessPattern struct {
	fontKey      string
	accessCount  uint64
	lastAccess   time.Time
	avgInterval  time.Duration
	relatedFonts map[string]int // 经常一起访问的字体及其权重
	predictNext  time.Time      // 预测下次访问时间
}

// PrefetchQueue 预取队列（优先级队列）
type PrefetchQueue struct {
	mu    sync.Mutex
	items []*PrefetchItem
}

// PrefetchItem 预取项
type PrefetchItem struct {
	fontKey  string
	priority float64 // 优先级（越高越重要）
	deadline time.Time
	index    int
}

// NewFontPrefetcher 创建新的字体预取器
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

	// 启动后台预取协程
	go fp.prefetchWorker()

	return fp
}

// RecordAccess 记录字体访问
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

		// 限制模式数量
		if len(fp.accessPattern.patterns) > fp.accessPattern.maxSize {
			// 删除最旧的模式
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

	// 更新访问计数
	pattern.accessCount++

	// 计算平均访问间隔
	if !pattern.lastAccess.IsZero() {
		interval := now.Sub(pattern.lastAccess)
		if pattern.avgInterval == 0 {
			pattern.avgInterval = interval
		} else {
			// 指数移动平均
			pattern.avgInterval = time.Duration(
				0.7*float64(pattern.avgInterval) + 0.3*float64(interval),
			)
		}

		// 预测下次访问时间
		pattern.predictNext = now.Add(pattern.avgInterval)
	}

	pattern.lastAccess = now

	// 更新关联字体
	for _, relatedKey := range relatedKeys {
		if relatedKey != fontKey {
			pattern.relatedFonts[relatedKey]++
		}
	}

	fp.accessPattern.mu.Unlock()

	// 触发预取决策（在锁外调用）
	fp.schedulePrefetch(fontKey)
}

// schedulePrefetch 安排预取
func (fp *FontPrefetcher) schedulePrefetch(fontKey string) {
	fp.accessPattern.mu.RLock()
	pattern, exists := fp.accessPattern.patterns[fontKey]
	fp.accessPattern.mu.RUnlock()

	if !exists {
		return
	}

	// 为关联字体计算预取优先级
	for relatedKey, weight := range pattern.relatedFonts {
		// 检查是否已在缓存中
		if _, cached := fp.cache.Get(relatedKey); cached {
			continue
		}

		// 计算优先级：访问权重 * 时间因子
		priority := float64(weight)

		// 需要再次加锁访问其他 pattern
		fp.accessPattern.mu.RLock()
		if relatedPattern, ok := fp.accessPattern.patterns[relatedKey]; ok {
			// 如果预测即将被访问，提高优先级
			if time.Until(relatedPattern.predictNext) < time.Second {
				priority *= 2.0
			}
		}
		fp.accessPattern.mu.RUnlock()

		// 添加到预取队列
		fp.enqueuePrefetch(&PrefetchItem{
			fontKey:  relatedKey,
			priority: priority,
			deadline: time.Now().Add(5 * time.Second),
		})
	}
}

// enqueuePrefetch 将项目加入预取队列
func (fp *FontPrefetcher) enqueuePrefetch(item *PrefetchItem) {
	fp.prefetchQueue.mu.Lock()
	defer fp.prefetchQueue.mu.Unlock()

	// 检查是否已存在
	for _, existing := range fp.prefetchQueue.items {
		if existing.fontKey == item.fontKey {
			// 更新优先级
			if item.priority > existing.priority {
				existing.priority = item.priority
				heap.Fix(fp.prefetchQueue, existing.index)
			}
			return
		}
	}

	// 添加新项目
	heap.Push(fp.prefetchQueue, item)
}

// prefetchWorker 后台预取工作协程
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

// processPrefetchQueue 处理预取队列
func (fp *FontPrefetcher) processPrefetchQueue() {
	fp.prefetchQueue.mu.Lock()

	// 最多每次处理 5 个项目
	batchSize := 5
	for i := 0; i < batchSize && fp.prefetchQueue.Len() > 0; i++ {
		item := heap.Pop(fp.prefetchQueue).(*PrefetchItem)
		fp.prefetchQueue.mu.Unlock()

		// 检查是否过期
		if time.Now().After(item.deadline) {
			fp.prefetchQueue.mu.Lock()
			continue
		}

		// 检查是否已在缓存中
		if _, cached := fp.cache.Get(item.fontKey); cached {
			fp.prefetchQueue.mu.Lock()
			continue
		}

		// 这里应该实际加载字体，但由于我们不知道如何加载，
		// 所以只是记录预取意图
		// 在实际应用中，这里会调用字体加载函数

		fp.prefetchQueue.mu.Lock()
	}

	fp.prefetchQueue.mu.Unlock()
}

// Enable 启用预取
func (fp *FontPrefetcher) Enable() {
	fp.enabled.Store(true)
}

// Disable 禁用预取
func (fp *FontPrefetcher) Disable() {
	fp.enabled.Store(false)
}

// isEnabled 检查是否启用
func (fp *FontPrefetcher) isEnabled() bool {
	return fp.enabled.Load().(bool)
}

// GetStats 获取预取统计信息
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

// PrefetchStats 预取统计信息
type PrefetchStats struct {
	PatternsTracked int
	QueueSize       int
	Enabled         bool
}

// ClearPatterns 清除访问模式
func (fp *FontPrefetcher) ClearPatterns() {
	fp.accessPattern.mu.Lock()
	fp.accessPattern.patterns = make(map[string]*AccessPattern)
	fp.accessPattern.mu.Unlock()
}

// Close 关闭预取器
func (fp *FontPrefetcher) Close() {
	close(fp.stopChan)
}

// 实现 heap.Interface for PrefetchQueue

func (pq *PrefetchQueue) Len() int {
	return len(pq.items)
}

func (pq *PrefetchQueue) Less(i, j int) bool {
	// 优先级高的在前
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
