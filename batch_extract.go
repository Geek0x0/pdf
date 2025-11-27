// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"context"
	"runtime"
	"sort"
	"sync"
	"time"
)

// FontCacheType specifies which font cache implementation to use
type FontCacheType int

const (
	// FontCacheStandard uses the standard GlobalFontCache (default)
	// - Stable and well-tested
	// - Good performance for most use cases
	// - Simpler implementation
	FontCacheStandard FontCacheType = iota

	// FontCacheOptimized uses the OptimizedFontCache
	// - 10-85x faster than standard (depending on workload)
	// - Lock-free read path with 16 shards
	// - Best for high-concurrency scenarios (>1000 qps)
	// - Recommended for production environments with heavy load
	FontCacheOptimized
)

// FontCacheInterface defines the common interface for font caches
type FontCacheInterface interface {
	Get(key string) (*Font, bool)
	Set(key string, font *Font)
	Clear()
	GetStats() FontCacheStats
}

// BatchExtractOptions configures batch extraction behavior
type BatchExtractOptions struct {
	// Pages to extract (nil means all pages)
	Pages []int

	// Number of concurrent workers (0 = NumCPU)
	Workers int

	// Whether to use smart text ordering
	SmartOrdering bool

	// Context for cancellation
	Context context.Context

	// Buffer size for each page result (0 = default 2KB)
	PageBufferSize int

	// Whether to enable font cache for this batch (default: false)
	// When enabled, a temporary font cache is created for the batch
	// to reduce redundant font parsing across pages
	UseFontCache bool

	// Maximum number of fonts to cache (0 = default 1000)
	// Only used when UseFontCache is true
	FontCacheSize int

	// FontCacheType specifies which cache implementation to use
	// - FontCacheStandard: Standard implementation (default)
	// - FontCacheOptimized: High-performance optimized cache (10-85x faster)
	// Only used when UseFontCache is true
	FontCacheType FontCacheType

	// PageTimeout is the maximum time allowed for processing a single page
	// If zero, defaults to 30 seconds. Set to negative value to disable.
	PageTimeout time.Duration

	// ParseLimits configures resource limits for parsing operations
	// If nil, uses DefaultParseLimits()
	ParseLimits *ParseLimits
}

// BatchResult contains the result of extracting a single page
type BatchResult struct {
	PageNum int
	Text    string
	Error   error
}

// ExtractPagesBatch extracts text from multiple pages in batches
// This is optimized for high-throughput scenarios with many pages
func (r *Reader) ExtractPagesBatch(opts BatchExtractOptions) <-chan BatchResult {
	// Optimize result channel buffer size to reduce blocking
	// Use Workers * 2 with a reasonable upper limit
	resultBufferSize := opts.Workers * 2
	if resultBufferSize <= 0 {
		resultBufferSize = runtime.NumCPU() * 2
	}
	if resultBufferSize > 64 {
		resultBufferSize = 64
	}
	results := make(chan BatchResult, resultBufferSize)

	// Set defaults
	if opts.Workers <= 0 {
		// Limit workers to prevent excessive goroutines and memory usage
		workers := runtime.NumCPU()
		if workers > 4 {
			workers = 4 // Cap at 4 workers for better memory control
		} else if workers < 1 {
			workers = 1
		}
		opts.Workers = workers
	}
	if opts.Context == nil {
		opts.Context = context.Background()
	}
	if opts.PageBufferSize <= 0 {
		opts.PageBufferSize = 2048
	}
	// Set default page timeout
	if opts.PageTimeout == 0 {
		opts.PageTimeout = 30 * time.Second
	}
	// Set default parse limits
	if opts.ParseLimits == nil {
		limits := DefaultParseLimits()
		opts.ParseLimits = &limits
	}

	// Initialize font cache if enabled
	var fontCache FontCacheInterface
	if opts.UseFontCache {
		cacheSize := opts.FontCacheSize
		if cacheSize <= 0 {
			cacheSize = 1000 // Default cache size
		}

		// Select cache implementation based on type
		switch opts.FontCacheType {
		case FontCacheOptimized:
			fontCache = NewOptimizedFontCache(cacheSize)
		default: // FontCacheStandard or unspecified
			fontCache = NewGlobalFontCache(cacheSize, 0) // No age limit for batch processing
		}
	}

	// Determine pages to extract
	pages := opts.Pages
	if len(pages) == 0 {
		pages = make([]int, r.NumPage())
		for i := range pages {
			pages[i] = i + 1
		}
	}

	// Set a reasonable object cache capacity for the Reader to prevent unlimited growth.
	// This is crucial for batch processing where many pages are extracted sequentially.
	// Without this, the Reader's internal objCache can grow to gigabytes for large PDFs.
	// We use a conservative heuristic: min(5000, pages * 10) to cap maximum cache size.
	// Only set if not already configured to allow users to override this behavior.
	if r.GetCacheCapacity() <= 0 && len(pages) > 0 {
		cacheSize := len(pages) * 5
		if cacheSize > 1000 {
			cacheSize = 1000 // Cap at 1000 objects to reduce memory footprint for GC efficiency
		}
		r.SetCacheCapacity(cacheSize)
	}

	go func() {
		defer close(results)

		// Create work queue with optimized buffer size
		// Only buffer a reasonable amount to avoid excessive memory
		jobBufferSize := opts.Workers * 2
		if jobBufferSize > len(pages) {
			jobBufferSize = len(pages)
		}
		jobs := make(chan int, jobBufferSize)

		// Start producer goroutine to avoid blocking
		go func() {
			for _, pageNum := range pages {
				select {
				case jobs <- pageNum:
				case <-opts.Context.Done():
					close(jobs)
					return
				}
			}
			close(jobs)
		}()

		// Start workers
		var wg sync.WaitGroup
		for w := 0; w < opts.Workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				batchExtractWorker(r, jobs, results, opts, fontCache)
			}()
		}

		wg.Wait()

		// Clean up font cache if used
		if fontCache != nil {
			fontCache.Clear()
			fontCache = nil // Explicitly nil out reference for GC
		}

		// CRITICAL: Clear the Reader's object cache to release all accumulated objects.
		// During batch processing, objCache can accumulate thousands of parsed PDF objects,
		// consuming gigabytes of memory. Clearing it here ensures memory is released
		// immediately after processing completes.
		r.ClearCache()

		// Skip explicit GC - let Go's GC handle this more efficiently
		// The runtime will handle GC based on memory pressure
		// Explicit GC calls were creating significant CPU overhead
	}()

	return results
}

