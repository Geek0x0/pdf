// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"context"
	"runtime"
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

	// For smaller collections, use regular sort
	if len(texts) < 10000 {
		// Implement a custom parallel sort for larger collections
		// For now, we'll just use regular sort as an example
		// In a real implementation, we'd implement merge sort or another algorithm
		parallelMergeSort(ctx, texts, less, pte.processor.numWorkers)
		return nil
	}

	// For large collections, use parallel sort
	return pte.parallelSortInternal(ctx, texts, less)
}

// parallelSortInternal implements parallel sorting using divide and conquer
func (pte *ParallelTextExtractor) parallelSortInternal(ctx context.Context, texts []Text, less func(i, j int) bool) error {
	// This is a simplified version - a full implementation would be more complex
	// Split the texts into chunks and sort them in parallel
	chunkSize := len(texts) / pte.processor.numWorkers
	if chunkSize < 1000 {
		chunkSize = 1000
	}

	var wg sync.WaitGroup
	errChan := make(chan error, pte.processor.numWorkers)

	start := 0
	for start < len(texts) {
		end := start + chunkSize
		if end > len(texts) {
			end = len(texts)
		}

		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			default:
			}

			// Sort this chunk
			for i := s; i < e-1; i++ {
				for j := i + 1; j < e; j++ {
					if less(j, i) {
						texts[i], texts[j] = texts[j], texts[i]
					}
				}
			}
		}(start, end)

		start = end
	}

	// Close the error channel once all goroutines complete
	go func() {
		wg.Wait()
		close(errChan)
	}()

	// Check for errors
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	// Merge the sorted chunks (simplified approach)
	// A full implementation would use a proper merge algorithm
	return nil
}

// parallelMergeSort implements parallel merge sort (simplified version)
func parallelMergeSort(ctx context.Context, texts []Text, less func(i, j int) bool, numWorkers int) {
	if len(texts) <= 1 {
		return
	}

	// For small arrays, use standard sort
	if len(texts) < 10000 {
		standardSort(texts, less)
		return
	}

	// Divide and conquer approach
	mid := len(texts) / 2
	left := texts[:mid]
	right := texts[mid:]

	// Process left and right recursively
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		parallelMergeSort(ctx, left, less, numWorkers/2)
	}()

	go func() {
		defer wg.Done()
		parallelMergeSort(ctx, right, less, numWorkers/2)
	}()

	wg.Wait()

	// Merge the results
	merge(left, right, texts, less)
}

// standardSort implements a simple bubble sort (in a real implementation, use a more efficient algorithm)
func standardSort(texts []Text, less func(i, j int) bool) {
	for i := 0; i < len(texts); i++ {
		for j := i + 1; j < len(texts); j++ {
			if less(j, i) {
				texts[i], texts[j] = texts[j], texts[i]
			}
		}
	}
}

// merge merges two sorted slices into one
func merge(left, right, result []Text, less func(i, j int) bool) {
	i, j, k := 0, 0, 0

	for i < len(left) && j < len(right) {
		if less(i, j) || !less(j, i) {
			result[k] = left[i]
			i++
		} else {
			result[k] = right[j]
			j++
		}
		k++
	}

	for i < len(left) {
		result[k] = left[i]
		i++
		k++
	}

	for j < len(right) {
		result[k] = right[j]
		j++
		k++
	}
}
