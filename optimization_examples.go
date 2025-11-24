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

// 对象池用于复用搜索结果切片,减少内存分配
var resultPool = sync.Pool{
	New: func() interface{} {
		return make([]*TextBlock, 0, 32)
	},
}

// 获取复用的结果切片
func getResultSlice() []*TextBlock {
	return resultPool.Get().([]*TextBlock)
}

// 归还结果切片到池中
func putResultSlice(s []*TextBlock) {
	if cap(s) > 1024 { // 避免保留过大的切片
		return
	}
	s = s[:0]
	resultPool.Put(s)
}

// ==================== 第一阶段优化示例 ====================

// AdaptiveCapacityEstimator 自适应容量估算器
// 基于历史数据动态调整预分配容量，减少重新分配
type AdaptiveCapacityEstimator struct {
	mu         sync.RWMutex
	history    []int
	maxSamples int
}

// NewAdaptiveCapacityEstimator 创建新的自适应估算器
func NewAdaptiveCapacityEstimator(maxSamples int) *AdaptiveCapacityEstimator {
	return &AdaptiveCapacityEstimator{
		history:    make([]int, 0, maxSamples),
		maxSamples: maxSamples,
	}
}

// Estimate 基于历史数据估算所需容量
func (ace *AdaptiveCapacityEstimator) Estimate(hint int) int {
	ace.mu.RLock()
	defer ace.mu.RUnlock()

	if len(ace.history) == 0 {
		// 无历史数据时，使用适度保守估计（调优：降低到1.3x）
		return int(float64(hint) * 1.3)
	}

	// 计算P80值作为估算（调优：从P90降低到P80）
	sorted := make([]int, len(ace.history))
	copy(sorted, ace.history)
	sort.Ints(sorted)
	p80Index := int(float64(len(sorted)) * 0.8)
	if p80Index >= len(sorted) {
		p80Index = len(sorted) - 1
	}

	estimated := sorted[p80Index]
	// 确保不小于提示值
	if estimated < hint {
		return int(float64(hint) * 1.3)
	}
	return estimated
}

// Record 记录实际使用的容量
func (ace *AdaptiveCapacityEstimator) Record(actual int) {
	ace.mu.Lock()
	defer ace.mu.Unlock()

	ace.history = append(ace.history, actual)
	if len(ace.history) > ace.maxSamples {
		// 保持固定大小，移除最老的样本
		ace.history = ace.history[1:]
	}
}

// 全局容量估算器实例
var (
	lineCapacityEstimator = NewAdaptiveCapacityEstimator(100)
	textCapacityEstimator = NewAdaptiveCapacityEstimator(100)
)

// BatchStringBuilder 批量字符串构建器
// 通过精确计算所需容量，避免多次重新分配
type BatchStringBuilder struct {
	buf []byte
}

// NewBatchStringBuilder 创建批量字符串构建器
func NewBatchStringBuilder(texts []Text) *BatchStringBuilder {
	// 精确计算所需容量
	totalLen := 0
	for _, t := range texts {
		totalLen += len(t.S)
	}

	// 额外预留空间用于分隔符和换行符
	capacity := totalLen + len(texts)*2

	return &BatchStringBuilder{
		buf: make([]byte, 0, capacity),
	}
}

// AppendTexts 批量追加文本内容
func (bsb *BatchStringBuilder) AppendTexts(texts []Text) string {
	for i := range texts {
		bsb.buf = append(bsb.buf, texts[i].S...)
		// 根据需要添加分隔符
		if i < len(texts)-1 {
			// 简化判断逻辑，实际应调用更复杂的needsSpace
			bsb.buf = append(bsb.buf, ' ')
		}
	}

	// 使用unsafe.String避免拷贝
	return unsafe.String(unsafe.SliceData(bsb.buf), len(bsb.buf))
}

// String 返回构建的字符串
func (bsb *BatchStringBuilder) String() string {
	return unsafe.String(unsafe.SliceData(bsb.buf), len(bsb.buf))
}

