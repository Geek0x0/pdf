// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

// Performance optimizations using object pools and efficient memory management

// Pool for small Text objects (short strings, < 100 chars)
var smallTextPool = sync.Pool{
	New: func() interface{} {
		return &Text{}
	},
}

// Pool for large Text objects (long strings, >= 100 chars)
var largeTextPool = sync.Pool{
	New: func() interface{} {
		return &Text{}
	},
}

// Pool for Text objects (used in styled text extraction) - general pool
var textPool = sync.Pool{
	New: func() interface{} {
		return &Text{}
	},
}

// GetText retrieves a Text object from the appropriate pool based on content size
func GetText() *Text {
	return textPool.Get().(*Text)
}

// GetTextBySize retrieves a Text object from the appropriate pool based on content size
func GetTextBySize(contentLength int) *Text {
	if contentLength < 100 {
		return smallTextPool.Get().(*Text)
	}
	return largeTextPool.Get().(*Text)
}

// PutText returns a Text object to the appropriate pool
func PutText(t *Text) {
	// Reset the object before returning to pool
	*t = Text{}
	if len(t.S) < 100 {
		smallTextPool.Put(t)
	} else {
		largeTextPool.Put(t)
	}
}

// Pool for strings.Builder (used for text accumulation)
var builderPool = sync.Pool{
	New: func() interface{} {
		return &strings.Builder{}
	},
}

// GetBuilder retrieves a strings.Builder from the pool
func GetBuilder() *strings.Builder {
	return builderPool.Get().(*strings.Builder)
}

// PutBuilder returns a strings.Builder to the pool after resetting it
func PutBuilder(b *strings.Builder) {
	b.Reset()
	builderPool.Put(b)
}

// Pool for ClassifiedBlock slices (used in text classification)
var blockSlicePool = sync.Pool{
	New: func() interface{} {
		// Pre-allocate with reasonable capacity
		s := make([]ClassifiedBlock, 0, 16)
		return &s
	},
}

// GetBlockSlice retrieves a ClassifiedBlock slice from the pool
func GetBlockSlice() []ClassifiedBlock {
	return *blockSlicePool.Get().(*[]ClassifiedBlock)
}

// PutBlockSlice returns a ClassifiedBlock slice to the pool
func PutBlockSlice(s []ClassifiedBlock) {
	// Clear the slice but keep the capacity
	s = s[:0]
	blockSlicePool.Put(&s)
}

// Pool for Text slices (used in text extraction)
var textSlicePool = sync.Pool{
	New: func() interface{} {
		s := make([]Text, 0, 32)
		return &s
	},
}

// GetTextSlice retrieves a Text slice from the pool
func GetTextSlice() []Text {
	return *textSlicePool.Get().(*[]Text)
}

// PutTextSlice returns a Text slice to the pool
func PutTextSlice(s []Text) {
	s = s[:0]
	textSlicePool.Put(&s)
}

// Pool for byte buffers (used in various operations)
var byteBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 0, 4096) // 4KB initial capacity
		return &buf
	},
}

// GetByteBuffer retrieves a byte buffer from the pool
func GetByteBuffer() *[]byte {
	return byteBufferPool.Get().(*[]byte)
}

// PutByteBuffer returns a byte buffer to the pool
func PutByteBuffer(buf *[]byte) {
	*buf = (*buf)[:0]
	byteBufferPool.Put(buf)
}

// Pool for PDF buffers (used in lexing and parsing)
var pdfBufferPool = sync.Pool{
	New: func() interface{} {
		return &buffer{
			buf:         make([]byte, 0, 65536), // 64KB capacity
			tmp:         make([]byte, 0, 256),   // 256B for tokens
			unread:      make([]token, 0, 16),   // capacity for unread tokens
			key:         make([]byte, 0, 64),    // capacity for keys
			allowObjptr: true,
			allowStream: true,
		}
	},
}

// GetPDFBuffer retrieves a PDF buffer from the pool
func GetPDFBuffer() *buffer {
	return pdfBufferPool.Get().(*buffer)
}

// PutPDFBuffer returns a PDF buffer to the pool after resetting
func PutPDFBuffer(b *buffer) {
	// Reset all fields
	b.r = nil
	b.buf = b.buf[:0]
	b.pos = 0
	b.offset = 0
	b.tmp = b.tmp[:0]
	b.unread = b.unread[:0]
	b.allowEOF = false
	b.allowObjptr = true
	b.allowStream = true
	b.eof = false
	b.key = b.key[:0]
	b.useAES = false
	b.objptr = objptr{}
	pdfBufferPool.Put(b)
}

// FastStringBuilder provides optimized string building with pre-allocation
type FastStringBuilder struct {
	buf []byte
}

// NewFastStringBuilder creates a builder with estimated capacity
func NewFastStringBuilder(estimatedSize int) *FastStringBuilder {
	return &FastStringBuilder{
		buf: make([]byte, 0, EstimateCapacity(estimatedSize, 1.5)),
	}
}

// WriteString appends a string
func (b *FastStringBuilder) WriteString(s string) {
	b.buf = append(b.buf, s...)
}

