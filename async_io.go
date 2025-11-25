// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"context"
	"io"
	"runtime"
	"sync"
)

// AsyncReader wraps a Reader to provide asynchronous operations
type AsyncReader struct {
	*Reader
	processor *ParallelProcessor
}

// NewAsyncReader creates a new async reader with async I/O support
func NewAsyncReader(reader *Reader) *AsyncReader {
	return &AsyncReader{
		Reader:    reader,
		processor: NewParallelProcessor(runtime.NumCPU()),
	}
}

func (ar *AsyncReader) workerCount() int {
	if ar == nil || ar.processor == nil || ar.processor.numWorkers <= 0 {
		return runtime.NumCPU()
	}
	return ar.processor.numWorkers
}

// AsyncExtractText extracts text from all pages asynchronously
func (ar *AsyncReader) AsyncExtractText(ctx context.Context) (<-chan string, <-chan error) {
	resultChan := make(chan string, 1)
	errChan := make(chan error, 1)

	go func() {
		defer close(resultChan)
		defer close(errChan)

		totalPages := ar.NumPage()
		if totalPages == 0 {
			select {
			case resultChan <- "":
			case <-ctx.Done():
				errChan <- ctx.Err()
			}
			return
		}

		workers := ar.workerCount()
		if workers > totalPages {
			workers = totalPages
		}

		// Use sufficiently large buffer to prevent goroutine blocking
		pageResults := make(chan struct {
			pageNum int
			text    string
			err     error
		}, totalPages) // buffer size changed to totalPages, ensure all results can be sent
		workChan := make(chan int, workers) // work channel also buffered
		var wg sync.WaitGroup

		worker := func() {
			defer wg.Done()
			for pageNum := range workChan {
				select {
				case <-ctx.Done():
					select {
					case pageResults <- struct {
						pageNum int
						text    string
						err     error
					}{pageNum: pageNum, text: "", err: ctx.Err()}:
					default:
						// Channel full or closed
					}
					continue
				default:
				}

				page := ar.Page(pageNum)
				text, err := page.GetPlainText(nil)

				// Use select to ensure no blocking
				select {
				case pageResults <- struct {
					pageNum int
					text    string
					err     error
				}{pageNum: pageNum, text: text, err: err}:
				case <-ctx.Done():
					// Context canceled, stop sending
				}
			}
		}

		for i := 0; i < workers; i++ {
			wg.Add(1)
			go worker()
		}

		go func() {
			defer close(workChan)
			for pageNum := 1; pageNum <= totalPages; pageNum++ {
				select {
				case <-ctx.Done():
					return
				case workChan <- pageNum:
				}
			}
		}()

		go func() {
			wg.Wait()
			close(pageResults)
		}()

		// Collect and combine results
		pageTexts := make([]string, totalPages)
		var firstErr error

		for result := range pageResults {
			if result.err != nil {
				if firstErr == nil {
					firstErr = result.err
				}
				continue
			}
			pageTexts[result.pageNum-1] = result.text
		}

		if firstErr == nil {
			firstErr = ctx.Err()
		}
		if firstErr != nil {
			select {
			case errChan <- firstErr:
			case <-ctx.Done():
			}
			return
		}

		// Calculate total length
		totalLen := 0
		for _, text := range pageTexts {
			totalLen += len(text)
		}

		// Use builder from object pool
		builder := GetSizedStringBuilder(totalLen + len(pageTexts))
		defer PutSizedStringBuilder(builder, totalLen+len(pageTexts))

		for _, text := range pageTexts {
			builder.WriteString(text)
		}
		combinedText := builder.String()

		select {
		case resultChan <- combinedText:
		case <-ctx.Done():
			errChan <- ctx.Err()
		}
	}()

	return resultChan, errChan
}