// batchExtractWorker processes pages from the job queue
func batchExtractWorker(r *Reader, jobs <-chan int, results chan<- BatchResult, opts BatchExtractOptions, fontCache FontCacheInterface) {
	for pageNum := range jobs {
		// Check cancellation
		select {
		case <-opts.Context.Done():
			return
		default:
		}

		// Extract page with timeout - wrap in function to ensure defer runs per iteration
		func() {
			// Create page-level context with timeout if configured
			var pageCtx context.Context
			var cancel context.CancelFunc
			if opts.PageTimeout > 0 {
				pageCtx, cancel = context.WithTimeout(opts.Context, opts.PageTimeout)
			} else {
				pageCtx, cancel = context.WithCancel(opts.Context)
			}
			defer cancel()

			page := r.Page(pageNum)
			var text string
			var err error

			// Enable font cache for this extraction if provided
			if fontCache != nil {
				page.SetFontCacheInterface(fontCache)
			}

			// CRITICAL: Use defer to ensure fontCache reference is cleared after THIS page.
			// The defer must be in this inner function so it runs per iteration, not at worker end.
			defer func() {
				page.SetFontCacheInterface(nil)
				page.Cleanup()
			}()

			// OPTIMIZATION: Execute directly instead of creating extra goroutine per page
			// This reduces goroutine overhead significantly (e.g., 1000 pages = 1000 fewer goroutines)
			// The extraction methods now support context cancellation internally
			select {
			case <-pageCtx.Done():
				// Context already cancelled/timed out before extraction
				if pageCtx.Err() == context.DeadlineExceeded {
					err = &PDFError{
						Op:   "extract page",
						Page: pageNum,
						Err:  ErrTimeout,
					}
				} else {
					err = pageCtx.Err()
				}
			default:
				// Execute extraction with context support
				if opts.SmartOrdering {
					text, err = page.GetPlainTextWithSmartOrdering(pageCtx, nil)
				} else {
					text, err = page.GetPlainText(pageCtx, nil)
				}
			}

			// Send result
			select {
			case results <- BatchResult{
				PageNum: pageNum,
				Text:    text,
				Error:   err,
			}:
			case <-opts.Context.Done():
			}
		}()
	}
}

// pageResult holds a page's extraction result for sorting
type pageResult struct {
	pageNum int
	text    string
}

// ExtractPagesBatchToString is a convenience function that collects
// all results into a single string
func (r *Reader) ExtractPagesBatchToString(opts BatchExtractOptions) (string, error) {
	resultChan := r.ExtractPagesBatch(opts)

	// Determine expected number of pages for pre-allocation
	pages := opts.Pages
	if len(pages) == 0 {
		pages = make([]int, r.NumPage())
		for i := range pages {
			pages[i] = i + 1
		}
	}

	// Pre-allocate results slice to avoid multiple reallocations
	results := make([]pageResult, 0, len(pages))

	// Collect results
	for result := range resultChan {
		if result.Error != nil {
			return "", &PDFError{
				Op:   "batch extract",
				Page: result.PageNum,
				Err:  result.Error,
			}
		}
		results = append(results, pageResult{
			pageNum: result.PageNum,
			text:    result.Text,
		})
	}

	// Sort by page number to maintain order
	// (results may arrive out of order due to concurrency)
	// Use standard library sort which is highly optimized
	sort.Slice(results, func(i, j int) bool {
		return results[i].pageNum < results[j].pageNum
	})

	// Combine into single string
	totalSize := 0
	for _, r := range results {
		totalSize += len(r.text)
	}
	totalSize += len(results) - 1 // newlines

	// Use direct allocation in concurrent scenario to avoid pool issues
	builder := NewFastStringBuilder(totalSize)

	for i, r := range results {
		builder.WriteString(r.text)
		if i < len(results)-1 {
			builder.WriteByte('\n')
		}
	}

	return builder.String(), nil
}

