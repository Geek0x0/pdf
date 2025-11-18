// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"context"
	"runtime"
	"sort"
	"sync"
)

// ParallelProcessor handles multi-level parallel processing for PDF text extraction
type ParallelProcessor struct {
	numWorkers int
}

// NewParallelProcessor creates a new parallel processor with the specified number of workers
func NewParallelProcessor(workers int) *ParallelProcessor {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	return &ParallelProcessor{numWorkers: workers}
}

// ProcessPages processes multiple pages in parallel
func (pp *ParallelProcessor) ProcessPages(ctx context.Context, pages []Page, processorFunc func(Page) ([]Text, error)) ([][]Text, error) {
	if len(pages) == 0 {
		return [][]Text{}, nil
	}

	numWorkers := pp.numWorkers
	if numWorkers > len(pages) {
		numWorkers = len(pages)
	}

	// Create channels for work distribution
	jobChan := make(chan struct {
		index int
		page  Page
	}, len(pages))
	resultChan := make(chan struct {
		index int
		texts []Text
		err   error
	}, len(pages))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobChan {
				select {
				case <-ctx.Done():
					resultChan <- struct {
						index int
						texts []Text
						err   error
					}{index: job.index, texts: nil, err: ctx.Err()}
					return
				default:
				}

				texts, err := processorFunc(job.page)
				resultChan <- struct {
					index int
					texts []Text
					err   error
				}{index: job.index, texts: texts, err: err}
			}
		}()
	}

	// Send jobs
	go func() {
		defer close(jobChan)
		for i, page := range pages {
			select {
			case <-ctx.Done():
				return
			case jobChan <- struct {
				index int
				page  Page
			}{index: i, page: page}:
			}
		}
	}()

	// Collect results
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	results := make([][]Text, len(pages))
	for result := range resultChan {
		if result.err != nil {
			return nil, result.err
		}
		results[result.index] = result.texts
	}

	return results, nil
}

// ProcessTextBlocks processes multiple text blocks in parallel
func (pp *ParallelProcessor) ProcessTextBlocks(ctx context.Context, blocks []*TextBlock, processorFunc func(*TextBlock) (*TextBlock, error)) ([]*TextBlock, error) {
	if len(blocks) == 0 {
		return []*TextBlock{}, nil
	}

	numWorkers := pp.numWorkers
	if numWorkers > len(blocks) {
		numWorkers = len(blocks)
	}

	// Create channels for work distribution
	jobChan := make(chan struct {
		index int
		block *TextBlock
	}, len(blocks))
	resultChan := make(chan struct {
		index int
		block *TextBlock
		err   error
	}, len(blocks))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobChan {
				select {
				case <-ctx.Done():
					resultChan <- struct {
						index int
						block *TextBlock
						err   error
					}{index: job.index, block: nil, err: ctx.Err()}
					return
				default:
				}

				processedBlock, err := processorFunc(job.block)
				resultChan <- struct {
					index int
					block *TextBlock
					err   error
				}{index: job.index, block: processedBlock, err: err}
			}
		}()
	}

	// Send jobs
	go func() {
		defer close(jobChan)
		for i, block := range blocks {
			select {
			case <-ctx.Done():
				return
			case jobChan <- struct {
				index int
				block *TextBlock
			}{index: i, block: cloneTextBlock(block)}:
			}
		}
	}()

	// Collect results
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	results := make([]*TextBlock, len(blocks))
	for result := range resultChan {
		if result.err != nil {
			return nil, result.err
		}
		results[result.index] = result.block
	}

	return results, nil
}

// cloneTextBlock makes a shallow copy of the TextBlock and a deep copy of the Text slice
// so that processors don't mutate the caller's data.
func cloneTextBlock(block *TextBlock) *TextBlock {
	if block == nil {
		return nil
	}
	copied := *block
	if len(block.Texts) > 0 {
		copiedTexts := make([]Text, len(block.Texts))
		copy(copiedTexts, block.Texts)
		copied.Texts = copiedTexts
	}
	return &copied
}

// ProcessTextInParallel processes individual text elements in parallel
func (pp *ParallelProcessor) ProcessTextInParallel(ctx context.Context, texts []Text, processorFunc func(Text) (Text, error)) ([]Text, error) {
	if len(texts) == 0 {
		return []Text{}, nil
	}

	numWorkers := pp.numWorkers
	if numWorkers > len(texts) {
		numWorkers = len(texts)
	}

	// For character-level processing, we may want to use smaller batches
	// to avoid creating too many goroutines
	if len(texts) < numWorkers {
		numWorkers = len(texts)
	}

	// Create channels for work distribution
	jobChan := make(chan struct {
		index int
		text  Text
	}, len(texts))
	resultChan := make(chan struct {
		index int
		text  Text
		err   error
	}, len(texts))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobChan {
				select {
				case <-ctx.Done():
					resultChan <- struct {
						index int
						text  Text
						err   error
					}{index: job.index, text: Text{}, err: ctx.Err()}
					return
				default:
				}

				processedText, err := processorFunc(job.text)
				resultChan <- struct {
					index int
					text  Text
					err   error
				}{index: job.index, text: processedText, err: err}
			}
		}()
	}

	// Send jobs
	go func() {
		defer close(jobChan)
		for i, text := range texts {
			select {
			case <-ctx.Done():
				return
			case jobChan <- struct {
				index int
				text  Text
			}{index: i, text: text}:
			}
		}
	}()

	// Collect results
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	results := make([]Text, len(texts))
	for result := range resultChan {
		if result.err != nil {
			return nil, result.err
		}
		results[result.index] = result.text
	}

	return results, nil
}

