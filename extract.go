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

// ExtractOptions configures text extraction behavior
type ExtractOptions struct {
	Workers   int   // Number of concurrent workers (0 = use NumCPU)
	PageRange []int // Specific pages to extract (nil = all pages)
}

// ExtractWithContext extracts plain text from all pages with cancellation support
func (r *Reader) ExtractWithContext(ctx context.Context, opts ExtractOptions) (io.Reader, error) {
	pages := r.NumPage()
	if pages == 0 {
		return emptyReader(), nil
	}

	// Set a reasonable object cache capacity to prevent unlimited growth
	// For concurrent page processing, limit cache to prevent memory explosion
	if r.GetCacheCapacity() <= 0 {
		cacheSize := len(opts.PageRange)
		if cacheSize == 0 {
			cacheSize = pages
		}
		cacheSize = cacheSize * 10
		if cacheSize > 5000 {
			cacheSize = 5000 // Cap at 5000 objects
		}
		r.SetCacheCapacity(cacheSize)
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers > pages {
		workers = pages
	}

	// Determine which pages to extract
	pageList := opts.PageRange
	if pageList == nil {
		pageList = make([]int, pages)
		for i := 0; i < pages; i++ {
			pageList[i] = i + 1
		}
	}

	results := make([]string, len(pageList))
	jobs := make(chan int, len(pageList))
	errCh := make(chan error, 1)

	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for idx := range jobs {
			select {
			case <-ctx.Done():
				return
			default:
			}

			pageNum := pageList[idx]
			text, err := r.Page(pageNum).GetPlainText(nil)
			if err != nil {
				select {
				case errCh <- wrapPageError("extract text", pageNum, err):
				default:
				}
				return
			}
			results[idx] = text
		}
	}

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go worker()
	}

	// Send jobs
	for i := range pageList {
		select {
		case <-ctx.Done():
			close(jobs)
			return nil, ctx.Err()
		case jobs <- i:
		}
	}
	close(jobs)

	// Wait for completion
	wg.Wait()

	// Check for errors
	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	// Combine results
	var buf writeBuffer
	for _, text := range results {
		buf.WriteString(text)
	}
	return &buf, nil
}

// writeBuffer is a simple io.Reader wrapper around strings
type writeBuffer struct {
	data   []string
	offset int
	pos    int
}

func (b *writeBuffer) WriteString(s string) {
	b.data = append(b.data, s)
}

func (b *writeBuffer) Read(p []byte) (n int, err error) {
	for b.offset < len(b.data) {
		s := b.data[b.offset]
		copied := copy(p[n:], s[b.pos:])
		n += copied
		b.pos += copied

		if b.pos >= len(s) {
			b.offset++
			b.pos = 0
		}

		if n >= len(p) {
			return n, nil
		}
	}

	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

func emptyReader() io.Reader {
	return &writeBuffer{}
}
