// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// OptimizedGetPlainText returns the page's all text using optimized string building.
// This version uses object pools and pre-allocation to reduce memory allocations.
func (p Page) OptimizedGetPlainText(fonts map[string]*Font) (string, error) {
	// Handle in case the content page is empty
	if p.V.IsNull() || p.V.Key("Contents").Kind() == Null {
		return "", nil
	}

	content, err := p.contentWithFonts(fonts)
	if err != nil {
		return "", wrapError("extract page content", err)
	}

	return optimizedTextRunsToPlain(content.Text), nil
}

// optimizedTextRunsToPlain converts text runs to plain text with optimized memory usage
func optimizedTextRunsToPlain(texts []Text) string {
	if len(texts) == 0 {
		return ""
	}

	// work on a copy so callers of Content() are not affected by ordering changes
	runs := append([]Text(nil), texts...)
	sort.Sort(TextVertical(runs))

	const lineTolerance = 2.0
	// Pre-allocate lines slice with estimated capacity
	lines := make([][]Text, 0, len(runs)/5+1) // Estimate 5 text runs per line
	currentLine := make([]Text, 0, 10)        // Pre-allocate with reasonable capacity
	var currentCoord float64

	for i, t := range runs {
		lineCoord := effectiveLineCoord(t)
		if i == 0 || math.Abs(lineCoord-currentCoord) <= lineTolerance {
			currentLine = append(currentLine, t)
			if len(currentLine) == 1 {
				currentCoord = lineCoord
			} else {
				currentCoord = (currentCoord*float64(len(currentLine)-1) + lineCoord) / float64(len(currentLine))
			}
			continue
		}
		if len(currentLine) > 0 {
			sort.Slice(currentLine, func(i, j int) bool {
				return effectiveOrderCoord(currentLine[i]) < effectiveOrderCoord(currentLine[j])
			})
			lines = append(lines, currentLine)
		}
		currentLine = make([]Text, 0, 10)
		currentLine = append(currentLine, t)
		currentCoord = lineCoord
	}

	if len(currentLine) > 0 {
		sort.Slice(currentLine, func(i, j int) bool {
			return effectiveOrderCoord(currentLine[i]) < effectiveOrderCoord(currentLine[j])
		})
		lines = append(lines, currentLine)
	}

	// Estimate final string size (average 50 chars per line)
	estimatedSize := len(lines) * 50
	builder := GetBuilder()
	defer PutBuilder(builder)

	// Pre-grow to avoid multiple allocations
	builder.Grow(estimatedSize)

	for i, line := range lines {
		optimizedAppendLine(builder, line)
		if i < len(lines)-1 {
			builder.WriteByte('\n')
		}
	}

	result := builder.String()
	return strings.TrimRight(result, "\n")
}

// optimizedAppendLine appends a line of text with space detection (optimized version)
func optimizedAppendLine(builder *strings.Builder, line []Text) {
	const minGap = 0.5
	var prevEnd float64
	hasPrev := false
	allVertical := true
	for _, t := range line {
		if !t.Vertical {
			allVertical = false
			break
		}
	}

	for _, t := range line {
		if hasPrev {
			var gap float64
			if allVertical {
				gap = math.Abs(t.Y - prevEnd)
			} else {
				gap = t.X - prevEnd
			}
			spaceThreshold := math.Max(t.FontSize*0.2, minGap)
			if gap > spaceThreshold && !allVertical {
				builder.WriteByte(' ')
			}
		}
		builder.WriteString(t.S)
		if allVertical {
			prevEnd = t.Y - t.W
		} else {
			prevEnd = t.X + t.W
		}
		hasPrev = true
	}
}

// OptimizedGetTextByRow returns the page's all text grouped by rows using optimized allocation
func (p Page) OptimizedGetTextByRow() (Rows, error) {
	var result Rows
	var err error

	defer func() {
		if r := recover(); r != nil {
			result = Rows{}
			if e, ok := r.(error); ok {
				err = wrapError("extract text by row", e)
			} else {
				err = wrapError("extract text by row", fmt.Errorf("%v", r))
			}
		}
	}()

	// Pre-allocate result with reasonable capacity
	result = make(Rows, 0, 20)

	showText := func(enc TextEncoding, currentX, currentY float64, s string) {
		// Use strings.Builder from pool for text accumulation
		builder := GetBuilder()
		defer PutBuilder(builder)

		for _, ch := range enc.Decode(s) {
			builder.WriteRune(ch)
		}

		text := Text{
			S: builder.String(),
			X: currentX,
			Y: currentY,
		}

		var currentRow *Row
		rowFound := false
		rowPosition := int64(currentY)

		// Linear search is acceptable for small result sets
		// For larger sets, consider using a map
		for _, row := range result {
			if rowPosition == row.Position {
				currentRow = row
				rowFound = true
				break
			}
		}

		if !rowFound {
			currentRow = &Row{
				Position: rowPosition,
				Content:  make(TextHorizontal, 0, 10), // Pre-allocate
			}
			result = append(result, currentRow)
		}

		currentRow.Content = append(currentRow.Content, text)
	}

	p.walkTextBlocks(showText)

	for _, row := range result {
		sort.Sort(row.Content)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Position > result[j].Position
	})

	return result, err
}

