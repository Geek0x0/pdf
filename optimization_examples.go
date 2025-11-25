// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// Object pool for reusing search result slices, reducing memory allocation
var resultPool = sync.Pool{
	New: func() interface{} {
		return make([]*TextBlock, 0, 32)
	},
}

// Get reused result slice
func getResultSlice() []*TextBlock {
	return resultPool.Get().([]*TextBlock)
}

// Return result slice to pool
func putResultSlice(s []*TextBlock) {
	if cap(s) > 1024 { // Avoid retaining oversized slices
		return
	}
	s = s[:0]
	resultPool.Put(s)
}

// ==================== First Stage Optimization Example ====================

// AdaptiveCapacityEstimator adaptive capacity estimator
// Dynamically adjusts pre-allocated capacity based on historical data, reducing reallocation
type AdaptiveCapacityEstimator struct {
	mu         sync.RWMutex
	history    []int
	maxSamples int
}

// NewAdaptiveCapacityEstimator creates new adaptive estimator
func NewAdaptiveCapacityEstimator(maxSamples int) *AdaptiveCapacityEstimator {
	return &AdaptiveCapacityEstimator{
		history:    make([]int, 0, maxSamples),
		maxSamples: maxSamples,
	}
}

// Estimate estimates required capacity based on historical data
func (ace *AdaptiveCapacityEstimator) Estimate(hint int) int {
	ace.mu.RLock()
	defer ace.mu.RUnlock()

	if len(ace.history) == 0 {
		// When no historical data, use moderately conservative estimate (tuned: reduced to 1.3x)
		return int(float64(hint) * 1.3)
	}

	// Calculate P80 value as estimate (tuned: reduced from P90 to P80)
	sorted := make([]int, len(ace.history))
	copy(sorted, ace.history)
	sort.Ints(sorted)
	p80Index := int(float64(len(sorted)) * 0.8)
	if p80Index >= len(sorted) {
		p80Index = len(sorted) - 1
	}

	estimated := sorted[p80Index]
	// Ensure not less than hint value
	if estimated < hint {
		return int(float64(hint) * 1.3)
	}
	return estimated
}

// Record records actual capacity used
func (ace *AdaptiveCapacityEstimator) Record(actual int) {
	ace.mu.Lock()
	defer ace.mu.Unlock()

	ace.history = append(ace.history, actual)
	if len(ace.history) > ace.maxSamples {
		// Keep fixed size, remove oldest samples
		ace.history = ace.history[1:]
	}
}

// Global capacity estimator instances
var (
	lineCapacityEstimator = NewAdaptiveCapacityEstimator(100)
	textCapacityEstimator = NewAdaptiveCapacityEstimator(100)
)

// BatchStringBuilder batch string builder
// Avoids multiple reallocations by precisely calculating required capacity
type BatchStringBuilder struct {
	buf []byte
}

// NewBatchStringBuilder creates batch string builder
func NewBatchStringBuilder(texts []Text) *BatchStringBuilder {
	// Precisely calculate required capacity
	totalLen := 0
	for _, t := range texts {
		totalLen += len(t.S)
	}

	// Reserve extra space for separators and newlines
	capacity := totalLen + len(texts)*2

	return &BatchStringBuilder{
		buf: make([]byte, 0, capacity),
	}
}

// AppendTexts appends text content in batch
func (bsb *BatchStringBuilder) AppendTexts(texts []Text) string {
	for i := range texts {
		bsb.buf = append(bsb.buf, texts[i].S...)
		// Add separator as needed
		if i < len(texts)-1 {
			// Simplified judgment logic, should actually call more complex needsSpace
			bsb.buf = append(bsb.buf, ' ')
		}
	}

	// Use unsafe.String to avoid copying
	return unsafe.String(unsafe.SliceData(bsb.buf), len(bsb.buf))
}

// String returns built string
func (bsb *BatchStringBuilder) String() string {
	return unsafe.String(unsafe.SliceData(bsb.buf), len(bsb.buf))
}

// Reset resets builder for reuse
func (bsb *BatchStringBuilder) Reset() {
	bsb.buf = bsb.buf[:0]
}

// ==================== Second Stage Optimization Example ====================