// ParallelTextExtractor provides multi-level parallel extraction
type ParallelTextExtractor struct {
	processor *ParallelProcessor
}

// NewParallelTextExtractor creates a new parallel text extractor
func NewParallelTextExtractor(workers int) *ParallelTextExtractor {
	return &ParallelTextExtractor{
		processor: NewParallelProcessor(workers),
	}
}

// ExtractWithParallelProcessing extracts text using multi-level parallel processing
func (pte *ParallelTextExtractor) ExtractWithParallelProcessing(ctx context.Context, reader *Reader) ([]Text, error) {
	totalPages := reader.NumPage()
	if totalPages == 0 {
		return []Text{}, nil
	}

	// Get all pages
	pages := make([]Page, totalPages)
	for i := 1; i <= totalPages; i++ {
		pages[i-1] = reader.Page(i)
	}

	// Process pages in parallel
	pageTexts, err := pte.processor.ProcessPages(ctx, pages, func(page Page) ([]Text, error) {
		return page.Content().Text, nil
	})
	if err != nil {
		return nil, err
	}

	// Flatten results
	var allTexts []Text
	for _, texts := range pageTexts {
		allTexts = append(allTexts, texts...)
	}

	// Process text blocks in parallel (for classification, ordering, etc.)
	textBlocks := clusterTextBlocks(allTexts)

	// Process blocks in parallel if we have many
	if len(textBlocks) > 1 {
		processedBlocks, err := pte.processor.ProcessTextBlocks(ctx, textBlocks, func(block *TextBlock) (*TextBlock, error) {
			// In a real implementation, this might perform classification or other processing
			return block, nil
		})
		if err != nil {
			return nil, err
		}

		// Flatten processed blocks back to texts
		var resultTexts []Text
		for _, block := range processedBlocks {
			resultTexts = append(resultTexts, block.Texts...)
		}
		return resultTexts, nil
	}

	// If we don't have many blocks, just return the original texts
	return allTexts, nil
}

// ParallelSort provides parallel sorting for large text collections
func (pte *ParallelTextExtractor) ParallelSort(ctx context.Context, texts []Text, less func(i, j int) bool) error {
	if len(texts) <= 1 {
		return nil
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	// For smaller collections, use the standard library sort for better efficiency
	if len(texts) < 10000 {
		sort.Slice(texts, less)
		return ctx.Err()
	}

	workers := pte.processor.numWorkers
	if workers < 2 {
		workers = 2
	}

	chunkSize := (len(texts) + workers - 1) / workers
	if chunkSize < 1000 {
		chunkSize = 1000
	}
	if chunkSize > len(texts) {
		chunkSize = len(texts)
	}

	type job struct {
		start int
		end   int
	}

	jobs := make(chan job)
	errChan := make(chan error, 1)
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		for job := range jobs {
			if err := ctx.Err(); err != nil {
				select {
				case errChan <- err:
				default:
				}
				return
			}
			sort.Slice(texts[job.start:job.end], func(i, j int) bool {
				return less(job.start+i, job.start+j)
			})
		}
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker()
	}

	for start := 0; start < len(texts); start += chunkSize {
		end := start + chunkSize
		if end > len(texts) {
			end = len(texts)
		}

		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		case jobs <- job{start: start, end: end}:
		}
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errChan:
		if err != nil {
			return err
		}
	default:
	}

	temp := make([]Text, len(texts))
	if err := mergeChunksWithContext(ctx, texts, temp, chunkSize, less); err != nil {
		return err
	}
	return ctx.Err()
}

// mergeChunksWithContext merges sorted chunks while honoring context cancellation.
func mergeChunksWithContext(ctx context.Context, texts, temp []Text, chunkSize int, less func(i, j int) bool) error {
	if chunkSize <= 0 || chunkSize >= len(texts) {
		return ctx.Err()
	}

	for currentSize := chunkSize; currentSize < len(texts); currentSize *= 2 {
		if err := ctx.Err(); err != nil {
			return err
		}

		for start := 0; start < len(texts); start += 2 * currentSize {
			mid := start + currentSize
			if mid > len(texts) {
				mid = len(texts)
			}
			end := start + 2*currentSize
			if end > len(texts) {
				end = len(texts)
			}

			if mid >= end {
				continue
			}

			mergeRanges(texts, temp, start, mid, end, less)
			copy(texts[start:end], temp[start:end])
		}
	}

	return ctx.Err()
}

// mergeRanges merges two sorted ranges [start,mid) and [mid,end) into temp.
func mergeRanges(texts, temp []Text, start, mid, end int, less func(i, j int) bool) {
	i, j, k := start, mid, start

	for i < mid && j < end {
		if less(i, j) || !less(j, i) {
			temp[k] = texts[i]
			i++
		} else {
			temp[k] = texts[j]
			j++
		}
		k++
	}

	for i < mid {
		temp[k] = texts[i]
		i++
		k++
	}
	for j < end {
		temp[k] = texts[j]
		j++
		k++
	}
}
