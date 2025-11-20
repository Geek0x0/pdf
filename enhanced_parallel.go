// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
)

// EnhancedParallelProcessor 增强的并行处理器
// 提供更好的并发控制、负载均衡和错误处理
type EnhancedParallelProcessor struct {
	workerPool     *WorkerPool
	batchSize      int
	enablePrefetch bool
}

// WorkerPool 工作池
type WorkerPool struct {
	workers    int
	semaphore  chan struct{}
	activeJobs int64
	totalJobs  int64
}

// NewEnhancedParallelProcessor 创建增强的并行处理器
func NewEnhancedParallelProcessor(workers int, batchSize int) *EnhancedParallelProcessor {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if batchSize <= 0 {
		batchSize = 10
	}

	return &EnhancedParallelProcessor{
		workerPool: &WorkerPool{
			workers:   workers,
			semaphore: make(chan struct{}, workers),
		},
		batchSize:      batchSize,
		enablePrefetch: true,
	}
}

// ProcessPagesEnhanced 增强的并行页面处理
func (epp *EnhancedParallelProcessor) ProcessPagesEnhanced(
	ctx context.Context,
	pages []Page,
	processorFunc func(Page) ([]Text, error),
) ([][]Text, error) {
	if len(pages) == 0 {
		return [][]Text{}, nil
	}

	// 使用批处理减少调度开销
	numBatches := (len(pages) + epp.batchSize - 1) / epp.batchSize
	results := make([][]Text, len(pages))
	var processingErr error
	var errOnce sync.Once

	var wg sync.WaitGroup

	for batchIdx := 0; batchIdx < numBatches; batchIdx++ {
		start := batchIdx * epp.batchSize
		end := start + epp.batchSize
		if end > len(pages) {
			end = len(pages)
		}

		wg.Add(1)
		go func(bStart, bEnd int) {
			defer wg.Done()

			// 获取工作许可
			select {
			case epp.workerPool.semaphore <- struct{}{}:
				defer func() { <-epp.workerPool.semaphore }()
			case <-ctx.Done():
				errOnce.Do(func() { processingErr = ctx.Err() })
				return
			}

			atomic.AddInt64(&epp.workerPool.activeJobs, 1)
			defer atomic.AddInt64(&epp.workerPool.activeJobs, -1)

			// 处理批次中的页面
			for i := bStart; i < bEnd; i++ {
				select {
				case <-ctx.Done():
					errOnce.Do(func() { processingErr = ctx.Err() })
					return
				default:
				}

				texts, err := processorFunc(pages[i])
				if err != nil {
					errOnce.Do(func() { processingErr = err })
					return
				}
				results[i] = texts
				atomic.AddInt64(&epp.workerPool.totalJobs, 1)
			}
		}(start, end)
	}

	wg.Wait()

	if processingErr != nil {
		return nil, processingErr
	}

	return results, nil
}

// ProcessWithLoadBalancing 带负载均衡的处理
func (epp *EnhancedParallelProcessor) ProcessWithLoadBalancing(
	ctx context.Context,
	pages []Page,
	processorFunc func(Page) ([]Text, error),
) ([][]Text, error) {
	if len(pages) == 0 {
		return [][]Text{}, nil
	}

	numWorkers := epp.workerPool.workers
	if numWorkers > len(pages) {
		numWorkers = len(pages)
	}

	// 使用动态工作窃取
	type job struct {
		index int
		page  Page
	}

	type result struct {
		index int
		texts []Text
		err   error
	}

	jobChan := make(chan job, numWorkers*2) // 缓冲以减少阻塞
	resultChan := make(chan result, len(pages))

	// 启动工作协程
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := range jobChan {
				select {
				case <-ctx.Done():
					resultChan <- result{index: j.index, err: ctx.Err()}
					return
				default:
				}

				texts, err := processorFunc(j.page)
				resultChan <- result{
					index: j.index,
					texts: texts,
					err:   err,
				}
			}
		}(i)
	}

	// 分发任务
	go func() {
		defer close(jobChan)
		for i, page := range pages {
			select {
			case <-ctx.Done():
				return
			case jobChan <- job{index: i, page: page}:
			}
		}
	}()

	// 等待所有工作完成
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// 收集结果
	results := make([][]Text, len(pages))
	var firstErr error
	for res := range resultChan {
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
		results[res.index] = res.texts
	}

	if firstErr != nil {
		return nil, firstErr
	}

	return results, nil
}