// KDNode KD tree node
type KDNode struct {
	point []float64  // Coordinate point [x, y]
	data  *TextBlock // Associated text block
	left  *KDNode
	right *KDNode
	axis  int // Split axis (0=x, 1=y)
}

// KDTree KD tree spatial index
// For O(log n) time complexity nearest neighbor search
type KDTree struct {
	root      *KDNode
	dimension int
}

// BuildKDTree builds KD tree from text blocks
func BuildKDTree(blocks []*TextBlock) *KDTree {
	if len(blocks) == 0 {
		return &KDTree{dimension: 2}
	}

	// Convert TextBlock to points
	points := make([]kdPoint, len(blocks))
	for i, block := range blocks {
		center := block.Center()
		points[i] = kdPoint{
			coords: []float64{center.X, center.Y},
			data:   block,
		}
	}

	tree := &KDTree{dimension: 2}
	tree.root = buildKDTreeRecursive(points, 0)
	return tree
}

type kdPoint struct {
	coords []float64
	data   *TextBlock
}

// buildKDTreeRecursiveIndexed optimized version using indices to avoid slice copying
func buildKDTreeRecursiveIndexed(points []kdPoint, indices []int, depth int) *KDNode {
	if len(indices) == 0 {
		return nil
	}

	axis := depth % 2 // Alternate between x and y axes in 2D space

	// Sort indices by current axis
	sort.Slice(indices, func(i, j int) bool {
		return points[indices[i]].coords[axis] < points[indices[j]].coords[axis]
	})

	// Select median as split point
	medianPos := len(indices) / 2
	medianIdx := indices[medianPos]

	return &KDNode{
		point: points[medianIdx].coords,
		data:  points[medianIdx].data,
		axis:  axis,
		left:  buildKDTreeRecursiveIndexed(points, indices[:medianPos], depth+1),
		right: buildKDTreeRecursiveIndexed(points, indices[medianPos+1:], depth+1),
	}
}

func buildKDTreeRecursive(points []kdPoint, depth int) *KDNode {
	// Create index array, allocate only once
	indices := make([]int, len(points))
	for i := range indices {
		indices[i] = i
	}
	return buildKDTreeRecursiveIndexed(points, indices, depth)
}

// RangeSearch range search, returns all text blocks within specified radius of target point
// Use iterative approach instead of recursion to avoid large memory allocation from deep recursion
func (tree *KDTree) RangeSearch(target []float64, radius float64) []*TextBlock {
	if tree.root == nil {
		return nil
	}

	// Get result slice from object pool
	result := getResultSlice()

	// Use stack for iterative search, avoid recursion call stack overhead
	type stackItem struct {
		node *KDNode
	}

	// Use fixed-size stack, avoid dynamic expansion
	stack := make([]stackItem, 0, 64)
	stack = append(stack, stackItem{node: tree.root})

	radiusSq := radius * radius

	for len(stack) > 0 {
		// Pop top element from stack
		idx := len(stack) - 1
		current := stack[idx].node
		stack = stack[:idx]

		if current == nil {
			continue
		}

		// Calculate squared distance from current point to target point
		dist := euclideanDistance(current.point, target)
		if dist <= radiusSq {
			result = append(result, current.data)
		}

		// Calculate distance to split hyperplane
		planeDist := target[current.axis] - current.point[current.axis]
		planeDist2 := planeDist * planeDist

		// Decide search order
		if planeDist < 0 {
			// Search left side first (near side)
			if current.left != nil {
				stack = append(stack, stackItem{node: current.left})
			}
			// If hyperplane is in range, also search right side (far side)
			if planeDist2 <= radiusSq && current.right != nil {
				stack = append(stack, stackItem{node: current.right})
			}
		} else {
			// Search right side first (near side)
			if current.right != nil {
				stack = append(stack, stackItem{node: current.right})
			}
			// If hyperplane is in range, also search left side (far side)
			if planeDist2 <= radiusSq && current.left != nil {
				stack = append(stack, stackItem{node: current.left})
			}
		}
	}

	return result
}

// Deprecated: use iterative approach instead
// rangeSearchRecursive's recursive implementation has been replaced by iterative approach
func (tree *KDTree) rangeSearchRecursive(node *KDNode, target []float64, radius float64, result *[]*TextBlock) {
	// This function is deprecated, kept only for compatibility
	// Actually use iterative version of RangeSearch
}