// WriteByte appends a byte
func (b *FastStringBuilder) WriteByte(c byte) error {
	b.buf = append(b.buf, c)
	return nil
}

// String returns the accumulated string
func (b *FastStringBuilder) String() string {
	return string(b.buf)
}

// Len returns the current length
func (b *FastStringBuilder) Len() int {
	return len(b.buf)
}

// Reset clears the builder
func (b *FastStringBuilder) Reset() {
	b.buf = b.buf[:0]
}

// LazyPage provides lazy loading of page content to reduce memory usage
// for large PDFs where not all pages need to be processed
type LazyPage struct {
	reader  *Reader
	pageNum int
	content *Content // nil until loaded
	mu      sync.RWMutex
}

// NewLazyPage creates a lazy-loading page wrapper
func NewLazyPage(r *Reader, pageNum int) *LazyPage {
	return &LazyPage{
		reader:  r,
		pageNum: pageNum,
	}
}

// GetContent loads and returns the page content (cached after first call)
func (lp *LazyPage) GetContent() *Content {
	lp.mu.RLock()
	if lp.content != nil {
		content := lp.content
		lp.mu.RUnlock()
		return content
	}
	lp.mu.RUnlock()

	lp.mu.Lock()
	defer lp.mu.Unlock()

	// Double-check after acquiring write lock
	if lp.content != nil {
		return lp.content
	}

	// Load the content
	page := lp.reader.Page(lp.pageNum)
	content := page.Content()
	lp.content = &content

	return lp.content
}

// Release clears the cached content to free memory
func (lp *LazyPage) Release() {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	lp.content = nil
}

// IsLoaded returns whether the page content has been loaded
func (lp *LazyPage) IsLoaded() bool {
	lp.mu.RLock()
	defer lp.mu.RUnlock()
	return lp.content != nil
}

// LazyPageManager manages lazy loading of multiple pages
type LazyPageManager struct {
	reader     *Reader
	pages      map[int]*LazyPage
	maxCached  int // Maximum number of pages to keep in memory
	mu         sync.RWMutex
	accessList []int // LRU tracking
}

// NewLazyPageManager creates a manager with LRU cache
func NewLazyPageManager(r *Reader, maxCached int) *LazyPageManager {
	if maxCached <= 0 {
		maxCached = 10 // Default to 10 pages
	}
	return &LazyPageManager{
		reader:     r,
		pages:      make(map[int]*LazyPage),
		maxCached:  maxCached,
		accessList: make([]int, 0, maxCached),
	}
}

// GetPage returns a lazy page, loading it if necessary
func (m *LazyPageManager) GetPage(pageNum int) *LazyPage {
	m.mu.RLock()
	if page, ok := m.pages[pageNum]; ok {
		m.mu.RUnlock()
		m.updateAccess(pageNum)
		return page
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check
	if page, ok := m.pages[pageNum]; ok {
		m.updateAccessLocked(pageNum)
		return page
	}

	// Create new lazy page
	page := NewLazyPage(m.reader, pageNum)
	m.pages[pageNum] = page
	m.updateAccessLocked(pageNum)

	// Enforce cache limit
	if len(m.accessList) > m.maxCached {
		oldest := m.accessList[0]
		if oldPage, ok := m.pages[oldest]; ok {
			oldPage.Release()
		}
		m.accessList = m.accessList[1:]
	}

	return page
}

// updateAccess updates the LRU access list (with lock)
func (m *LazyPageManager) updateAccess(pageNum int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateAccessLocked(pageNum)
}

// updateAccessLocked updates the LRU access list (caller must hold lock)
func (m *LazyPageManager) updateAccessLocked(pageNum int) {
	// Remove from current position
	for i, p := range m.accessList {
		if p == pageNum {
			m.accessList = append(m.accessList[:i], m.accessList[i+1:]...)
			break
		}
	}
	// Add to end (most recently used)
	m.accessList = append(m.accessList, pageNum)
}

// Clear releases all cached pages
func (m *LazyPageManager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, page := range m.pages {
		page.Release()
	}
	m.pages = make(map[int]*LazyPage)
	m.accessList = m.accessList[:0]
}

// GetStats returns cache statistics
func (m *LazyPageManager) GetStats() (totalPages, loadedPages int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalPages = len(m.pages)
	for _, page := range m.pages {
		if page.IsLoaded() {
			loadedPages++
		}
	}
	return
}

// ResourceManager provides automatic resource cleanup
type ResourceManager struct {
	resources []io.Closer
	mu        sync.Mutex
}

// NewResourceManager creates a new resource manager
func NewResourceManager() *ResourceManager {
	return &ResourceManager{
		resources: make([]io.Closer, 0, 8),
	}
}

// Add adds a resource to be managed
func (rm *ResourceManager) Add(resource io.Closer) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.resources = append(rm.resources, resource)
}

// Close closes all managed resources
func (rm *ResourceManager) Close() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	var errs []error
	for _, resource := range rm.resources {
		if err := resource.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	rm.resources = rm.resources[:0] // Clear the slice

	if len(errs) > 0 {
		return fmt.Errorf("resource cleanup errors: %v", errs)
	}
	return nil
}