// AsyncExtractTextWithContext extracts text with cancellation and timeout support
func (ar *AsyncReader) AsyncExtractTextWithContext(ctx context.Context, opts ExtractOptions) (<-chan string, <-chan error) {
	resultChan := make(chan string, 1)
	errChan := make(chan error, 1)

	go func() {
		defer close(resultChan)
		defer close(errChan)

		// Respect contexts that are already cancelled before work begins.
		if err := ctx.Err(); err != nil {
			select {
			case errChan <- err:
			default:
			}
			return
		}

		// Determine which pages to extract
		pageList := opts.PageRange
		if pageList == nil {
			pages := ar.NumPage()
			pageList = make([]int, pages)
			for i := 0; i < pages; i++ {
				pageList[i] = i + 1
			}
		}

		// Create channels for async processing
		pageResults := make(chan struct {
			pageNum int
			text    string
			err     error
		}, len(pageList))

		workers := opts.Workers
		if workers <= 0 {
			workers = ar.workerCount()
		}

		// Launch page processing workers
		var wg sync.WaitGroup
		workChan := make(chan int, len(pageList))

		// Send work
		go func() {
			defer close(workChan)
			for _, pageNum := range pageList {
				select {
				case workChan <- pageNum:
				case <-ctx.Done():
					return
				}
			}
		}()

		// Launch worker goroutines
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for pageNum := range workChan {
					select {
					case <-ctx.Done():
						// Use select to prevent blocking
						select {
						case pageResults <- struct {
							pageNum int
							text    string
							err     error
						}{pageNum: pageNum, text: "", err: ctx.Err()}:
						default:
							// Channel full or closed
						}
						return
					default:
					}

					page := ar.Page(pageNum)
					text, err := page.GetPlainText(nil)

					// Use select to ensure no blocking
					select {
					case pageResults <- struct {
						pageNum int
						text    string
						err     error
					}{pageNum: pageNum, text: text, err: err}:
					case <-ctx.Done():
						// Context canceled, stop sending
						return
					}
				}
			}()
		}

		// Close pageResults when done
		go func() {
			wg.Wait()
			close(pageResults)
		}()

		// Collect results in order
		pageTexts := make(map[int]string)
		var firstErr error

		for result := range pageResults {
			if result.err != nil {
				if firstErr == nil {
					firstErr = result.err
				}
				continue
			}
			pageTexts[result.pageNum] = result.text
		}

		if firstErr == nil {
			firstErr = ctx.Err()
		}
		if firstErr != nil {
			select {
			case errChan <- firstErr:
			case <-ctx.Done():
			}
			return
		}

		// Calculate total length
		totalLen := 0
		for _, text := range pageTexts {
			totalLen += len(text)
		}

		// Use builder from object pool
		builder := GetSizedStringBuilder(totalLen)
		defer PutSizedStringBuilder(builder, totalLen)

		for _, pageNum := range pageList {
			builder.WriteString(pageTexts[pageNum])
		}
		combinedText := builder.String()

		select {
		case resultChan <- combinedText:
		case <-ctx.Done():
			errChan <- ctx.Err()
		}
	}()

	return resultChan, errChan
}

// AsyncExtractStructured extracts structured text asynchronously
func (ar *AsyncReader) AsyncExtractStructured(ctx context.Context) (<-chan []ClassifiedBlock, <-chan error) {
	resultChan := make(chan []ClassifiedBlock, 1)
	errChan := make(chan error, 1)

	go func() {
		defer close(resultChan)
		defer close(errChan)

		totalPages := ar.NumPage()
		if totalPages == 0 {
			select {
			case resultChan <- []ClassifiedBlock{}:
			case <-ctx.Done():
				errChan <- ctx.Err()
			}
			return
		}

		// Process each page asynchronously - use buffered channel to prevent blocking
		pageResults := make(chan struct {
			pageNum int
			blocks  []ClassifiedBlock
			err     error
		}, totalPages) // buffer size equals total pages, prevent goroutine blocking

		var wg sync.WaitGroup
		for i := 1; i <= totalPages; i++ {
			wg.Add(1)
			go func(pageNum int) {
				defer wg.Done()

				select {
				case <-ctx.Done():
					// Use select to ensure results can be sent even if context is canceled
					select {
					case pageResults <- struct {
						pageNum int
						blocks  []ClassifiedBlock
						err     error
					}{pageNum: pageNum, blocks: nil, err: ctx.Err()}:
					default:
						// Channel full or closed, return directly
					}
					return
				default:
				}

				page := ar.Page(pageNum)
				blocks, err := page.ClassifyTextBlocks()

				// Use select to ensure sending does not block
				select {
				case pageResults <- struct {
					pageNum int
					blocks  []ClassifiedBlock
					err     error
				}{pageNum: pageNum, blocks: blocks, err: err}:
				case <-ctx.Done():
					// Context canceled, no longer send
				}
			}(i)
		}

		// Close results when done
		go func() {
			wg.Wait()
			close(pageResults)
		}()

		// Collect all blocks
		var allBlocks []ClassifiedBlock
		var firstErr error

		for result := range pageResults {
			if result.err != nil {
				if firstErr == nil {
					firstErr = result.err
				}
				continue
			}
			allBlocks = append(allBlocks, result.blocks...)
		}

		if firstErr != nil {
			select {
			case errChan <- firstErr:
			case <-ctx.Done():
			}
			return
		}

		select {
		case resultChan <- allBlocks:
		case <-ctx.Done():
			errChan <- ctx.Err()
		}
	}()

	return resultChan, errChan
}