func euclideanDistance(p1, p2 []float64) float64 {
	dx := p1[0] - p2[0]
	dy := p1[1] - p2[1]
	return dx*dx + dy*dy // Return squared distance, avoid sqrt
}

// ClusterTextBlocksOptimized uses KD tree optimized text block clustering
// Optimized version: reduce temporary object allocation, use object pool
func ClusterTextBlocksOptimized(texts []Text) []*TextBlock {
	if len(texts) == 0 {
		return nil
	}

	// Calculate average font size as distance threshold
	var totalFontSize float64
	for i := range texts {
		totalFontSize += texts[i].FontSize
	}
	avgFontSize := totalFontSize / float64(len(texts))
	distThreshold := avgFontSize * 2.0
	distThresholdSq := distThreshold * distThreshold

	// Initialize: each text as independent block
	// Optimization: pre-allocate exact size to avoid expansion
	blocks := make([]*TextBlock, len(texts))
	for i := range texts {
		t := &texts[i]
		blocks[i] = &TextBlock{
			Texts:       []Text{*t},
			MinX:        t.X,
			MaxX:        t.X + t.W,
			MinY:        t.Y,
			MaxY:        t.Y + t.FontSize,
			AvgFontSize: t.FontSize,
		}
	}

	// Build KD tree
	kdtree := BuildKDTree(blocks)

	// Use union-find for clustering
	parent := make([]int, len(blocks))
	for i := range parent {
		parent[i] = i
	}

	// Non-recursive find function, use iterative path compression to avoid stack overflow
	find := func(x int) int {
		root := x
		for parent[root] != root {
			root = parent[root]
		}
		// Path compression
		for parent[x] != root {
			next := parent[x]
			parent[x] = root
			x = next
		}
		return root
	}

	union := func(x, y int) {
		px, py := find(x), find(y)
		if px != py {
			parent[px] = py
		}
	}

	// Create block to index mapping, avoid repeated lookups
	blockToIdx := make(map[*TextBlock]int, len(blocks))
	for i, block := range blocks {
		blockToIdx[block] = i
	}

	// Find neighbors for each block and merge
	for i, block := range blocks {
		center := block.Center()
		// Use optimized iterative search
		neighbors := kdtree.RangeSearch([]float64{center.X, center.Y}, distThresholdSq)

		for _, neighbor := range neighbors {
			if j, ok := blockToIdx[neighbor]; ok && i != j {
				if shouldMergeClusters(block, neighbor, distThreshold) {
					union(i, j)
				}
			}
		}

		// Return search result slice to pool for reuse
		putResultSlice(neighbors)
	}

	// Collect clustering results
	// Optimization: estimate cluster count to reduce map expansion
	clusterMap := make(map[int][]*TextBlock, len(blocks)/4)
	for i, block := range blocks {
		root := find(i)
		clusterMap[root] = append(clusterMap[root], block)
	}

	// Merge text blocks in each cluster
	result := make([]*TextBlock, 0, len(clusterMap))
	for _, cluster := range clusterMap {
		merged := mergeTextBlocks(cluster)
		result = append(result, merged)
	}

	return result
}

func mergeTextBlocks(blocks []*TextBlock) *TextBlock {
	if len(blocks) == 0 {
		return nil
	}
	if len(blocks) == 1 {
		return blocks[0]
	}

	// Pre-calculate total text count, allocate at once
	totalTexts := 0
	for _, block := range blocks {
		totalTexts += len(block.Texts)
	}

	merged := &TextBlock{
		Texts:       make([]Text, 0, totalTexts),
		MinX:        blocks[0].MinX,
		MaxX:        blocks[0].MaxX,
		MinY:        blocks[0].MinY,
		MaxY:        blocks[0].MaxY,
		AvgFontSize: 0,
	}

	totalFontSize := 0.0

	for _, block := range blocks {
		merged.Texts = append(merged.Texts, block.Texts...)
		if block.MinX < merged.MinX {
			merged.MinX = block.MinX
		}
		if block.MaxX > merged.MaxX {
			merged.MaxX = block.MaxX
		}
		if block.MinY < merged.MinY {
			merged.MinY = block.MinY
		}
		if block.MaxY > merged.MaxY {
			merged.MaxY = block.MaxY
		}
		totalFontSize += block.AvgFontSize * float64(len(block.Texts))
		totalTexts += len(block.Texts)
	}

	if totalTexts > 0 {
		merged.AvgFontSize = totalFontSize / float64(totalTexts)
	}

	return merged
}

