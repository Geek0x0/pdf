// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"context"
	"io"
	"runtime"
)

// ExtractMode specifies the type of extraction to perform
type ExtractMode int

const (
	ModePlain      ExtractMode = iota // Plain text extraction
	ModeStyled                        // Text with style information
	ModeStructured                    // Structured text with classification
)

// ExtractResult contains the results of text extraction
type ExtractResult struct {
	Text             string            // Plain text (for ModePlain)
	StyledTexts      []Text            // Styled texts (for ModeStyled)
	ClassifiedBlocks []ClassifiedBlock // Classified blocks (for ModeStructured)
	Metadata         Metadata          // Document metadata
	PageCount        int               // Total number of pages
}

// Extractor provides a builder pattern for configuring and executing extraction
type Extractor struct {
	reader        *Reader
	mode          ExtractMode
	workers       int
	pageRange     []int
	smartOrdering bool
	ctx           context.Context
}

// NewExtractor creates a new extractor for the given reader
func NewExtractor(r *Reader) *Extractor {
	return &Extractor{
		reader:        r,
		mode:          ModePlain,
		workers:       runtime.NumCPU(),
		smartOrdering: false,
		ctx:           context.Background(),
	}
}

// Mode sets the extraction mode
func (e *Extractor) Mode(mode ExtractMode) *Extractor {
	e.mode = mode
	return e
}

// Workers sets the number of concurrent workers
func (e *Extractor) Workers(n int) *Extractor {
	if n <= 0 {
		n = runtime.NumCPU()
	}
	e.workers = n
	return e
}

// Pages sets specific pages to extract (1-indexed)
func (e *Extractor) Pages(pages ...int) *Extractor {
	e.pageRange = pages
	return e
}

// SmartOrdering enables smart text ordering for multi-column layouts
func (e *Extractor) SmartOrdering(enabled bool) *Extractor {
	e.smartOrdering = enabled
	return e
}

// Context sets the context for cancellation
func (e *Extractor) Context(ctx context.Context) *Extractor {
	e.ctx = ctx
	return e
}

// Extract performs the extraction and returns the result
func (e *Extractor) Extract() (*ExtractResult, error) {
	result := &ExtractResult{
		PageCount: e.reader.NumPage(),
	}

	// Extract metadata
	meta, err := e.reader.GetMetadata()
	if err != nil {
		// Metadata extraction failure shouldn't block text extraction
		if DebugOn {
			println("failed to extract metadata:", err.Error())
		}
	}
	result.Metadata = meta

	// Determine which pages to extract
	pages := e.getPageNumbers()

	switch e.mode {
	case ModePlain:
		text, err := e.extractPlainText(pages)
		if err != nil {
			return nil, err
		}
		result.Text = text

	case ModeStyled:
		texts, err := e.extractStyledTexts(pages)
		if err != nil {
			return nil, err
		}
		result.StyledTexts = texts

	case ModeStructured:
		blocks, err := e.extractStructuredText(pages)
		if err != nil {
			return nil, err
		}
		result.ClassifiedBlocks = blocks
	}

	return result, nil
}

// ExtractText is a convenience method for extracting plain text
func (e *Extractor) ExtractText() (string, error) {
	e.mode = ModePlain
	result, err := e.Extract()
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

// ExtractStyledTexts is a convenience method for extracting styled texts
func (e *Extractor) ExtractStyledTexts() ([]Text, error) {
	e.mode = ModeStyled
	result, err := e.Extract()
	if err != nil {
		return nil, err
	}
	return result.StyledTexts, nil
}

// ExtractStructured is a convenience method for extracting structured text
func (e *Extractor) ExtractStructured() ([]ClassifiedBlock, error) {
	e.mode = ModeStructured
	result, err := e.Extract()
	if err != nil {
		return nil, err
	}
	return result.ClassifiedBlocks, nil
}

// getPageNumbers returns the list of page numbers to extract
func (e *Extractor) getPageNumbers() []int {
	if len(e.pageRange) > 0 {
		return e.pageRange
	}

	// Extract all pages
	pages := make([]int, e.reader.NumPage())
	for i := range pages {
		pages[i] = i + 1
	}
	return pages
}

// extractPlainText extracts plain text from specified pages
func (e *Extractor) extractPlainText(pages []int) (string, error) {
	if e.workers == 1 || len(pages) == 1 {
		// Single-threaded extraction
		return e.extractPlainTextSequential(pages)
	}

	// Use concurrent extraction with context
	opts := ExtractOptions{
		Workers:   e.workers,
		PageRange: pages,
	}

	reader, err := e.reader.ExtractWithContext(e.ctx, opts)
	if err != nil {
		return "", err
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// extractPlainTextSequential extracts text sequentially
func (e *Extractor) extractPlainTextSequential(pages []int) (string, error) {
	// Use zero-copy StringBuffer for performance optimization
	// Estimated capacity: average 2KB per page
	estimatedSize := len(pages) * 2048
	builder := GetSizedStringBuilder(estimatedSize)
	defer PutSizedStringBuilder(builder, estimatedSize)

	for i, pageNum := range pages {
		select {
		case <-e.ctx.Done():
			return builder.String(), e.ctx.Err()
		default:
		}

		page := e.reader.Page(pageNum)
		var text string
		var err error

		if e.smartOrdering {
			text, err = page.GetPlainTextWithSmartOrdering(nil)
		} else {
			text, err = page.GetPlainText(nil)
		}

		if err != nil {
			return "", &PDFError{
				Op:   "extract text",
				Page: pageNum,
				Err:  err,
			}
		}

		builder.WriteString(text)
		if i < len(pages)-1 {
			builder.WriteByte('\n')
		}

		// CRITICAL FIX: Cleanup page resources after extraction
		page.Cleanup()
	}

	// Return copy (because builder will be reused)
	return builder.String(), nil
}

// extractStyledTexts extracts styled texts from specified pages
func (e *Extractor) extractStyledTexts(pages []int) ([]Text, error) {
	var allTexts []Text

	for _, pageNum := range pages {
		select {
		case <-e.ctx.Done():
			return allTexts, e.ctx.Err()
		default:
		}

		page := e.reader.Page(pageNum)
		content := page.Content()

		allTexts = append(allTexts, content.Text...)

		// CRITICAL FIX: Cleanup page resources
		page.Cleanup()
	}

	return allTexts, nil
}

// extractStructuredText extracts and classifies text from specified pages
func (e *Extractor) extractStructuredText(pages []int) ([]ClassifiedBlock, error) {
	var allBlocks []ClassifiedBlock

	for _, pageNum := range pages {
		select {
		case <-e.ctx.Done():
			return allBlocks, e.ctx.Err()
		default:
		}

		page := e.reader.Page(pageNum)
		blocks, err := page.ClassifyTextBlocks()
		if err != nil {
			return nil, &PDFError{
				Op:   "classify text blocks",
				Page: pageNum,
				Err:  err,
			}
		}

		allBlocks = append(allBlocks, blocks...)

		// CRITICAL FIX: Cleanup page resources
		page.Cleanup()
	}

	return allBlocks, nil
}