// Reset 重置构建器以便重用
func (bsb *BatchStringBuilder) Reset() {
	bsb.buf = bsb.buf[:0]
}

// ==================== 第二阶段优化示例 ====================

// KDNode KD树节点
type KDNode struct {
	point []float64  // 坐标点 [x, y]
	data  *TextBlock // 关联的文本块
	left  *KDNode
	right *KDNode
	axis  int // 分割轴（0=x, 1=y）
}

// KDTree KD树空间索引
// 用于O(log n)时间复杂度的最近邻搜索
type KDTree struct {
	root      *KDNode
	dimension int
}

// BuildKDTree 从文本块构建KD树
func BuildKDTree(blocks []*TextBlock) *KDTree {
	if len(blocks) == 0 {
		return &KDTree{dimension: 2}
	}

	// 将TextBlock转换为点
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

// buildKDTreeRecursiveIndexed 使用索引避免切片复制的优化版本
func buildKDTreeRecursiveIndexed(points []kdPoint, indices []int, depth int) *KDNode {
	if len(indices) == 0 {
		return nil
	}

	axis := depth % 2 // 在2D空间中交替使用x和y轴

	// 按当前轴排序索引
	sort.Slice(indices, func(i, j int) bool {
		return points[indices[i]].coords[axis] < points[indices[j]].coords[axis]
	})

	// 选择中位数作为分割点
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
	// 创建索引数组,只分配一次
	indices := make([]int, len(points))
	for i := range indices {
		indices[i] = i
	}
	return buildKDTreeRecursiveIndexed(points, indices, depth)
}

// RangeSearch 范围搜索，返回距离目标点在指定半径内的所有文本块
// 使用迭代方式替代递归,避免深度递归导致的大量内存分配
func (tree *KDTree) RangeSearch(target []float64, radius float64) []*TextBlock {
	if tree.root == nil {
		return nil
	}

	// 从对象池获取结果切片
	result := getResultSlice()

	// 使用栈进行迭代搜索,避免递归调用栈开销
	type stackItem struct {
		node *KDNode
	}

	// 使用固定大小的栈,避免动态扩容
	stack := make([]stackItem, 0, 64)
	stack = append(stack, stackItem{node: tree.root})

	radiusSq := radius * radius

	for len(stack) > 0 {
		// 弹出栈顶元素
		idx := len(stack) - 1
		current := stack[idx].node
		stack = stack[:idx]

		if current == nil {
			continue
		}

		// 计算当前点到目标点的距离平方
		dist := euclideanDistance(current.point, target)
		if dist <= radiusSq {
			result = append(result, current.data)
		}

		// 计算到分割超平面的距离
		planeDist := target[current.axis] - current.point[current.axis]
		planeDist2 := planeDist * planeDist

		// 决定搜索顺序
		if planeDist < 0 {
			// 先搜索左侧(近侧)
			if current.left != nil {
				stack = append(stack, stackItem{node: current.left})
			}
			// 如果超平面在范围内,也搜索右侧(远侧)
			if planeDist2 <= radiusSq && current.right != nil {
				stack = append(stack, stackItem{node: current.right})
			}
		} else {
			// 先搜索右侧(近侧)
			if current.right != nil {
				stack = append(stack, stackItem{node: current.right})
			}
			// 如果超平面在范围内,也搜索左侧(远侧)
			if planeDist2 <= radiusSq && current.left != nil {
				stack = append(stack, stackItem{node: current.left})
			}
		}
	}

	return result
}

// 已废弃: 使用迭代方式替代
// rangeSearchRecursive 的递归实现已被迭代方式取代
func (tree *KDTree) rangeSearchRecursive(node *KDNode, target []float64, radius float64, result *[]*TextBlock) {
	// 此函数已废弃,保留仅为兼容性
	// 实际使用迭代版本的 RangeSearch
}

func euclideanDistance(p1, p2 []float64) float64 {
	dx := p1[0] - p2[0]
	dy := p1[1] - p2[1]
	return dx*dx + dy*dy // 返回距离平方，避免sqrt
}

// ClusterTextBlocksOptimized 使用KD树优化的文本块聚类
// 优化版本:减少临时对象分配,使用对象池
func ClusterTextBlocksOptimized(texts []Text) []*TextBlock {
	if len(texts) == 0 {
		return nil
	}

	// 计算平均字体大小作为距离阈值
	var totalFontSize float64
	for i := range texts {
		totalFontSize += texts[i].FontSize
	}
	avgFontSize := totalFontSize / float64(len(texts))
	distThreshold := avgFontSize * 2.0
	distThresholdSq := distThreshold * distThreshold

	// 初始化：每个文本作为独立块
	// 优化: 预分配精确大小避免扩容
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

	// 构建KD树
	kdtree := BuildKDTree(blocks)

	// 使用并查集进行聚类
	parent := make([]int, len(blocks))
	for i := range parent {
		parent[i] = i
	}

	// 非递归的 find 函数,使用迭代路径压缩避免栈溢出
	find := func(x int) int {
		root := x
		for parent[root] != root {
			root = parent[root]
		}
		// 路径压缩
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

	// 创建block到索引的映射,避免重复查找
	blockToIdx := make(map[*TextBlock]int, len(blocks))
	for i, block := range blocks {
		blockToIdx[block] = i
	}

	// 对每个块查找近邻并合并
	for i, block := range blocks {
		center := block.Center()
		// 使用优化后的迭代搜索
		neighbors := kdtree.RangeSearch([]float64{center.X, center.Y}, distThresholdSq)

		for _, neighbor := range neighbors {
			if j, ok := blockToIdx[neighbor]; ok && i != j {
				if shouldMergeClusters(block, neighbor, distThreshold) {
					union(i, j)
				}
			}
		}

		// 归还搜索结果切片到池中复用
		putResultSlice(neighbors)
	}

	// 收集聚类结果
	// 优化: 预估集群数量减少 map 扩容
	clusterMap := make(map[int][]*TextBlock, len(blocks)/4)
	for i, block := range blocks {
		root := find(i)
		clusterMap[root] = append(clusterMap[root], block)
	}

	// 合并每个聚类中的文本块
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

	// 预先计算总文本数量，一次性分配
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

// ==================== 工作窃取调度器 ====================

// WorkStealingScheduler 工作窃取调度器
// 减少goroutine创建开销，提升并行处理效率
type WorkStealingScheduler struct {
	workers     []*Worker
	globalQueue chan Task
	numWorkers  int
	wg          sync.WaitGroup
	stop        chan struct{}
}

// Worker 工作线程
type Worker struct {
	id         int
	localQueue chan Task
	scheduler  *WorkStealingScheduler
	stealing   atomic.Bool // 标记是否正在窃取
}

// Task 任务接口
type Task interface {
	Execute() error
}

// NewWorkStealingScheduler 创建工作窃取调度器
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

	// 创建worker
	for i := 0; i < numWorkers; i++ {
		scheduler.workers[i] = &Worker{
			id:         i,
			localQueue: make(chan Task, 100),
			scheduler:  scheduler,
		}
	}

	return scheduler
}

// Start 启动调度器
func (wss *WorkStealingScheduler) Start() {
	for _, worker := range wss.workers {
		wss.wg.Add(1)
		go worker.run()
	}
}

// Submit 提交任务
func (wss *WorkStealingScheduler) Submit(task Task) {
	// 轮询分配到worker本地队列
	select {
	case wss.globalQueue <- task:
	default:
		// 全局队列满，直接执行
		task.Execute()
	}
}

// Stop 停止调度器
func (wss *WorkStealingScheduler) Stop() {
	close(wss.stop)
	wss.wg.Wait()
}

// Wait 等待所有任务完成
func (wss *WorkStealingScheduler) Wait() {
	// 等待所有队列为空
	for {
		allEmpty := true
		if len(wss.globalQueue) > 0 {
			allEmpty = false
		}
		for _, worker := range wss.workers {
			if len(worker.localQueue) > 0 {
				allEmpty = false
				break
			}
		}
		if allEmpty {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (w *Worker) run() {
	defer w.scheduler.wg.Done()

	for {
		select {
		case <-w.scheduler.stop:
			return

		case task := <-w.localQueue:
			// 优先处理本地队列
			task.Execute()

		case task := <-w.scheduler.globalQueue:
			// 处理全局队列任务
			task.Execute()

		default:
			// 尝试窃取其他worker的任务
			if task := w.steal(); task != nil {
				task.Execute()
			} else {
				// 无任务时短暂休眠
				time.Sleep(100 * time.Microsecond)
			}
		}
	}
}

func (w *Worker) steal() Task {
	if w.stealing.Load() {
		return nil
	}
	w.stealing.Store(true)
	defer w.stealing.Store(false)

	// 尝试从其他worker窃取
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

// ==================== 多级缓存实现 ====================

// MultiLevelCache 多级缓存管理器
type MultiLevelCache struct {
	l1    sync.Map     // L1: 热数据（无锁）
	l2    *ResultCache // L2: 温数据（LRU）
	l3    *ResultCache // L3: 冷数据（大容量）
	stats struct {
		l1Hits   atomic.Uint64
		l2Hits   atomic.Uint64
		l3Hits   atomic.Uint64
		misses   atomic.Uint64
		prefetch atomic.Uint64
	}
}

// NewMultiLevelCache 创建多级缓存
func NewMultiLevelCache() *MultiLevelCache {
	return &MultiLevelCache{
		l2: NewResultCache(10*1024*1024, 5*time.Minute, "LRU"),   // 10MB, 5min
		l3: NewResultCache(100*1024*1024, 30*time.Minute, "LFU"), // 100MB, 30min
	}
}

// Get 从缓存获取数据
func (mlc *MultiLevelCache) Get(key string) (interface{}, bool) {
	// L1查找（最快）
	if val, ok := mlc.l1.Load(key); ok {
		mlc.stats.l1Hits.Add(1)
		return val, true
	}

	// L2查找
	if val, ok := mlc.l2.Get(key); ok {
		mlc.stats.l2Hits.Add(1)
		// 提升到L1
		mlc.l1.Store(key, val)
		return val, true
	}

	// L3查找
	if val, ok := mlc.l3.Get(key); ok {
		mlc.stats.l3Hits.Add(1)
		// 提升到L2
		mlc.l2.Put(key, val)
		return val, true
	}

	mlc.stats.misses.Add(1)
	return nil, false
}

// Put 存入缓存
func (mlc *MultiLevelCache) Put(key string, value interface{}) {
	// 新数据直接存入L1
	mlc.l1.Store(key, value)
	// 同时存入L2作为备份
	mlc.l2.Put(key, value)
}

// Prefetch 预取页面数据
func (mlc *MultiLevelCache) Prefetch(keys []string) {
	mlc.stats.prefetch.Add(uint64(len(keys)))
	// 异步预取
	go func() {
		for _, key := range keys {
			mlc.Get(key)
			// 如果未命中，可以触发外部加载逻辑
		}
	}()
}

// Stats 获取缓存统计
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

// ==================== 性能监控 ====================

// PerformanceMetrics 性能指标收集器
type PerformanceMetrics struct {
	ExtractDuration atomic.Int64 // 纳秒
	ParseDuration   atomic.Int64
	SortDuration    atomic.Int64
	TotalAllocs     atomic.Uint64
	BytesAllocated  atomic.Uint64
	GoroutineCount  atomic.Int32
	CacheHitRate    atomic.Uint64 // 百分比 * 100
}

// RecordExtractDuration 记录提取耗时
func (pm *PerformanceMetrics) RecordExtractDuration(d time.Duration) {
	pm.ExtractDuration.Store(int64(d))
}

// RecordAllocation 记录内存分配
func (pm *PerformanceMetrics) RecordAllocation(bytes uint64) {
	pm.TotalAllocs.Add(1)
	pm.BytesAllocated.Add(bytes)
}

// GetMetrics 获取当前指标快照
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

// 全局性能指标实例
var GlobalMetrics = &PerformanceMetrics{}