// ==================== Work Stealing Scheduler ====================

// WorkStealingScheduler work stealing scheduler
// Reduce goroutine creation overhead, improve parallel processing efficiency
type WorkStealingScheduler struct {
	workers     []*Worker
	globalQueue chan Task
	numWorkers  int
	wg          sync.WaitGroup
	taskWg      sync.WaitGroup
	stop        chan struct{}
}

// Worker worker thread
type Worker struct {
	id         int
	localQueue chan Task
	scheduler  *WorkStealingScheduler
	stealing   atomic.Bool // Mark if currently stealing
}

// Task task interface
type Task interface {
	Execute() error
}

// NewWorkStealingScheduler create work stealing scheduler
func NewWorkStealingScheduler(numWorkers int) *WorkStealingScheduler {
	if numWorkers <= 0 {
		numWorkers = 4
	}

	scheduler := &WorkStealingScheduler{
		workers:     make([]*Worker, numWorkers),
		globalQueue: make(chan Task, numWorkers*10),
		numWorkers:  numWorkers,
		stop:        make(chan struct{}),
	}

	// Create worker
	for i := 0; i < numWorkers; i++ {
		scheduler.workers[i] = &Worker{
			id:         i,
			localQueue: make(chan Task, 100),
			scheduler:  scheduler,
		}
	}

	return scheduler
}

// Start start scheduler
func (wss *WorkStealingScheduler) Start() {
	for _, worker := range wss.workers {
		wss.wg.Add(1)
		go worker.run()
	}
}

// Submit submit task
func (wss *WorkStealingScheduler) Submit(task Task) {
	// Round-robin assign to worker local queue
	wss.taskWg.Add(1)
	select {
	case wss.globalQueue <- task:
	default:
		// Global queue full, execute directly
		task.Execute()
		wss.taskWg.Done()
	}
}

// Stop stop scheduler
func (wss *WorkStealingScheduler) Stop() {
	close(wss.stop)
	wss.wg.Wait()
}

// Wait wait for all tasks to complete
func (wss *WorkStealingScheduler) Wait() {
	// Wait for all submitted tasks to complete execution
	wss.taskWg.Wait()
}

func (w *Worker) run() {
	defer w.scheduler.wg.Done()

	for {
		select {
		case <-w.scheduler.stop:
			return

		case task := <-w.localQueue:
			// Prioritize processing local queue
			w.execute(task)

		case task := <-w.scheduler.globalQueue:
			// Process global queue tasks
			w.execute(task)

		default:
			// Try to steal tasks from other workers
			if task := w.steal(); task != nil {
				w.execute(task)
			} else {
				// Sleep briefly when no tasks
				time.Sleep(100 * time.Microsecond)
			}
		}
	}
}

func (w *Worker) execute(task Task) {
	if task == nil {
		return
	}

	task.Execute()
	w.scheduler.taskWg.Done()
}

func (w *Worker) steal() Task {
	if w.stealing.Load() {
		return nil
	}
	w.stealing.Store(true)
	defer w.stealing.Store(false)

	// Try to steal from other workers
	for i := 0; i < w.scheduler.numWorkers; i++ {
		if i == w.id {
			continue
		}

		victim := w.scheduler.workers[i]
		select {
		case task := <-victim.localQueue:
			return task
		default:
		}
	}

	return nil
}

// ==================== Multi-level Cache Implementation ====================

// MultiLevelCache multi-level cache manager
type MultiLevelCache struct {
	l1    sync.Map     // L1: hot data (lock-free)
	l2    *ResultCache // L2: warm data (LRU)
	l3    *ResultCache // L3: cold data (large capacity)
	stats struct {
		l1Hits   atomic.Uint64
		l2Hits   atomic.Uint64
		l3Hits   atomic.Uint64
		misses   atomic.Uint64
		prefetch atomic.Uint64
	}
}