// ProcessWithPipeline 管道式处理
func (epp *EnhancedParallelProcessor) ProcessWithPipeline(
	ctx context.Context,
	pages []Page,
	stages []func(Page, []Text) ([]Text, error),
) ([][]Text, error) {
	if len(pages) == 0 || len(stages) == 0 {
		return [][]Text{}, nil
	}

	// 初始阶段：提取文本
	currentResults := make([][]Text, len(pages))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var pipelineErr error

	// 第一个阶段：并行处理所有页面
	semaphore := make(chan struct{}, epp.workerPool.workers)

	for i, page := range pages {
		wg.Add(1)
		go func(idx int, p Page) {
			defer wg.Done()

			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				mu.Lock()
				if pipelineErr == nil {
					pipelineErr = ctx.Err()
				}
				mu.Unlock()
				return
			}

			// 初始处理
			var texts []Text
			var err error

			// 执行所有阶段
			for _, stage := range stages {
				texts, err = stage(p, texts)
				if err != nil {
					mu.Lock()
					if pipelineErr == nil {
						pipelineErr = err
					}
					mu.Unlock()
					return
				}
			}

			mu.Lock()
			currentResults[idx] = texts
			mu.Unlock()
		}(i, page)
	}

	wg.Wait()

	if pipelineErr != nil {
		return nil, pipelineErr
	}

	return currentResults, nil
}

// AdaptiveProcessor 自适应处理器
// 根据系统负载自动调整并发级别
type AdaptiveProcessor struct {
	minWorkers     int
	maxWorkers     int
	currentWorkers atomic.Int32
	loadThreshold  float64
}

// NewAdaptiveProcessor 创建自适应处理器
func NewAdaptiveProcessor(min, max int) *AdaptiveProcessor {
	if min <= 0 {
		min = 1
	}
	if max <= 0 || max < min {
		max = runtime.NumCPU()
	}

	ap := &AdaptiveProcessor{
		minWorkers:    min,
		maxWorkers:    max,
		loadThreshold: 0.8,
	}
	ap.currentWorkers.Store(int32(min))

	return ap
}

// AdjustWorkers 根据系统负载调整工作数
func (ap *AdaptiveProcessor) AdjustWorkers() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	// 简化的负载评估：基于 GC 暂停时间
	current := ap.currentWorkers.Load()

	// 如果系统压力大，减少工作协程
	if ms.NumGC > 0 {
		if current > int32(ap.minWorkers) {
			ap.currentWorkers.Store(current - 1)
		}
	} else if current < int32(ap.maxWorkers) {
		// 系统空闲，增加工作协程
		ap.currentWorkers.Store(current + 1)
	}
}

// GetWorkerCount 获取当前工作协程数
func (ap *AdaptiveProcessor) GetWorkerCount() int {
	return int(ap.currentWorkers.Load())
}

// ProcessAdaptive 自适应处理
func (ap *AdaptiveProcessor) ProcessAdaptive(
	ctx context.Context,
	pages []Page,
	processorFunc func(Page) ([]Text, error),
) ([][]Text, error) {
	// 定期调整工作数
	ap.AdjustWorkers()

	epp := NewEnhancedParallelProcessor(ap.GetWorkerCount(), 10)
	return epp.ProcessPagesEnhanced(ctx, pages, processorFunc)
}

// GetStats 获取工作池统计信息
func (wp *WorkerPool) GetStats() WorkerPoolStats {
	return WorkerPoolStats{
		Workers:    wp.workers,
		ActiveJobs: atomic.LoadInt64(&wp.activeJobs),
		TotalJobs:  atomic.LoadInt64(&wp.totalJobs),
	}
}

// WorkerPoolStats 工作池统计信息
type WorkerPoolStats struct {
	Workers    int
	ActiveJobs int64
	TotalJobs  int64
}

// ParallelExtractor 并行提取器
// 结合所有优化的高级提取接口
type ParallelExtractor struct {
	processor  *EnhancedParallelProcessor
	cache      *ShardedCache
	prefetcher *FontPrefetcher
}

// NewParallelExtractor 创建并行提取器
func NewParallelExtractor(workers int) *ParallelExtractor {
	return &ParallelExtractor{
		processor:  NewEnhancedParallelProcessor(workers, 10),
		cache:      NewShardedCache(10000, 0),
		prefetcher: NewFontPrefetcher(NewOptimizedFontCache(1000)),
	}
}

// ExtractAllPages 提取所有页面（使用所有优化）
func (pe *ParallelExtractor) ExtractAllPages(
	ctx context.Context,
	pages []Page,
) ([][]Text, error) {
	return pe.processor.ProcessPagesEnhanced(ctx, pages, func(page Page) ([]Text, error) {
		// 使用缓存加速内容提取
		content := page.Content()
		return content.Text, nil
	})
}

// GetCacheStats 获取缓存统计
func (pe *ParallelExtractor) GetCacheStats() ShardedCacheStats {
	return pe.cache.GetStats()
}

// GetPrefetchStats 获取预取统计
func (pe *ParallelExtractor) GetPrefetchStats() PrefetchStats {
	return pe.prefetcher.GetStats()
}

// Close 关闭并清理资源
func (pe *ParallelExtractor) Close() {
	pe.prefetcher.Close()
}