// AsyncStream processes the PDF file with async I/O operations
func (ar *AsyncReader) AsyncStream(ctx context.Context, processor func(Page, int) error) <-chan error {
	errChan := make(chan error, 1)

	go func() {
		defer close(errChan)

		totalPages := ar.NumPage()
		for i := 1; i <= totalPages; i++ {
			select {
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			default:
			}

			page := ar.Page(i)
			if err := processor(page, i); err != nil {
				errChan <- err
				return
			}
		}

		select {
		case errChan <- nil: // No error
		case <-ctx.Done():
			errChan <- ctx.Err()
		}
	}()

	return errChan
}

// AsyncReaderAt provides async I/O for low-level file operations
type AsyncReaderAt struct {
	reader io.ReaderAt
}

// NewAsyncReaderAt creates a new async reader with async I/O support
func NewAsyncReaderAt(reader io.ReaderAt) *AsyncReaderAt {
	return &AsyncReaderAt{
		reader: reader,
	}
}

// ReadAtAsync reads from the file asynchronously
func (ara *AsyncReaderAt) ReadAtAsync(ctx context.Context, buf []byte, offset int64) (<-chan int, <-chan error) {
	nChan := make(chan int, 1)
	errChan := make(chan error, 1)

	go func() {
		defer close(nChan)
		defer close(errChan)

		select {
		case <-ctx.Done():
			errChan <- ctx.Err()
			return
		default:
		}

		n, err := ara.reader.ReadAt(buf, offset)
		if err != nil {
			select {
			case errChan <- err:
			case <-ctx.Done():
			}
			return
		}

		select {
		case nChan <- n:
		case <-ctx.Done():
			errChan <- ctx.Err()
		}
	}()

	return nChan, errChan
}

// StreamValueReader provides async streaming of value data
func (ar *AsyncReader) StreamValueReader(ctx context.Context, v Value) (<-chan []byte, <-chan error) {
	dataChan := make(chan []byte, 10) // Buffered channel for streaming data
	errChan := make(chan error, 1)

	go func() {
		defer close(dataChan)
		defer close(errChan)

		reader := v.Reader()
		defer reader.Close()

		buf := make([]byte, 4096) // 4KB chunks
		for {
			select {
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			default:
			}

			n, err := reader.Read(buf)
			if n > 0 {
				// Create a copy of the buffer to send
				data := make([]byte, n)
				copy(data, buf[:n])

				select {
				case dataChan <- data:
				case <-ctx.Done():
					errChan <- ctx.Err()
					return
				}
			}

			if err != nil {
				if err == io.EOF {
					// Successfully completed
					select {
					case errChan <- nil:
					case <-ctx.Done():
					}
					return
				} else {
					// Error occurred
					select {
					case errChan <- err:
					case <-ctx.Done():
					}
					return
				}
			}
		}
	}()

	return dataChan, errChan
}