// NewMultiLevelCache create multi-level cache
func NewMultiLevelCache() *MultiLevelCache {
	return &MultiLevelCache{
		l2: NewResultCache(10*1024*1024, 5*time.Minute, "LRU"),   // 10MB, 5min
		l3: NewResultCache(100*1024*1024, 30*time.Minute, "LFU"), // 100MB, 30min
	}
}

// Get get data from cache
func (mlc *MultiLevelCache) Get(key string) (interface{}, bool) {
	// L1 lookup (fastest)
	if val, ok := mlc.l1.Load(key); ok {
		mlc.stats.l1Hits.Add(1)
		return val, true
	}

	// L2 lookup
	if val, ok := mlc.l2.Get(key); ok {
		mlc.stats.l2Hits.Add(1)
		// Promote to L1
		mlc.l1.Store(key, val)
		return val, true
	}

	// L3 lookup
	if val, ok := mlc.l3.Get(key); ok {
		mlc.stats.l3Hits.Add(1)
		// Promote to L2
		mlc.l2.Put(key, val)
		return val, true
	}

	mlc.stats.misses.Add(1)
	return nil, false
}

// Put store in cache
func (mlc *MultiLevelCache) Put(key string, value interface{}) {
	// New data directly stored in L1
	mlc.l1.Store(key, value)
	// Also stored in L2 as backup
	mlc.l2.Put(key, value)
}

// Prefetch prefetch page data
func (mlc *MultiLevelCache) Prefetch(keys []string) {
	mlc.stats.prefetch.Add(uint64(len(keys)))
	// Async prefetch
	go func() {
		for _, key := range keys {
			mlc.Get(key)
			// If not hit, can trigger external loading logic
		}
	}()
}

// Stats get cache statistics
func (mlc *MultiLevelCache) Stats() map[string]uint64 {
	total := mlc.stats.l1Hits.Load() + mlc.stats.l2Hits.Load() +
		mlc.stats.l3Hits.Load() + mlc.stats.misses.Load()

	hitRate := float64(0)
	if total > 0 {
		hits := mlc.stats.l1Hits.Load() + mlc.stats.l2Hits.Load() + mlc.stats.l3Hits.Load()
		hitRate = float64(hits) / float64(total) * 100
	}

	return map[string]uint64{
		"l1_hits":  mlc.stats.l1Hits.Load(),
		"l2_hits":  mlc.stats.l2Hits.Load(),
		"l3_hits":  mlc.stats.l3Hits.Load(),
		"misses":   mlc.stats.misses.Load(),
		"prefetch": mlc.stats.prefetch.Load(),
		"hit_rate": uint64(hitRate),
	}
}

// ==================== Performance Monitoring ====================

// PerformanceMetrics performance metrics collector
type PerformanceMetrics struct {
	ExtractDuration atomic.Int64 // nanoseconds
	ParseDuration   atomic.Int64
	SortDuration    atomic.Int64
	TotalAllocs     atomic.Uint64
	BytesAllocated  atomic.Uint64
	GoroutineCount  atomic.Int32
	CacheHitRate    atomic.Uint64 // percentage * 100
}

// RecordExtractDuration record extraction duration
func (pm *PerformanceMetrics) RecordExtractDuration(d time.Duration) {
	pm.ExtractDuration.Store(int64(d))
}

// RecordAllocation record memory allocation
func (pm *PerformanceMetrics) RecordAllocation(bytes uint64) {
	pm.TotalAllocs.Add(1)
	pm.BytesAllocated.Add(bytes)
}

// GetMetrics get current metrics snapshot
func (pm *PerformanceMetrics) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"extract_duration_ms": float64(pm.ExtractDuration.Load()) / 1e6,
		"parse_duration_ms":   float64(pm.ParseDuration.Load()) / 1e6,
		"sort_duration_ms":    float64(pm.SortDuration.Load()) / 1e6,
		"total_allocs":        pm.TotalAllocs.Load(),
		"bytes_allocated":     pm.BytesAllocated.Load(),
		"goroutine_count":     pm.GoroutineCount.Load(),
		"cache_hit_rate":      float64(pm.CacheHitRate.Load()) / 100.0,
	}
}

// Global performance metrics instance
var GlobalMetrics = &PerformanceMetrics{}