// OptimizedGetTextByColumn returns the page's all text grouped by column using optimized allocation
func (p Page) OptimizedGetTextByColumn() (Columns, error) {
	var result Columns
	var err error

	defer func() {
		if r := recover(); r != nil {
			result = Columns{}
			if e, ok := r.(error); ok {
				err = wrapError("extract text by column", e)
			} else {
				err = wrapError("extract text by column", fmt.Errorf("%v", r))
			}
		}
	}()

	// Pre-allocate result with reasonable capacity
	result = make(Columns, 0, 5) // Estimate ~5 columns max

	showText := func(enc TextEncoding, currentX, currentY float64, s string) {
		// Use strings.Builder from pool
		builder := GetBuilder()
		defer PutBuilder(builder)

		for _, ch := range enc.Decode(s) {
			builder.WriteRune(ch)
		}

		text := Text{
			S: builder.String(),
			X: currentX,
			Y: currentY,
		}

		var currentColumn *Column
		columnFound := false
		columnPosition := int64(currentX)

		for _, column := range result {
			if columnPosition == column.Position {
				currentColumn = column
				columnFound = true
				break
			}
		}

		if !columnFound {
			currentColumn = &Column{
				Position: columnPosition,
				Content:  make(TextVertical, 0, 20), // Pre-allocate
			}
			result = append(result, currentColumn)
		}

		currentColumn.Content = append(currentColumn.Content, text)
	}

	p.walkTextBlocks(showText)

	for _, column := range result {
		sort.Sort(column.Content)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Position < result[j].Position
	})

	return result, err
}

// BatchExtractText extracts text from multiple pages using lazy loading and object pooling
// This is optimized for processing many pages without keeping all in memory
func (r *Reader) BatchExtractText(pageNums []int, useLazy bool) (map[int]string, error) {
	if len(pageNums) == 0 {
		return make(map[int]string), nil
	}

	results := make(map[int]string, len(pageNums))

	if useLazy {
		// Use lazy page manager for memory efficiency
		manager := NewLazyPageManager(r, 10) // Keep max 10 pages in memory
		defer manager.Clear()

		for _, pageNum := range pageNums {
			lazyPage := manager.GetPage(pageNum)
			content := lazyPage.GetContent()

			text := optimizedTextRunsToPlain(content.Text)
			results[pageNum] = text
		}
	} else {
		// Direct extraction without lazy loading
		fontCache := make(map[string]*Font)
		for _, pageNum := range pageNums {
			page := r.Page(pageNum)
			text, err := page.OptimizedGetPlainText(fontCache)
			if err != nil {
				return nil, wrapPageError("batch extract text", pageNum, err)
			}
			results[pageNum] = text
		}
	}

	return results, nil
}

// StreamingTextExtractor provides memory-efficient text extraction for large PDFs
type StreamingTextExtractor struct {
	reader      *Reader
	pageManager *LazyPageManager
	currentPage int
	totalPages  int
	batchSize   int
	fontCache   map[string]*Font
}

// NewStreamingTextExtractor creates a streaming extractor for large PDFs
func NewStreamingTextExtractor(r *Reader, maxCachedPages int) *StreamingTextExtractor {
	if maxCachedPages <= 0 {
		maxCachedPages = 10
	}

	return &StreamingTextExtractor{
		reader:      r,
		pageManager: NewLazyPageManager(r, maxCachedPages),
		currentPage: 1,
		totalPages:  r.NumPage(),
		batchSize:   maxCachedPages,
		fontCache:   make(map[string]*Font),
	}
}

// NextPage extracts text from the next page
func (e *StreamingTextExtractor) NextPage() (pageNum int, text string, hasMore bool, err error) {
	if e.currentPage > e.totalPages {
		return 0, "", false, nil
	}

	pageNum = e.currentPage
	page := e.reader.Page(pageNum)

	text, err = page.OptimizedGetPlainText(e.fontCache)
	if err != nil {
		return pageNum, "", false, wrapPageError("extract next page", pageNum, err)
	}

	e.currentPage++
	hasMore = e.currentPage <= e.totalPages

	return pageNum, text, hasMore, nil
}

// NextBatch extracts text from the next batch of pages
func (e *StreamingTextExtractor) NextBatch() (results map[int]string, hasMore bool, err error) {
	if e.currentPage > e.totalPages {
		return make(map[int]string), false, nil
	}

	endPage := e.currentPage + e.batchSize - 1
	if endPage > e.totalPages {
		endPage = e.totalPages
	}

	results = make(map[int]string, endPage-e.currentPage+1)

	for i := e.currentPage; i <= endPage; i++ {
		page := e.reader.Page(i)
		text, err := page.OptimizedGetPlainText(e.fontCache)
		if err != nil {
			return nil, false, wrapPageError("extract batch", i, err)
		}
		results[i] = text
	}

	e.currentPage = endPage + 1
	hasMore = e.currentPage <= e.totalPages

	return results, hasMore, nil
}

// Close releases resources used by the extractor
func (e *StreamingTextExtractor) Close() {
	if e.pageManager != nil {
		e.pageManager.Clear()
	}
	e.fontCache = nil
}

// Reset resets the extractor to the beginning
func (e *StreamingTextExtractor) Reset() {
	e.currentPage = 1
	if e.pageManager != nil {
		e.pageManager.Clear()
	}
}

// GetProgress returns the extraction progress (0.0 to 1.0)
func (e *StreamingTextExtractor) GetProgress() float64 {
	if e.totalPages == 0 {
		return 1.0
	}
	return float64(e.currentPage-1) / float64(e.totalPages)
}
