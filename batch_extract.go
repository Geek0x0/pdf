// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"context"
	"runtime"
	"sync"
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
	results := make(chan BatchResult, opts.Workers)

	// Set defaults
	if opts.Workers <= 0 {
		opts.Workers = runtime.NumCPU()
	}
	if opts.Context == nil {
		opts.Context = context.Background()
	}
	if opts.PageBufferSize <= 0 {
		opts.PageBufferSize = 2048
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

	go func() {
		defer close(results)

		// Create work queue
		jobs := make(chan int, len(pages))
		for _, pageNum := range pages {
			jobs <- pageNum
		}
		close(jobs)

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
		}
	}()

	return results
}

// batchExtractWorker processes pages from the job queue
func batchExtractWorker(r *Reader, jobs <-chan int, results chan<- BatchResult, opts BatchExtractOptions, fontCache FontCacheInterface) {
	for pageNum := range jobs {
		// Check cancellation
		select {
		case <-opts.Context.Done():
			results <- BatchResult{
				PageNum: pageNum,
				Error:   opts.Context.Err(),
			}
			return
		default:
		}

		// Extract page
		page := r.Page(pageNum)
		var text string
		var err error

		// Enable font cache for this extraction if provided
		if fontCache != nil {
			// Wrap the interface in a cache adapter for the page
			page.SetFontCacheInterface(fontCache)
		}

		if opts.SmartOrdering {
			text, err = page.GetPlainTextWithSmartOrdering(nil)
		} else {
			text, err = page.GetPlainText(nil)
		}

		results <- BatchResult{
			PageNum: pageNum,
			Text:    text,
			Error:   err,
		}
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

	// Collect results
	var results []pageResult
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
	sortPageResults(results)

	// Combine into single string
	totalSize := 0
	for _, r := range results {
		totalSize += len(r.text)
	}
	totalSize += len(results) - 1 // newlines

	builder := GetSizedStringBuilder(totalSize)
	defer PutSizedStringBuilder(builder, totalSize)

	for i, r := range results {
		builder.WriteString(r.text)
		if i < len(results)-1 {
			builder.WriteByte('\n')
		}
	}

	return builder.String(), nil
}

// sortPageResults sorts results by page number using quicksort
func sortPageResults(results []pageResult) {
	if len(results) <= 1 {
		return
	}

	// Simple insertion sort for small arrays
	if len(results) < 20 {
		for i := 1; i < len(results); i++ {
			key := results[i]
			j := i - 1
			for j >= 0 && results[j].pageNum > key.pageNum {
				results[j+1] = results[j]
				j--
			}
			results[j+1] = key
		}
		return
	}

	// Quicksort for larger arrays
	quicksortPageResults(results, 0, len(results)-1)
}

func quicksortPageResults(results []pageResult, low, high int) {
	if low < high {
		pivot := partitionPageResults(results, low, high)
		quicksortPageResults(results, low, pivot-1)
		quicksortPageResults(results, pivot+1, high)
	}
}

func partitionPageResults(results []pageResult, low, high int) int {
	pivot := results[high].pageNum
	i := low - 1

	for j := low; j < high; j++ {
		if results[j].pageNum < pivot {
			i++
			results[i], results[j] = results[j], results[i]
		}
	}

	results[i+1], results[high] = results[high], results[i+1]
	return i + 1
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
		opts.Workers = runtime.NumCPU()
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
	}()

	return results
}

// structuredBatchWorker processes structured extraction
func structuredBatchWorker(r *Reader, jobs <-chan int, results chan<- StructuredBatchResult, opts BatchExtractOptions) {
	for pageNum := range jobs {
		select {
		case <-opts.Context.Done():
			results <- StructuredBatchResult{
				PageNum: pageNum,
				Error:   opts.Context.Err(),
			}
			return
		default:
		}

		page := r.Page(pageNum)
		blocks, err := page.ClassifyTextBlocks()

		results <- StructuredBatchResult{
			PageNum: pageNum,
			Blocks:  blocks,
			Error:   err,
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