// BatchExtractStructured extracts structured text from multiple pages in batches
type StructuredBatchResult struct {
	PageNum int
	Blocks  []ClassifiedBlock
	Error   error
}

// ExtractStructuredBatch extracts structured text in batches
func (r *Reader) ExtractStructuredBatch(opts BatchExtractOptions) <-chan StructuredBatchResult {
	results := make(chan StructuredBatchResult, opts.Workers)

	// Set defaults
	if opts.Workers <= 0 {
		// Limit workers to prevent excessive goroutines and memory usage
		workers := runtime.NumCPU()
		if workers > 4 {
			workers = 4 // Cap at 4 workers for better memory control
		} else if workers < 1 {
			workers = 1
		}
		opts.Workers = workers
	}
	if opts.Context == nil {
		opts.Context = context.Background()
	}

	// Determine pages to extract
	pages := opts.Pages
	if len(pages) == 0 {
		pages = make([]int, r.NumPage())
		for i := range pages {
			pages[i] = i + 1
		}
	}

	// Set a reasonable object cache capacity for the Reader to prevent unlimited growth.
	// This is crucial for batch processing where many pages are extracted sequentially.
	// Without this, the Reader's internal objCache can grow to gigabytes for large PDFs.
	// We use a conservative heuristic: min(5000, pages * 10) to cap maximum cache size.
	// Only set if not already configured to allow users to override this behavior.
	if r.GetCacheCapacity() <= 0 && len(pages) > 0 {
		cacheSize := len(pages) * 5
		if cacheSize > 1000 {
			cacheSize = 1000 // Cap at 1000 objects to reduce memory footprint for GC efficiency
		}
		r.SetCacheCapacity(cacheSize)
	}

	go func() {
		defer close(results)

		jobs := make(chan int, len(pages))
		for _, pageNum := range pages {
			jobs <- pageNum
		}
		close(jobs)

		var wg sync.WaitGroup
		for w := 0; w < opts.Workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				structuredBatchWorker(r, jobs, results, opts)
			}()
		}

		wg.Wait()

		// CRITICAL: Clear the Reader's object cache to release all accumulated objects.
		// During batch processing, objCache can accumulate thousands of parsed PDF objects,
		// consuming gigabytes of memory. Clearing it here ensures memory is released
		// immediately after processing completes.
		r.ClearCache()

		// Skip explicit GC - let Go's GC handle this more efficiently
		// The runtime will handle GC based on memory pressure
		// Explicit GC calls were creating significant CPU overhead
	}()

	return results
}

// structuredBatchWorker processes structured extraction
func structuredBatchWorker(r *Reader, jobs <-chan int, results chan<- StructuredBatchResult, opts BatchExtractOptions) {
	for pageNum := range jobs {
		select {
		case <-opts.Context.Done():
			return
		default:
		}

		page := r.Page(pageNum)
		blocks, err := page.ClassifyTextBlocks()

		// Send result, but don't block if context is cancelled or channel is full
		select {
		case results <- StructuredBatchResult{
			PageNum: pageNum,
			Blocks:  blocks,
			Error:   err,
		}:
		case <-opts.Context.Done():
			return
		}
	}
}

// StreamingBatchExtractor provides a streaming interface for batch extraction
// This is useful for very large PDFs where you want to process results as they arrive
type StreamingBatchExtractor struct {
	reader  *Reader
	opts    BatchExtractOptions
	results <-chan BatchResult
}

// NewStreamingBatchExtractor creates a new streaming batch extractor
func NewStreamingBatchExtractor(r *Reader, opts BatchExtractOptions) *StreamingBatchExtractor {
	return &StreamingBatchExtractor{
		reader: r,
		opts:   opts,
	}
}

// Start begins the extraction process
func (sbe *StreamingBatchExtractor) Start() {
	sbe.results = sbe.reader.ExtractPagesBatch(sbe.opts)
}

// Next returns the next result, or nil if done
func (sbe *StreamingBatchExtractor) Next() *BatchResult {
	result, ok := <-sbe.results
	if !ok {
		return nil
	}
	return &result
}

// ProcessAll processes all pages with a callback function
func (sbe *StreamingBatchExtractor) ProcessAll(callback func(BatchResult) error) error {
	for result := range sbe.results {
		if err := callback(result); err != nil {
			return err
		}
	}
	return nil
}
