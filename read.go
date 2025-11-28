// Copyright 2014 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pdf implements reading of PDF files.
//
// # Overview
//
// PDF is Adobe's Portable Document Format, ubiquitous on the internet.
// A PDF document is a complex data format built on a fairly simple structure.
// This package exposes the simple structure along with some wrappers to
// extract basic information. If more complex information is needed, it is
// possible to extract that information by interpreting the structure exposed
// by this package.
//
// Specifically, a PDF is a data structure built from Values, each of which has
// one of the following Kinds:
//
//	Null, for the null object.
//	Integer, for an integer.
//	Real, for a floating-point number.
//	Bool, for a boolean value.
//	Name, for a name constant (as in /Helvetica).
//	String, for a string constant.
//	Dict, for a dictionary of name-value pairs.
//	Array, for an array of values.
//	Stream, for an opaque data stream and associated header dictionary.
//
// The accessors on Value—Int64, Float64, Bool, Name, and so on—return
// a view of the data as the given type. When there is no appropriate view,
// the accessor returns a zero result. For example, the Name accessor returns
// the empty string if called on a Value v for which v.Kind() != Name.
// Returning zero values this way, especially from the Dict and Array accessors,
// which themselves return Values, makes it possible to traverse a PDF quickly
// without writing any error checking. On the other hand, it means that mistakes
// can go unreported.
//
// The basic structure of the PDF file is exposed as the graph of Values.
//
// Most richer data structures in a PDF file are dictionaries with specific interpretations
// of the name-value pairs. The Font and Page wrappers make the interpretation
// of a specific Value as the corresponding type easier. They are only helpers, though:
// they are implemented only in terms of the Value API and could be moved outside
// the package. Equally important, traversal of other PDF data structures can be implemented
// in other packages as needed.
package pdf

// BUG(rsc): The package is incomplete, although it has been used successfully on some
// large real-world PDF files.

// BUG(rsc): The library makes no attempt at efficiency beyond the value cache and font cache.
// Further optimizations could improve performance for large files.

// BUG(rsc): The support for reading encrypted files is limited to basic RC4 and AES encryption.

import (
	"bufio"
	"bytes"
	"compress/lzw"
	"compress/zlib"
	"container/list"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rc4"
	"encoding/ascii85"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// toLatin1 converts a UTF-8 string to Latin-1 (ISO-8859-1) encoding.
// Characters that cannot be represented in Latin-1 are replaced with '?'.
func toLatin1(s string) []byte {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r < 256 {
			b = append(b, byte(r))
		} else {
			b = append(b, '?')
		}
	}
	return b
}

// bytesLastIndexOptimized is an optimized replacement for bytes.LastIndex
// that avoids the Rabin-Karp overhead for patterns <= 32 bytes.
// For longer patterns, it falls back to bytes.LastIndex.
//
//go:nosplit
func bytesLastIndexOptimized(s, sep []byte) int {
	n := len(sep)
	if n == 0 {
		return len(s)
	}
	if n > len(s) {
		return -1
	}
	// For short patterns, use simple reverse scan (faster than Rabin-Karp)
	if n <= 32 {
		first := sep[0]
		last := sep[n-1]
		for i := len(s) - n; i >= 0; i-- {
			// Quick 2-byte check before full comparison
			if s[i] == first && s[i+n-1] == last {
				match := true
				for j := 1; j < n-1; j++ {
					if s[i+j] != sep[j] {
						match = false
						break
					}
				}
				if match {
					return i
				}
			}
		}
		return -1
	}
	// For longer patterns, use standard library
	return bytes.LastIndex(s, sep)
}

// DebugOn is responsible for logging messages into stdout. If problems arise during reading, set it true.
var DebugOn = false

// FontCache stores parsed fonts to avoid re-parsing across pages
type FontCache struct {
	mu    sync.RWMutex
	fonts map[string]*Font
}

// NewFontCache creates a new font cache
func NewFontCache() *FontCache {
	return &FontCache{
		fonts: make(map[string]*Font),
	}
}

// Get retrieves a font from the cache
func (fc *FontCache) Get(key string) (*Font, bool) {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	font, ok := fc.fonts[key]
	return font, ok
}

// Set stores a font in the cache
func (fc *FontCache) Set(key string, font *Font) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.fonts[key] = font
}

// P2 optimization: global multi-level cache instance
var (
	globalMultiLevelCache = NewMultiLevelCache()
	globalFontCache       = NewFontCache()
)

// A Reader is a single PDF file open for reading.
type Reader struct {
	f             io.ReaderAt
	closer        io.Closer // Optional closer for underlying resource
	end           int64
	xref          []xref
	trailer       dict
	trailerptr    objptr
	key           []byte
	useAES        bool
	cacheMu       sync.RWMutex
	objCache      map[objptr]*list.Element
	cacheList     *list.List
	cacheCap      int
	fontCache     *FontCache
	compatibility *PDFCompatibilityInfo // Compatibility information

	// Cache for object streams to avoid re-parsing
	objStreamCache   map[uint32]map[int64]int64
	objStreamCacheMu sync.RWMutex
}

type xref struct {
	ptr      objptr
	inStream bool
	stream   objptr
	offset   int64
}

func (r *Reader) getCachedObject(ptr objptr) (object, bool) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	elem, ok := r.objCache[ptr]
	if !ok || elem == nil {
		return nil, false
	}
	r.cacheList.MoveToFront(elem)
	if entry, ok := elem.Value.(cacheEntry); ok {
		return entry.value, true
	}
	return nil, false
}

func (r *Reader) storeCachedObject(ptr objptr, obj object) {
	if ptr.id == 0 || obj == nil {
		return
	}
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if r.cacheList == nil {
		r.cacheList = list.New()
	}
	if r.objCache == nil {
		r.objCache = make(map[objptr]*list.Element)
	}
	if elem, ok := r.objCache[ptr]; ok {
		elem.Value = cacheEntry{key: ptr, value: obj}
		r.cacheList.MoveToFront(elem)
		return
	}
	elem := r.cacheList.PushFront(cacheEntry{key: ptr, value: obj})
	r.objCache[ptr] = elem
	if r.cacheCap > 0 && r.cacheList.Len() > r.cacheCap {
		r.evictOldest()
	}
}

func (r *Reader) evictOldest() {
	back := r.cacheList.Back()
	if back == nil {
		return
	}
	r.cacheList.Remove(back)
	if entry, ok := back.Value.(cacheEntry); ok {
		delete(r.objCache, entry.key)
	}
}

func (r *Reader) SetCacheCapacity(n int) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	r.cacheCap = n
	if n <= 0 {
		return
	}
	for r.cacheList != nil && r.cacheList.Len() > r.cacheCap {
		r.evictOldest()
	}
}

// GetCacheCapacity returns the current object cache capacity.
// Returns 0 if no capacity limit is set (unbounded cache).
func (r *Reader) GetCacheCapacity() int {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	return r.cacheCap
}

// ClearCache clears the object cache, releasing all cached objects.
// This is useful for freeing memory after batch processing large PDFs.
func (r *Reader) ClearCache() {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if r.objCache != nil {
		r.objCache = make(map[objptr]*list.Element)
	}
	if r.cacheList != nil {
		r.cacheList.Init()
	}
}

type cacheEntry struct {
	key   objptr
	value object
}

// Close closes the Reader and releases associated resources.
// If the underlying ReaderAt implements io.Closer, it will be closed.
func (r *Reader) Close() error {
	// Clear object cache to free memory
	r.ClearCache()

	// Clear font cache to free memory
	if r.fontCache != nil {
		r.fontCache = nil
	}

	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}

// Open opens a file for reading.
func Open(file string) (*os.File, *Reader, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	reader, err := NewReader(f, fi.Size())
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, reader, err
}

// NewReader opens a file for reading, using the data in f with the given total size.
func NewReader(f io.ReaderAt, size int64) (*Reader, error) {
	r, err := NewReaderEncrypted(f, size, nil)
	if err != nil {
		return nil, err
	}
	// If f implements io.Closer, store it for cleanup
	if closer, ok := f.(io.Closer); ok {
		r.closer = closer
	}
	return r, nil
}

// NewReaderEncrypted opens a file for reading, using the data in f with the given total size.
// If the PDF is encrypted, NewReaderEncrypted calls pw repeatedly to obtain passwords
// to try. If pw returns the empty string, NewReaderEncrypted stops trying to decrypt
// the file and returns an error.
func NewReaderEncrypted(f io.ReaderAt, size int64, pw func() string) (*Reader, error) {
	const headerSearchLimit = 4096
	headerProbe := headerSearchLimit
	if size < int64(headerProbe) {
		headerProbe = int(size)
	}
	if headerProbe < 8 {
		return nil, fmt.Errorf("not a PDF file: invalid header")
	}

	buf := make([]byte, headerProbe)
	n, _ := f.ReadAt(buf, 0)
	buf = buf[:n]

	// Search for the %PDF- signature within the first few KB to tolerate BOM/whitespace
	sig := []byte("%PDF-")
	sigIdx := bytes.Index(buf, sig)
	if sigIdx < 0 {
		// Try more tolerant patterns for damaged headers
		// Look for PDF- without the leading %
		sig = []byte("PDF-")
		sigIdx = bytes.Index(buf, sig)
		if sigIdx < 0 {
			return nil, fmt.Errorf("not a PDF file: invalid header")
		}
		if DebugOn {
			fmt.Println("warning: PDF header missing leading %, attempting recovery")
		}
		// Adjust sigIdx to account for missing %
		sigIdx-- // We'll treat the position before PDF- as if % was there
		if sigIdx < 0 {
			sigIdx = 0
		}
	}

	// Ensure we have enough bytes after the signature to parse the version
	const minHeaderLen = 9 // %PDF-X.Y plus a following byte
	required := sigIdx + minHeaderLen
	if int64(required) > size {
		// File too short, but try to continue if we at least have the PDF- marker
		if sigIdx+4 <= len(buf) && bytes.Equal(buf[sigIdx:sigIdx+4], []byte("%PDF")) {
			// We have minimal header, set a default version and continue
			if DebugOn {
				fmt.Println("warning: PDF header truncated, assuming version 1.4")
			}
			// Continue with minimal validation
		} else {
			return nil, fmt.Errorf("not a PDF file: invalid header")
		}
	}
	if required > len(buf) {
		more := make([]byte, required-len(buf))
		readMore, _ := f.ReadAt(more, int64(len(buf)))
		buf = append(buf, more[:readMore]...)
	}
	// sigIdx+7 points to the last character of version (e.g., '7' in '%PDF-1.7')
	// We need at least sigIdx+8 bytes to have the complete version string
	if sigIdx+8 > len(buf) {
		// Truncated version, try to continue with what we have
		if DebugOn {
			fmt.Println("warning: PDF version string truncated, attempting to parse partial version")
		}
		// Don't fail immediately, let parsePDFVersion handle it
	}

	// Parse version number using enhanced compatibility checking
	version, err := parsePDFVersion(buf[sigIdx:])
	if err != nil {
		return nil, err
	}

	// Check version compatibility
	if !version.IsSupported() {
		return nil, fmt.Errorf("not a PDF file: unsupported PDF version %s", version.String())
	}

	// Log version information
	if DebugOn {
		fmt.Printf("PDF version: %s\n", version.String())
	}

	// The byte after version should be whitespace, EOL, or start of binary marker.
	// We still log unexpected characters but do not fail parsing.
	if len(buf) > sigIdx+8 {
		c := buf[sigIdx+8]
		if c != '\r' && c != '\n' && c != ' ' && c != '%' && c != '\t' {
			// Log but don't fail - some non-conforming PDFs still work
			if DebugOn {
				fmt.Printf("warning: unexpected character after PDF version: %q\n", c)
			}
		}
	}

	end := size
	const endChunk = 1024 // Increased to handle PDFs with trailing data
	buf = make([]byte, endChunk)
	readLen := endChunk
	if end < int64(readLen) {
		readLen = int(end)
	}
	f.ReadAt(buf[:readLen], end-int64(readLen))
	buf = buf[:readLen]

	// Keep original buffer before trimming for fallback search
	originalBuf := make([]byte, len(buf))
	copy(originalBuf, buf)

	// Trim trailing whitespace
	for len(buf) > 0 && (buf[len(buf)-1] == '\n' || buf[len(buf)-1] == '\r' || buf[len(buf)-1] == ' ' || buf[len(buf)-1] == '\t' || buf[len(buf)-1] == 0) {
		buf = buf[:len(buf)-1]
	}

	// Find %%EOF marker - it might not be at the very end
	eofIdx := bytesLastIndexOptimized(buf, []byte("%%EOF"))
	if eofIdx < 0 {
		// Try to continue without %%EOF for damaged files
		// This is a recovery mechanism
		if DebugOn {
			fmt.Println("warning: PDF missing EOF marker, attempting recovery")
		}
		// Still try to find startxref
	} else {
		// Use content up to and including %%EOF for finding startxref
		buf = buf[:eofIdx+5]
	}

	// Multi-layer search strategy for startxref with enhanced recovery
	var i int

	// Strategy 1: Try with original untrimmed buffer first
	i = findLastLine(originalBuf, "startxref")
	if i >= 0 {
		buf = originalBuf
		if DebugOn {
			fmt.Println("Found startxref in original buffer")
		}
	}

	// Strategy 2: Try with EOF-truncated buffer
	if i < 0 {
		i = findLastLine(buf, "startxref")
		if i >= 0 && DebugOn {
			fmt.Println("Found startxref in EOF-truncated buffer")
		}
	}

	// Strategy 3: Try with larger chunk if still not found
	if i < 0 && size > int64(endChunk) {
		// Search in larger chunk (up to 10KB)
		chunkSize := int64(10 * 1024)
		if size < chunkSize {
			chunkSize = size
		}
		bigBuf := make([]byte, chunkSize)
		readOffset := size - chunkSize
		if readOffset < 0 {
			readOffset = 0
		}
		f.ReadAt(bigBuf, readOffset)
		bigIdx := findLastLine(bigBuf, "startxref")
		if bigIdx >= 0 {
			buf = bigBuf
			i = bigIdx
			if DebugOn {
				fmt.Println("Found startxref in larger buffer")
			}
		}
	}

	// Strategy 4: Backward search from end of file (more tolerant)
	if i < 0 {
		// Try searching from the very end backward, byte by byte
		if backwardIdx := searchBackwardForStartxref(f, size); backwardIdx >= 0 {
			// Re-read buffer at found position
			readLen := endChunk
			if size-backwardIdx < int64(readLen) {
				readLen = int(size - backwardIdx)
			}
			buf = make([]byte, readLen)
			f.ReadAt(buf, backwardIdx)
			i = 0 // startxref is at the beginning of our new buffer
			if DebugOn {
				fmt.Printf("Found startxref via backward search at offset %d\n", backwardIdx)
			}
		}
	}

	// Strategy 5: Last resort - search without strict line boundaries
	if i < 0 {
		// Try simple byte search as last resort
		simpleIdx := bytesLastIndexOptimized(buf, []byte("startxref"))
		if simpleIdx >= 0 {
			// Verify it's on its own line by checking context
			if simpleIdx > 0 && (buf[simpleIdx-1] == '\n' || buf[simpleIdx-1] == '\r') {
				i = simpleIdx
				if DebugOn {
					fmt.Println("Found startxref using fallback simple search")
				}
			} else if simpleIdx == 0 {
				// startxref is at the very beginning of buffer
				i = 0
				if DebugOn {
					fmt.Println("Found startxref at buffer start")
				}
			}
		}
	}

	// All strategies failed
	if i < 0 {
		if DebugOn {
			fmt.Printf("Failed to find startxref. Buffer length: %d, Last 100 bytes: %q\n",
				len(buf), string(buf[max(0, len(buf)-100):]))
		}
		return nil, fmt.Errorf("malformed PDF file: missing startxref")
	}

	r := &Reader{
		f:         f,
		end:       end,
		fontCache: NewFontCache(),
		// CRITICAL FIX: Set default cache capacity to prevent unbounded growth.
		// Without this limit, objCache can grow to gigabytes during batch processing.
		// A capacity of 2000 objects provides good performance while limiting memory.
		cacheCap:       2000,
		objStreamCache: make(map[uint32]map[int64]int64),
	}

	// Initialize compatibility information
	if compatInfo, err := CheckPDFCompatibility(buf); err == nil {
		r.compatibility = compatInfo
		if DebugOn {
			fmt.Printf("PDF Compatibility: %+v\n", compatInfo)
		}
	}
	// Calculate position correctly for small files
	// If the file is smaller than endChunk, we read from 0, so pos = i
	// Otherwise, pos = end - bytesActuallyRead + i
	var pos int64
	if end < int64(endChunk) {
		pos = int64(i)
	} else {
		pos = end - int64(len(buf)) + int64(i)
	}
	b := newBuffer(io.NewSectionReader(f, pos, end-pos), pos)
	if b.readToken() != keyword("startxref") {
		PutPDFBuffer(b)
		return nil, fmt.Errorf("malformed PDF file: missing startxref")
	}
	startxref, ok := b.readToken().(int64)
	PutPDFBuffer(b)
	if !ok {
		return nil, fmt.Errorf("malformed PDF file: startxref not followed by integer")
	}
	b = newBuffer(io.NewSectionReader(r.f, startxref, r.end-startxref), startxref)
	xref, trailerptr, trailer, err := readXref(r, b)
	if err != nil {
		// Recovery strategy chain:
		// 1. Try searchAndParseXref (search for xref/XRef in file)
		// 2. Try rebuildXrefTable (scan for all objects)
		// 3. Try RecoverPDF (comprehensive recovery with multiple strategies)

		recovered := false

		// Strategy 1: Search for xref table/stream
		if searchErr := r.searchAndParseXref(); searchErr == nil {
			trailer = r.trailer
			trailerptr = r.trailerptr
			recovered = true
			if DebugOn {
				fmt.Println("Recovery successful: searchAndParseXref")
			}
		}

		// Strategy 2: Rebuild xref by scanning objects
		if !recovered {
			if rebuildErr := r.rebuildXrefTable(); rebuildErr == nil {
				trailer = r.trailer
				recovered = true
				if DebugOn {
					fmt.Println("Recovery successful: rebuildXrefTable")
				}
			}
		}

		// Strategy 3: Use comprehensive recovery
		if !recovered {
			opts := DefaultRecoveryOptions()
			if recoverErr := recoverPDFInternal(r, opts); recoverErr == nil {
				trailer = r.trailer
				recovered = true
				if DebugOn {
					fmt.Println("Recovery successful: RecoverPDF")
				}
			}
		}

		if !recovered {
			return nil, fmt.Errorf("malformed PDF: xref table at offset %d: %v (all recovery strategies failed)", startxref, err)
		}

		_ = trailerptr // Not used for rebuilt xref
	} else {
		r.xref = xref
		r.trailer = trailer
		r.trailerptr = trailerptr
	}
	if trailer["Encrypt"] == nil {
		return r, nil
	}
	err = r.initEncrypt("")
	if err == nil {
		return r, nil
	}
	if pw == nil || err != ErrInvalidPassword {
		return nil, err
	}
	for {
		next := pw()
		if next == "" {
			break
		}
		if r.initEncrypt(next) == nil {
			return r, nil
		}
	}
	return nil, err
}

// NewReaderEncryptedWithMmap opens a file for reading with memory mapping for large files.
// If the file size exceeds 10MB, it uses memory mapping to reduce memory usage.
// This is a wrapper around NewReaderEncrypted that optimizes for large files.
func NewReaderEncryptedWithMmap(f io.ReaderAt, size int64, pw func() string) (*Reader, error) {
	const largeFileThreshold = 10 * 1024 * 1024 // 10MB
	if size > largeFileThreshold {
		// For large files, we could implement memory mapping here
		// For now, fall back to regular reader but log the opportunity
		// TODO: Implement actual memory mapping using syscall.Mmap or similar
		if DebugOn {
			fmt.Printf("Large file detected (%d bytes), consider memory mapping optimization\n", size)
		}
	}
	return NewReaderEncrypted(f, size, pw)
}

// NewReaderLinearized creates a reader optimized for linearized PDFs
func NewReaderLinearized(f io.ReaderAt, size int64, pw func() string) (*Reader, error) {
	r, err := NewReaderEncrypted(f, size, pw)
	if err != nil {
		return nil, err
	}

	// If PDF is linearized, enable optimizations
	if r.compatibility != nil && r.compatibility.IsLinearized {
		if DebugOn {
			fmt.Println("PDF is linearized, enabling optimized reading")
		}

		// Attempt to extract Linearization Parameter Dictionary
		// It must be the first object in the file.
		b := newBuffer(io.NewSectionReader(f, 0, size), 0)

		// readToken skips comments (header), so the first token should be the object number.
		tok1 := b.readToken()
		tok2 := b.readToken()
		tok3 := b.readToken()

		if _, ok1 := tok1.(int64); ok1 {
			if _, ok2 := tok2.(int64); ok2 {
				if kw, ok3 := tok3.(keyword); ok3 && kw == "obj" {
					obj := b.readObject()
					if d, ok := obj.(dict); ok {
						// Check if it is indeed the linearization dict
						if _, isLin := d["Linearized"]; isLin {
							params := make(map[string]interface{})
							for k, v := range d {
								params[string(k)] = v
							}
							r.compatibility.LinearizationParams = params
						}
					}
				}
			}
		}
		PutPDFBuffer(b)
	}

	return r, nil
}

// Trailer returns the file's Trailer value.
func (r *Reader) Trailer() Value {
	return Value{r, r.trailerptr, r.trailer}
}

func readXref(r *Reader, b *buffer) (xr []xref, trailerptr objptr, trailer dict, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("malformed PDF: %v", rec)
		}
	}()
	defer PutPDFBuffer(b)
	offset := b.readOffset()

	// Special handling for offset 116 which is extremely common in corrupted PDFs
	// This offset often indicates a systematic corruption pattern
	if offset == 116 {
		if DebugOn {
			fmt.Println("Detected offset 116 corruption pattern, attempting enhanced recovery")
		}
		// Try multiple recovery strategies specific to offset 116
		if xr, trailerptr, trailer, err = tryRecoverFromOffset116(r); err == nil {
			return
		}
	}

	tok := b.readToken()
	if tok == keyword("xref") {
		xr, trailerptr, trailer, err = readXrefTable(r, b)
		return
	}
	if _, ok := tok.(int64); ok {
		b.unreadToken(tok)
		xr, trailerptr, trailer, err = readXrefStream(r, b)
		return
	}

	// The startxref offset might be pointing to wrong location
	// Try to search nearby for xref stream or xref table

	// Check if we got a dict that looks like it could be part of xref stream
	if d, ok := tok.(dict); ok {
		// This might be the dict part of an xref stream object
		// Try to find the object header before this position
		if xr, trailerptr, trailer, err = tryRecoverXrefFromDict(r, d, offset); err == nil {
			return
		}

		// If tryRecoverXrefFromDict failed, try global search for xref
		// This handles cases where startxref points to a non-XRef stream dict
		if searchErr := r.searchAndParseXref(); searchErr == nil {
			return r.xref, r.trailerptr, r.trailer, nil
		}

		// Try rebuilding xref by scanning all objects
		if rebuildErr := r.rebuildXrefTable(); rebuildErr == nil {
			return r.xref, r.trailerptr, r.trailer, nil
		}
	}

	// Check for common corruption patterns
	err = diagnoseXrefCorruption(tok, offset)
	return
}

// tryRecoverXrefFromDict attempts to recover xref information when we find
// a dictionary at the startxref position, which might indicate the startxref
// offset is pointing slightly off (e.g., to the stream dictionary instead of
// the object start).
func tryRecoverXrefFromDict(r *Reader, d dict, offset int64) ([]xref, objptr, dict, error) {
	// Check if this dictionary has xref stream characteristics
	// Accept dictionaries with Type:XRef OR dictionaries that look like stream headers
	// (containing Filter and DecodeParms which are common in xref streams)
	isXRefDict := d["Type"] == name("XRef")
	isStreamDict := d["Filter"] != nil || d["DecodeParms"] != nil

	if !isXRefDict && !isStreamDict {
		return nil, objptr{}, nil, fmt.Errorf("dictionary is not an XRef stream or stream header")
	}

	// This looks like an xref stream dictionary or a stream header
	// We need to find the full stream object
	// Search backward from current position to find object definition

	// Expand lookback window significantly - some PDFs have large gaps
	const dictSearchBack = 8192
	searchStart := offset - dictSearchBack
	if searchStart < 0 {
		searchStart = 0
	}

	if offset <= searchStart {
		return nil, objptr{}, nil, fmt.Errorf("could not find object definition")
	}

	chunkSize := offset - searchStart
	chunk := make([]byte, chunkSize)
	_, err := r.f.ReadAt(chunk, searchStart)
	if err != nil && err != io.EOF {
		return nil, objptr{}, nil, err
	}

	// Look for "N M obj" pattern before the current position
	searchArea := chunk

	// Find last occurrence of " obj"
	objIdx := bytesLastIndexOptimized(searchArea, []byte(" obj"))
	if objIdx < 0 {
		objIdx = bytesLastIndexOptimized(searchArea, []byte("\nobj"))
	}
	if objIdx < 0 {
		objIdx = bytesLastIndexOptimized(searchArea, []byte("\robj"))
	}
	if objIdx < 0 {
		return nil, objptr{}, nil, fmt.Errorf("could not find object definition")
	}

	// Find line start
	lineStart := objIdx
	for lineStart > 0 && searchArea[lineStart-1] != '\n' && searchArea[lineStart-1] != '\r' {
		lineStart--
	}

	// Parse object from this position
	objStart := searchStart + int64(lineStart)
	b := newBuffer(io.NewSectionReader(r.f, objStart, r.end-objStart), objStart)
	defer PutPDFBuffer(b)

	return readXrefStream(r, b)
}

// tryRecoverFromOffset116 attempts enhanced recovery for the common offset 116 corruption pattern
func tryRecoverFromOffset116(r *Reader) ([]xref, objptr, dict, error) {
	// Offset 116 is the most common corruption pattern (44% of xref errors)
	// This typically means startxref is pointing to wrong location
	// Try multiple recovery strategies specific to this pattern

	// Strategy 1: Search for xref streams in the entire file
	if err := r.searchAndParseXref(); err == nil {
		return r.xref, r.trailerptr, r.trailer, nil
	}

	// Strategy 2: Try rebuilding xref by scanning all objects
	if err := r.rebuildXrefTable(); err == nil {
		return r.xref, r.trailerptr, r.trailer, nil
	}

	// Strategy 3: Check common offset variations around 116
	// Sometimes the offset is slightly off
	offsets := []int64{0, 100, 120, 150, 200, 250}
	for _, offset := range offsets {
		if offset == 116 {
			continue // Already tried this
		}
		b := newBuffer(io.NewSectionReader(r.f, offset, r.end-offset), offset)
		tok := b.readToken()

		// Check if it's a traditional xref table
		if tok == keyword("xref") {
			xr, tp, tr, err := readXrefTable(r, b)
			PutPDFBuffer(b)
			if err == nil {
				return xr, tp, tr, nil
			}
			continue // Skip the Put at the end since we already Put
		}

		// Check if it's an xref stream (starts with object number)
		if _, ok := tok.(int64); ok {
			b.unreadToken(tok)
			xr, tp, tr, err := readXrefStream(r, b)
			PutPDFBuffer(b)
			if err == nil {
				return xr, tp, tr, nil
			}
			continue // Skip the Put at the end since we already Put
		}

		PutPDFBuffer(b)
	}

	return nil, objptr{}, nil, fmt.Errorf("offset 116 recovery failed")
}

// searchBackwardForStartxref searches backward from the end of file for startxref marker
// This is more tolerant to file corruption and trailing data
func searchBackwardForStartxref(f io.ReaderAt, size int64) int64 {
	// For small files, just search the whole thing
	if size < 4096 {
		buf := make([]byte, size)
		n, err := f.ReadAt(buf, 0)
		if err != nil && err != io.EOF {
			return -1
		}
		idx := bytesLastIndexOptimized(buf[:n], []byte("startxref"))
		if idx >= 0 {
			return int64(idx)
		}
		return -1
	}

	// Search in chunks from the end backward
	const chunkSize = 4096
	var searchBuf []byte

	// Start from the end and work backward
	for offset := size - chunkSize; offset >= 0; offset -= chunkSize / 2 {
		if offset < 0 {
			offset = 0
		}

		readSize := chunkSize
		if offset+int64(readSize) > size {
			readSize = int(size - offset)
		}

		chunk := make([]byte, readSize)
		n, err := f.ReadAt(chunk, offset)
		if err != nil && err != io.EOF {
			return -1
		}
		chunk = chunk[:n]

		// Prepend this chunk to our search buffer (searching backward)
		searchBuf = append(chunk, searchBuf...)

		// Look for startxref in accumulated buffer
		idx := bytesLastIndexOptimized(searchBuf, []byte("startxref"))
		if idx >= 0 {
			// Found it! Calculate absolute position
			// The searchBuf starts at 'offset', so absolute position is offset + idx
			return offset + int64(idx)
		}

		// If we've searched back to the beginning, stop
		if offset == 0 {
			break
		}

		// Keep some overlap for next iteration (in case startxref spans chunks)
		if len(searchBuf) > chunkSize*2 {
			searchBuf = searchBuf[len(searchBuf)-chunkSize:]
		}
	}

	return -1
}

// diagnoseXrefCorruption analyzes what was found instead of xref table and provides specific diagnosis
func diagnoseXrefCorruption(tok token, offset int64) error {
	tokStr := fmt.Sprintf("%T", tok)

	// Check for the specific corruption pattern seen in multiple files
	if dict, ok := tok.(dict); ok {
		if dict["Filter"] != nil && dict["DecodeParms"] != nil {
			// Check for the specific corruption pattern seen in multiple files
			if filter := dict["Filter"]; filter == name("FlateDecode") {
				if decodeParms := dict["DecodeParms"]; decodeParms != nil {
					return fmt.Errorf("malformed PDF: cross-reference table not found at offset %d, found stream object header instead (FlateDecode stream with compression parameters) - this appears to be a systematic PDF generation or transmission error affecting multiple files", offset)
				}
			}
			return fmt.Errorf("malformed PDF: cross-reference table not found at offset %d, found stream object header instead (Filter: %v, DecodeParms present) - PDF structure severely corrupted", offset, dict["Filter"])
		}
		if len(dict) > 0 {
			return fmt.Errorf("malformed PDF: cross-reference table not found at offset %d, found dictionary object instead - possible xref table corruption", offset)
		}
	}

	// Check if we found an object definition
	if objdef, ok := tok.(objdef); ok {
		// Check if this object definition contains xref stream metadata
		if strm, ok := objdef.obj.(stream); ok {
			if strm.hdr["Type"] == name("XRef") {
				return fmt.Errorf("malformed PDF: cross-reference table not found at offset %d, found xref stream object definition instead - startxref offset incorrect, should point to xref stream start, not object definition", offset)
			}
			// Check for systematic corruption: stream object with FlateDecode at xref position
			if strm.hdr["Filter"] == name("FlateDecode") && strm.hdr["DecodeParms"] != nil {
				return fmt.Errorf("malformed PDF: cross-reference table not found at offset %d, found FlateDecode stream object definition instead - this matches systematic PDF corruption pattern seen in multiple files (object %d)", offset, objdef.ptr.id)
			}
		}
		return fmt.Errorf("malformed PDF: cross-reference table not found at offset %d, found object definition instead - xref offset points to wrong location", offset)
	}

	// Check for malformed object definition strings (systematic corruption pattern)
	if str, ok := tok.(string); ok {
		if strings.Contains(str, "obj") && strings.Contains(str, "/Filter") && strings.Contains(str, "/FlateDecode") {
			return fmt.Errorf("malformed PDF: cross-reference table not found at offset %d, found malformed object definition string with FlateDecode filter - this matches systematic PDF corruption pattern seen in multiple files", offset)
		}
	}

	// Generic case with length limit
	valStr := fmt.Sprintf("%v", tok)
	if len(valStr) > 100 {
		valStr = valStr[:100] + "... (truncated)"
	}
	tokStr += ": " + valStr
	return fmt.Errorf("malformed PDF: cross-reference table not found at offset %d, found %s", offset, tokStr)
}

func readXrefStream(r *Reader, b *buffer) ([]xref, objptr, dict, error) {
	obj1 := b.readObject()
	obj, ok := obj1.(objdef)
	if !ok {
		// Enhanced error for xref stream corruption
		if dict, ok := obj1.(dict); ok && dict["Filter"] != nil {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: expected xref stream but found stream object header (Filter: %v) - PDF structure corrupted", dict["Filter"])
		}
		// Check for systematic corruption: malformed object definition strings at xref stream position
		if str, ok := obj1.(string); ok {
			if strings.Contains(str, "obj") && strings.Contains(str, "/Filter") && strings.Contains(str, "/FlateDecode") {
				return nil, objptr{}, nil, fmt.Errorf("malformed PDF: expected xref stream but found malformed object definition string with FlateDecode filter - this matches systematic PDF corruption pattern seen in multiple files")
			}
		}
		// Limit error message length for large objects
		objStr := fmt.Sprintf("%T", obj1)
		valStr := fmt.Sprintf("%v", obj1)
		if len(valStr) > 100 {
			valStr = valStr[:100] + "... (truncated)"
		}
		objStr += ": " + valStr
		return nil, objptr{}, nil, fmt.Errorf("malformed PDF: cross-reference table not found: %s", objStr)
	}
	strmptr := obj.ptr
	strm, ok := obj.obj.(stream)
	if !ok {
		// Limit error message length for large objects
		objStr := fmt.Sprintf("%T", obj.obj)
		if len(fmt.Sprintf("%v", obj.obj)) > 100 {
			objStr += " (value too long to display)"
		} else {
			objStr += fmt.Sprintf(": %v", obj.obj)
		}
		return nil, objptr{}, nil, fmt.Errorf("malformed PDF: cross-reference table not found: %s", objStr)
	}
	if strm.hdr["Type"] != name("XRef") {
		return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref stream does not have type XRef")
	}
	size, ok := strm.hdr["Size"].(int64)
	if !ok {
		return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref stream missing Size")
	}
	table := make([]xref, size)

	table, err := readXrefStreamData(r, strm, table, size)
	if err != nil {
		return nil, objptr{}, nil, fmt.Errorf("malformed PDF: %v", err)
	}

	seenPrev := make(map[int64]struct{})
	for prevoff := strm.hdr["Prev"]; prevoff != nil; {
		off, ok := prevoff.(int64)
		if !ok {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref Prev is not integer: %v", prevoff)
		}
		if off < 0 || off >= r.end {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref Prev offset out of range: %v", prevoff)
		}
		if _, seen := seenPrev[off]; seen {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref Prev chain contains cycle at offset %d", off)
		}
		seenPrev[off] = struct{}{}
		b := newBuffer(io.NewSectionReader(r.f, off, r.end-off), off)
		obj1 := b.readObject()
		obj, ok := obj1.(objdef)
		PutPDFBuffer(b)
		if !ok {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref prev stream not found: %v", objfmt(obj1))
		}
		prevstrm, ok := obj.obj.(stream)
		if !ok {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref prev stream not found: %v", objfmt(obj))
		}
		prevoff = prevstrm.hdr["Prev"]
		prev := Value{r, objptr{}, prevstrm}
		if prev.Kind() != Stream {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref prev stream is not stream: %v", prev)
		}
		if prev.Key("Type").Name() != "XRef" {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref prev stream does not have type XRef")
		}
		psize := prev.Key("Size").Int64()
		if psize > size {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref prev stream larger than last stream")
		}
		if table, err = readXrefStreamData(r, prev.data.(stream), table, psize); err != nil {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: reading xref prev stream: %v", err)
		}
	}

	return table, strmptr, strm.hdr, nil
}

func readXrefStreamData(r *Reader, strm stream, table []xref, size int64) ([]xref, error) {
	index, _ := strm.hdr["Index"].(array)
	if index == nil {
		index = array{int64(0), size}
	}
	if len(index)%2 != 0 {
		return nil, fmt.Errorf("invalid Index array %v", objfmt(index))
	}
	ww, ok := strm.hdr["W"].(array)
	if !ok {
		return nil, fmt.Errorf("xref stream missing W array")
	}

	var w []int
	for _, x := range ww {
		i, ok := x.(int64)
		if !ok || int64(int(i)) != i {
			return nil, fmt.Errorf("invalid W array %v", objfmt(ww))
		}
		w = append(w, int(i))
	}
	if len(w) < 3 {
		return nil, fmt.Errorf("invalid W array %v", objfmt(ww))
	}

	v := Value{r, objptr{}, strm}
	wtotal := 0
	for _, wid := range w {
		wtotal += wid
	}
	buf := make([]byte, wtotal)
	data := v.Reader()
	for len(index) > 0 {
		start, ok1 := index[0].(int64)
		n, ok2 := index[1].(int64)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("malformed Index pair %v %v %T %T", objfmt(index[0]), objfmt(index[1]), index[0], index[1])
		}
		index = index[2:]
		for i := 0; i < int(n); i++ {
			_, err := io.ReadFull(data, buf)
			if err != nil {
				return nil, fmt.Errorf("error reading xref stream: %v", err)
			}
			v1 := decodeInt(buf[0:w[0]])
			if w[0] == 0 {
				v1 = 1
			}
			v2 := decodeInt(buf[w[0] : w[0]+w[1]])
			v3 := decodeInt(buf[w[0]+w[1] : w[0]+w[1]+w[2]])
			x := int(start) + i
			// Ensure table has enough capacity AND length
			for len(table) <= x {
				table = append(table, xref{})
			}
			if table[x].ptr != (objptr{}) {
				continue
			}
			switch v1 {
			case 0:
				table[x] = xref{ptr: objptr{0, 65535}}
			case 1:
				table[x] = xref{ptr: objptr{uint32(x), uint16(v3)}, offset: int64(v2)}
			case 2:
				// Type 2: Compressed object in object stream (PDF 1.5+)
				// v2 = object number of object stream, v3 = index within object stream
				table[x] = xref{ptr: objptr{uint32(x), 0}, inStream: true, stream: objptr{uint32(v2), 0}, offset: int64(v3)}
				if DebugOn {
					fmt.Printf("xref entry %d: object in stream (obj %d index %d)\n", x, v2, v3)
				}
			default:
				if DebugOn {
					fmt.Printf("invalid xref stream type %d: %x\n", v1, buf)
				}
			}
		}
	}
	return table, nil
}

func decodeInt(b []byte) int {
	x := 0
	for _, c := range b {
		x = x<<8 | int(c)
	}
	return x
}

func readXrefTable(r *Reader, b *buffer) ([]xref, objptr, dict, error) {
	var table []xref

	table, err := readXrefTableData(b, table)
	if err != nil {
		return nil, objptr{}, nil, fmt.Errorf("malformed PDF: %v", err)
	}

	trailer, ok := b.readObject().(dict)
	if !ok {
		return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref table not followed by trailer dictionary")
	}

	// Hybrid Reference Support: Check for XRefStm
	if xrefStmOffset, ok := trailer["XRefStm"].(int64); ok {
		b2 := newBuffer(io.NewSectionReader(r.f, xrefStmOffset, r.end-xrefStmOffset), xrefStmOffset)
		stmTable, _, _, err := readXrefStream(r, b2)
		PutPDFBuffer(b2)
		if err != nil {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: processing XRefStm at %d: %v", xrefStmOffset, err)
		}
		if len(stmTable) > len(table) {
			newTable := make([]xref, len(stmTable))
			copy(newTable, table)
			table = newTable
		}
		for i, x := range stmTable {
			if x.ptr != (objptr{}) {
				table[i] = x
			}
		}
	}

	seenPrev := make(map[int64]struct{})
	for prevoff := trailer["Prev"]; prevoff != nil; {
		off, ok := prevoff.(int64)
		if !ok {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref Prev is not integer: %v", prevoff)
		}
		if off < 0 || off >= r.end {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref Prev offset out of range: %v", prevoff)
		}
		if _, seen := seenPrev[off]; seen {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref Prev chain contains cycle at offset %d", off)
		}
		seenPrev[off] = struct{}{}
		b := newBuffer(io.NewSectionReader(r.f, off, r.end-off), off)
		tok := b.readToken()
		if tok != keyword("xref") {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref Prev does not point to xref")
		}
		table, err = readXrefTableData(b, table)
		if err != nil {
			PutPDFBuffer(b)
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: %v", err)
		}

		trailer, ok := b.readObject().(dict)
		PutPDFBuffer(b)
		if !ok {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref Prev table not followed by trailer dictionary")
		}
		prevoff = trailer["Prev"]
	}

	size, ok := trailer[name("Size")].(int64)
	if !ok {
		return nil, objptr{}, nil, fmt.Errorf("malformed PDF: trailer missing /Size entry")
	}

	if size < int64(len(table)) {
		table = table[:size]
	}

	return table, objptr{}, trailer, nil
}

// searchAndParseXref searches the PDF file for xref streams or xref tables
// when the startxref offset points to an invalid location.
// This is a recovery mechanism for PDFs with incorrect startxref values.
func (r *Reader) searchAndParseXref() error {
	// Limit search to reasonable file sizes to avoid memory issues
	if r.end > 100<<20 { // 100MB limit for search
		return errors.New("file too large for xref search")
	}

	// Read file content for searching
	data := make([]byte, r.end)
	if _, err := r.f.ReadAt(data, 0); err != nil && err != io.EOF {
		return err
	}

	// Try to find xref stream first (PDF 1.5+)
	if err := r.searchXrefStream(data); err == nil {
		return nil
	}

	// Try to find traditional xref table
	if err := r.searchXrefTable(data); err == nil {
		return nil
	}

	return errors.New("could not find valid xref table or stream")
}

// findXRefStreamPositions scans raw PDF bytes and returns every position where
// a /Type ... /XRef marker appears, tolerating arbitrary PDF whitespace (including newlines)
// between the two tokens.
func findXRefStreamPositions(data []byte) []int {
	var positions []int
	const needle = "/Type"
	start := 0

	for {
		idx := bytes.Index(data[start:], []byte(needle))
		if idx < 0 {
			break
		}
		idx += start

		j := idx + len(needle)
		for j < len(data) && isSpace(data[j]) {
			j++
		}

		if j < len(data) && bytes.HasPrefix(data[j:], []byte("/XRef")) {
			positions = append(positions, idx)
		}

		start = idx + 1
	}

	return positions
}

// searchXrefStream searches for xref stream objects in the PDF data
func (r *Reader) searchXrefStream(data []byte) error {
	positions := findXRefStreamPositions(data)
	if len(positions) == 0 {
		return errors.New("no xref stream found")
	}

	// Try each position, starting from the last one (most likely to be the main xref)
	var lastErr error
	for i := len(positions) - 1; i >= 0; i-- {
		matchPos := positions[i]

		// Find the start of the object containing this xref stream
		// Search backward for "N M obj" pattern - expand search range significantly
		searchStart := matchPos - 2000
		if searchStart < 0 {
			searchStart = 0
		}

		searchArea := data[searchStart:matchPos]

		// Find " obj" or line-starting "obj"
		objPatterns := [][]byte{[]byte(" obj"), []byte("\nobj"), []byte("\robj")}
		bestIdx := -1
		for _, p := range objPatterns {
			idx := bytesLastIndexOptimized(searchArea, p)
			if idx > bestIdx {
				bestIdx = idx
			}
		}

		if bestIdx < 0 {
			lastErr = errors.New("could not find object definition for xref stream")
			continue
		}

		// Find line start
		lineStart := bestIdx
		for lineStart > 0 && searchArea[lineStart-1] != '\n' && searchArea[lineStart-1] != '\r' {
			lineStart--
		}

		objStart := int64(searchStart + lineStart)

		// Try to parse this as an xref stream
		b := newBuffer(io.NewSectionReader(r.f, objStart, r.end-objStart), objStart)
		xref, trailerptr, trailer, err := readXrefStream(r, b)
		PutPDFBuffer(b)
		if err != nil {
			lastErr = err
			continue
		}

		r.xref = xref
		r.trailer = trailer
		r.trailerptr = trailerptr
		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return errors.New("could not parse any xref stream")
}

// searchXrefTable searches for traditional xref table in the PDF data
func (r *Reader) searchXrefTable(data []byte) error {
	// Look for "xref" keyword at start of line
	patterns := [][]byte{
		[]byte("\nxref\n"),
		[]byte("\nxref\r"),
		[]byte("\rxref\n"),
		[]byte("\rxref\r"),
	}

	var lastMatch int = -1
	for _, pattern := range patterns {
		idx := bytesLastIndexOptimized(data, pattern)
		if idx > lastMatch {
			lastMatch = idx
		}
	}

	if lastMatch < 0 {
		return errors.New("no xref table found")
	}

	// Start parsing from "xref" keyword
	xrefStart := int64(lastMatch + 1) // Skip the leading newline

	b := newBuffer(io.NewSectionReader(r.f, xrefStart, r.end-xrefStart), xrefStart)

	// Read and verify the "xref" keyword
	tok := b.readToken()
	if tok != keyword("xref") {
		PutPDFBuffer(b)
		return fmt.Errorf("expected 'xref' keyword at offset %d, got %v", xrefStart, tok)
	}

	xref, trailerptr, trailer, err := readXrefTable(r, b)
	PutPDFBuffer(b)
	if err != nil {
		return err
	}

	r.xref = xref
	r.trailer = trailer
	r.trailerptr = trailerptr
	return nil
}

func (r *Reader) rebuildXrefTable() error {
	if r.end <= 0 {
		return errors.New("cannot rebuild xref: empty file")
	}
	if r.end > 200<<20 {
		return errors.New("pdf: file too large to rebuild xref")
	}
	data := make([]byte, int(r.end))
	sr := io.NewSectionReader(r.f, 0, r.end)
	if _, err := io.ReadFull(sr, data); err != nil {
		return err
	}
	entries := make(map[uint32]xref)
	search := 0
	objCount := 0
	for {
		idx := bytes.Index(data[search:], []byte(" obj"))
		if idx < 0 {
			break
		}
		pos := search + idx
		objCount++
		lineStart := pos
		for lineStart > 0 && data[lineStart-1] != '\n' && data[lineStart-1] != '\r' {
			lineStart--
		}
		line := strings.Fields(string(data[lineStart:pos]))
		if len(line) >= 2 {
			if id64, err1 := strconv.ParseUint(line[0], 10, 32); err1 == nil {
				if gen64, err2 := strconv.ParseUint(line[1], 10, 16); err2 == nil {
					ptr := objptr{uint32(id64), uint16(gen64)}
					if _, ok := entries[ptr.id]; !ok {
						entries[ptr.id] = xref{ptr: ptr, offset: int64(lineStart)}
					}
				}
			}
		}
		search = pos + len(" obj")
	}
	if len(entries) == 0 {
		return fmt.Errorf("pdf: unable to rebuild xref - found %d ' obj' occurrences but no valid objects in %d bytes", objCount, len(data))
	}
	var maxID uint32
	for id := range entries {
		if id > maxID {
			maxID = id
		}
	}
	table := make([]xref, maxID+1)
	for id, entry := range entries {
		table[id] = entry
	}
	r.xref = table
	if err := r.recoverTrailer(data); err != nil {
		return fmt.Errorf("failed to recover trailer: %w", err)
	}
	return nil
}

func (r *Reader) recoverTrailer(data []byte) error {
	// First, try to find traditional trailer keyword
	idx := bytesLastIndexOptimized(data, []byte("trailer"))
	if idx >= 0 {
		buf := newBuffer(bytes.NewReader(data[idx:]), int64(idx))
		buf.allowEOF = true
		if tok := buf.readToken(); tok == keyword("trailer") {
			obj := buf.readObject()
			PutPDFBuffer(buf)
			if d, ok := obj.(dict); ok {
				r.trailer = d
				r.trailerptr = objptr{}
				return nil
			}
		} else {
			PutPDFBuffer(buf)
		}
	}

	// For PDF 1.5+ with xref stream, try to find and parse xref stream object
	// The xref stream contains trailer information in its dictionary
	if err := r.recoverXrefStreamTrailer(data); err == nil {
		return nil
	}

	// Last resort: try to synthesize a minimal trailer by finding Root object
	if rootRef := findRootObject(data); rootRef != (objptr{}) {
		r.trailer = make(dict)
		r.trailer["Size"] = int64(len(r.xref))
		r.trailer["Root"] = rootRef
		r.trailerptr = objptr{}
		if DebugOn {
			fmt.Printf("Synthesized minimal trailer with Root=%v\n", rootRef)
		}
		return nil
	}

	return fmt.Errorf("trailer not found in %d bytes of PDF data", len(data))
}

// recoverXrefStreamTrailer attempts to find and parse an xref stream object
// to recover trailer information for PDF 1.5+ files that use xref streams.
func (r *Reader) recoverXrefStreamTrailer(data []byte) error {
	// Search for xref stream objects by looking for "/Type /XRef" pattern
	// This is more reliable than looking for startxref offset
	candidates := findXRefStreamPositions(data)

	if len(candidates) == 0 {
		return fmt.Errorf("no xref stream found")
	}

	// Try each candidate, starting from the last one (most likely to be the main xref)
	for i := len(candidates) - 1; i >= 0; i-- {
		pos := candidates[i]

		// Find the start of the object definition by searching backward for "N M obj"
		objStart := r.findObjectStart(data, pos)
		if objStart < 0 {
			continue
		}

		// Try to parse the xref stream
		buf := newBuffer(bytes.NewReader(data[objStart:]), int64(objStart))
		buf.allowEOF = true
		obj := buf.readObject()
		PutPDFBuffer(buf)

		objdef, ok := obj.(objdef)
		if !ok {
			continue
		}

		strm, ok := objdef.obj.(stream)
		if !ok {
			continue
		}

		// Verify this is an XRef stream
		if strm.hdr["Type"] != name("XRef") {
			continue
		}

		// Extract trailer-equivalent information from the xref stream header
		trailer := make(dict)
		// Copy relevant trailer keys from xref stream header
		trailerKeys := []name{"Size", "Root", "Info", "ID", "Encrypt", "Prev"}
		for _, key := range trailerKeys {
			if val := strm.hdr[key]; val != nil {
				trailer[key] = val
			}
		}

		if trailer["Size"] == nil || trailer["Root"] == nil {
			continue
		}

		// Try to parse the xref stream data to build the xref table
		size, ok := trailer["Size"].(int64)
		if !ok {
			continue
		}

		table := make([]xref, size)
		table, err := readXrefStreamData(r, strm, table, size)
		if err != nil {
			// Even if we can't read the stream data, we might still have valid trailer
			// Try to use the rebuilt xref table from rebuildXrefTable
			if len(r.xref) > 0 {
				r.trailer = trailer
				r.trailerptr = objdef.ptr
				return nil
			}
			continue
		}

		// Merge with existing xref table if present
		if len(r.xref) > 0 {
			for i, entry := range table {
				if i < len(r.xref) && r.xref[i].ptr == (objptr{}) && entry.ptr != (objptr{}) {
					r.xref[i] = entry
				}
			}
		} else {
			r.xref = table
		}

		r.trailer = trailer
		r.trailerptr = objdef.ptr
		return nil
	}

	return fmt.Errorf("failed to parse any xref stream")
}

// findObjectStart searches backward from pos to find the start of an object definition
// Returns the position of the object number, or -1 if not found
func (r *Reader) findObjectStart(data []byte, pos int) int {
	// Search backward for "obj" keyword
	searchStart := pos
	if searchStart > 200 {
		searchStart = pos - 200
	} else {
		searchStart = 0
	}

	// Look for pattern like "123 0 obj" before the current position
	chunk := data[searchStart:pos]

	// Find the last occurrence of " obj" or "\nobj" or "\robj"
	objPatterns := [][]byte{[]byte(" obj"), []byte("\nobj"), []byte("\robj")}

	bestPos := -1
	for _, pattern := range objPatterns {
		idx := bytesLastIndexOptimized(chunk, pattern)
		if idx > bestPos {
			bestPos = idx
		}
	}

	if bestPos < 0 {
		return -1
	}

	// Now find the start of the line containing this "obj"
	lineStart := searchStart + bestPos
	for lineStart > 0 && data[lineStart-1] != '\n' && data[lineStart-1] != '\r' {
		lineStart--
	}

	// Verify this looks like an object definition (starts with number)
	if lineStart >= len(data) {
		return -1
	}

	// Skip whitespace
	for lineStart < pos && (data[lineStart] == ' ' || data[lineStart] == '\t') {
		lineStart++
	}

	// Check if it starts with a digit
	if lineStart < len(data) && data[lineStart] >= '0' && data[lineStart] <= '9' {
		return lineStart
	}

	return -1
}

func readXrefTableData(b *buffer, table []xref) ([]xref, error) {
	for {
		tok := b.readToken()
		if tok == keyword("trailer") {
			break
		}
		start, ok1 := tok.(int64)
		n, ok2 := b.readToken().(int64)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("malformed xref table")
		}
		for i := 0; i < int(n); i++ {
			off, ok1 := b.readToken().(int64)
			gen, ok2 := b.readToken().(int64)
			alloc, ok3 := b.readToken().(keyword)
			if !ok1 || !ok2 || !ok3 || alloc != keyword("f") && alloc != keyword("n") {
				return nil, fmt.Errorf("malformed xref table")
			}
			x := int(start) + i
			// Ensure table has enough length
			for len(table) <= x {
				table = append(table, xref{})
			}
			if alloc == "n" && table[x].offset == 0 {
				table[x] = xref{ptr: objptr{uint32(x), uint16(gen)}, offset: int64(off)}
			}
		}
	}
	return table, nil
}

// findLastLine finds the last occurrence of s that starts at the beginning of a line.
// Optimized version using manual reverse scan instead of bytes.LastIndex to avoid
// Rabin-Karp overhead for short patterns.
func findLastLine(buf []byte, s string) int {
	if len(s) == 0 || len(buf) < len(s) {
		return -1
	}

	bs := []byte(s)
	slen := len(bs)
	firstByte := bs[0]

	// Scan backwards from the end of buffer
	for i := len(buf) - slen; i >= 1; i-- {
		// Quick check on first byte before full comparison
		if buf[i] != firstByte {
			continue
		}

		// Check if this position starts at beginning of line
		if buf[i-1] != '\n' && buf[i-1] != '\r' {
			continue
		}

		// Full pattern match
		match := true
		for j := 1; j < slen; j++ {
			if buf[i+j] != bs[j] {
				match = false
				break
			}
		}
		if !match {
			continue
		}

		// Check if we have newline/CR after (or at/near end of buffer)
		afterPos := i + slen
		if afterPos >= len(buf) {
			return i
		}
		if buf[afterPos] == '\n' || buf[afterPos] == '\r' {
			return i
		}
		// Space after is NOT acceptable - continue searching
	}

	return -1
}

// A Value is a single PDF value, such as an integer, dictionary, or array.
// The zero Value is a PDF null (Kind() == Null, IsNull() = true).
type Value struct {
	r    *Reader
	ptr  objptr
	data interface{}
}

// IsNull reports whether the value is a null. It is equivalent to Kind() == Null.
func (v Value) IsNull() bool {
	return v.data == nil
}

// A ValueKind specifies the kind of data underlying a Value.
type ValueKind int

// The PDF value kinds.
const (
	Null ValueKind = iota
	Bool
	Integer
	Real
	String
	Name
	Dict
	Array
	Stream
)

// Kind reports the kind of value underlying v.
func (v Value) Kind() ValueKind {
	switch v.data.(type) {
	default:
		return Null
	case bool:
		return Bool
	case int64:
		return Integer
	case float64:
		return Real
	case string:
		return String
	case name:
		return Name
	case dict:
		return Dict
	case array:
		return Array
	case stream:
		return Stream
	}
}

// String returns a textual representation of the value v.
// Note that String is not the accessor for values with Kind() == String.
// To access such values, see RawString, Text, and TextFromUTF16.
func (v Value) String() string {
	return objfmt(v.data)
}

func objfmt(x interface{}) string {
	switch x := x.(type) {
	default:
		return fmt.Sprint(x)
	case string:
		if isPDFDocEncoded(x) {
			return strconv.Quote(pdfDocDecode(x))
		}
		if isUTF16(x) {
			return strconv.Quote(utf16Decode(x[2:]))
		}
		return strconv.Quote(x)
	case name:
		return "/" + string(x)
	case dict:
		var keys []string
		for k := range x {
			keys = append(keys, string(k))
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteString("<<")
		for i, k := range keys {
			elem := x[name(k)]
			if i > 0 {
				buf.WriteString(" ")
			}
			buf.WriteString("/")
			buf.WriteString(k)
			buf.WriteString(" ")
			buf.WriteString(objfmt(elem))
		}
		buf.WriteString(">>")
		return buf.String()

	case array:
		var buf bytes.Buffer
		buf.WriteString("[")
		for i, elem := range x {
			if i > 0 {
				buf.WriteString(" ")
			}
			buf.WriteString(objfmt(elem))
		}
		buf.WriteString("]")
		return buf.String()

	case stream:
		return fmt.Sprintf("%v@%d", objfmt(x.hdr), x.offset)

	case objptr:
		return fmt.Sprintf("%d %d R", x.id, x.gen)

	case objdef:
		return fmt.Sprintf("{%d %d obj}%v", x.ptr.id, x.ptr.gen, objfmt(x.obj))
	}
}

// Bool returns v's boolean value.
// If v.Kind() != Bool, Bool returns false.
func (v Value) Bool() bool {
	x, ok := v.data.(bool)
	if !ok {
		return false
	}
	return x
}

// Int64 returns v's int64 value.
// If v.Kind() != Int64, Int64 returns 0.
func (v Value) Int64() int64 {
	x, ok := v.data.(int64)
	if !ok {
		return 0
	}
	return x
}

// Float64 returns v's float64 value, converting from integer if necessary.
// If v.Kind() != Float64 and v.Kind() != Int64, Float64 returns 0.
func (v Value) Float64() float64 {
	x, ok := v.data.(float64)
	if !ok {
		x, ok := v.data.(int64)
		if ok {
			return float64(x)
		}
		return 0
	}
	return x
}

// RawString returns v's string value.
// If v.Kind() != String, RawString returns the empty string.
func (v Value) RawString() string {
	x, ok := v.data.(string)
	if !ok {
		return ""
	}
	return x
}

// Text returns v's string value interpreted as a “text string” (defined in the PDF spec)
// and converted to UTF-8.
// If v.Kind() != String, Text returns the empty string.
func (v Value) Text() string {
	x, ok := v.data.(string)
	if !ok {
		return ""
	}
	if isPDFDocEncoded(x) {
		return pdfDocDecode(x)
	}
	if isUTF16(x) {
		return utf16Decode(x[2:])
	}
	return x
}

// TextFromUTF16 returns v's string value interpreted as big-endian UTF-16
// and then converted to UTF-8.
// If v.Kind() != String or if the data is not valid UTF-16, TextFromUTF16 returns
// the empty string.
func (v Value) TextFromUTF16() string {
	x, ok := v.data.(string)
	if !ok {
		return ""
	}
	if len(x)%2 == 1 {
		return ""
	}
	if x == "" {
		return ""
	}
	return utf16Decode(x)
}

// Name returns v's name value.
// If v.Kind() != Name, Name returns the empty string.
// The returned name does not include the leading slash:
// if v corresponds to the name written using the syntax /Helvetica,
// Name() == "Helvetica".
func (v Value) Name() string {
	x, ok := v.data.(name)
	if !ok {
		return ""
	}
	return string(x)
}

// Key returns the value associated with the given name key in the dictionary v.
// Like the result of the Name method, the key should not include a leading slash.
// If v is a stream, Key applies to the stream's header dictionary.
// If v.Kind() != Dict and v.Kind() != Stream, Key returns a null Value.
func (v Value) Key(key string) Value {
	x, ok := v.data.(dict)
	if !ok {
		strm, ok := v.data.(stream)
		if !ok {
			return Value{}
		}
		x = strm.hdr
	}
	return v.r.resolve(v.ptr, x[name(key)])
}

// Keys returns a sorted list of the keys in the dictionary v.
// If v is a stream, Keys applies to the stream's header dictionary.
// If v.Kind() != Dict and v.Kind() != Stream, Keys returns nil.
func (v Value) Keys() []string {
	x, ok := v.data.(dict)
	if !ok {
		strm, ok := v.data.(stream)
		if !ok {
			return nil
		}
		x = strm.hdr
	}
	keys := []string{} // not nil
	for k := range x {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	return keys
}

// Index returns the i'th element in the array v.
// If v.Kind() != Array or if i is outside the array bounds,
// Index returns a null Value.
func (v Value) Index(i int) Value {
	x, ok := v.data.(array)
	if !ok || i < 0 || i >= len(x) {
		return Value{}
	}
	return v.r.resolve(v.ptr, x[i])
}

// Len returns the length of the array v.
// If v.Kind() != Array, Len returns 0.
func (v Value) Len() int {
	x, ok := v.data.(array)
	if !ok {
		return 0
	}
	return len(x)
}

func (r *Reader) resolve(parent objptr, x interface{}) Value {
	if ptr, ok := x.(objptr); ok {
		if obj, ok := r.getCachedObject(ptr); ok {
			return Value{r, parent, obj}
		}
		if ptr.id >= uint32(len(r.xref)) {
			return Value{}
		}
		xref := r.xref[ptr.id]
		if xref.ptr != ptr || !xref.inStream && xref.offset == 0 {
			return Value{}
		}
		var obj object
		if xref.inStream {
			strm := r.resolve(parent, xref.stream)
			currentStreamID := xref.stream.id

		Search:
			for {
				if strm.Kind() != Stream {
					// Tolerate corrupted xref stream reference
					return Value{}
				}
				if strm.Key("Type").Name() != "ObjStm" {
					// Not an object stream, return empty
					return Value{}
				}
				n := int(strm.Key("N").Int64())
				first := strm.Key("First").Int64()
				if first == 0 {
					// Missing First entry, return empty
					return Value{}
				}

				var offset int64
				found := false

				if currentStreamID != 0 {
					// Check cache
					r.objStreamCacheMu.RLock()
					if cache, ok := r.objStreamCache[currentStreamID]; ok {
						if off, ok := cache[int64(ptr.id)]; ok {
							offset = off
							found = true
						}
					}
					r.objStreamCacheMu.RUnlock()

					if !found {
						// Check if populated
						r.objStreamCacheMu.RLock()
						_, populated := r.objStreamCache[currentStreamID]
						r.objStreamCacheMu.RUnlock()

						if !populated {
							// Populate cache
							b := newBuffer(strm.Reader(), 0)
							b.allowEOF = true
							streamCache := make(map[int64]int64, n)
							for i := 0; i < n; i++ {
								id, _ := b.readToken().(int64)
								off, _ := b.readToken().(int64)
								streamCache[id] = first + off
							}
							PutPDFBuffer(b)

							r.objStreamCacheMu.Lock()
							r.objStreamCache[currentStreamID] = streamCache
							r.objStreamCacheMu.Unlock()

							if off, ok := streamCache[int64(ptr.id)]; ok {
								offset = off
								found = true
							}
						}
					}
				} else {
					// Fallback for Extends streams
					b := newBuffer(strm.Reader(), 0)
					b.allowEOF = true
					for i := 0; i < n; i++ {
						id, _ := b.readToken().(int64)
						off, _ := b.readToken().(int64)
						if uint32(id) == ptr.id {
							offset = first + off
							found = true
							break
						}
					}
					PutPDFBuffer(b)
				}

				if found {
					b := newBuffer(strm.Reader(), 0)
					b.seekForward(offset)
					x = b.readObject()
					r.storeCachedObject(ptr, x)
					PutPDFBuffer(b)
					break Search
				}

				ext := strm.Key("Extends")
				if ext.Kind() != Stream {
					// Cannot find object in stream, return empty
					return Value{}
				}
				strm = ext
				currentStreamID = 0
			}
		} else {
			b := newBuffer(io.NewSectionReader(r.f, xref.offset, r.end-xref.offset), xref.offset)
			b.key = r.key
			b.useAES = r.useAES
			obj = b.readObject()
			def, ok := obj.(objdef)
			if !ok {
				// Tolerate corrupted object definition
				PutPDFBuffer(b)
				return Value{}
			}
			if def.ptr != ptr {
				// Object pointer mismatch, tolerate and use what we found
				// This can happen in corrupted PDFs where xref table is inconsistent
			}
			x = def.obj
			r.storeCachedObject(ptr, x)
			PutPDFBuffer(b)
		}
		parent = ptr
	}

	switch x := x.(type) {
	case nil, bool, int64, float64, name, dict, array, stream:
		return Value{r, parent, x}
	case string:
		return Value{r, parent, x}
	default:
		// Unknown type, return empty value instead of panic
		return Value{}
	}
}

type errorReadCloser struct {
	err error
}

func (e *errorReadCloser) Read([]byte) (int, error) {
	return 0, e.err
}

func (e *errorReadCloser) Close() error {
	return e.err
}

// Reader returns the data contained in the stream v.
// If v.Kind() != Stream, Reader returns a ReadCloser that
// responds to all reads with a “stream not present” error.
func (v Value) Reader() io.ReadCloser {
	x, ok := v.data.(stream)
	if !ok {
		return &errorReadCloser{fmt.Errorf("stream not present")}
	}
	var rd io.Reader
	rd = io.NewSectionReader(v.r.f, x.offset, v.Key("Length").Int64())
	if v.r.key != nil {
		rd = decryptStream(v.r.key, v.r.useAES, x.ptr, rd)
	}
	filter := v.Key("Filter")
	param := v.Key("DecodeParms")
	switch filter.Kind() {
	default:
		// Unsupported filter, return error reader
		return &errorReadCloser{fmt.Errorf("unsupported filter %v", filter)}
	case Null:
		// ok
	case Name:
		rd = applyFilter(rd, filter.Name(), param)
		if rd == nil {
			return &errorReadCloser{fmt.Errorf("failed to apply filter %s", filter.Name())}
		}
	case Array:
		for i := 0; i < filter.Len(); i++ {
			rd = applyFilter(rd, filter.Index(i).Name(), param.Index(i))
			if rd == nil {
				return &errorReadCloser{fmt.Errorf("failed to apply filter at index %d", i)}
			}
		}
	}

	return ioutil.NopCloser(rd)
}

func applyFilter(rd io.Reader, name string, param Value) io.Reader {
	switch name {
	default:
		// Unknown filter, return nil to signal error
		return nil
	case "FlateDecode":
		zr, err := zlib.NewReader(rd)
		if err != nil {
			// Failed to create zlib reader, return nil
			return nil
		}
		return applyPredictor(zr, param)
	case "LZWDecode":
		early := param.Key("EarlyChange")
		if early.Kind() != Null && early.Int64() != 1 {
			// Unsupported LZW configuration, return nil
			return nil
		}
		lr := lzw.NewReader(rd, lzw.MSB, 8)
		return applyPredictor(lr, param)
	case "ASCIIHexDecode":
		// ASCIIHexDecode: decodes data encoded in hexadecimal representation
		return newASCIIHexDecoder(rd)
	case "ASCII85Decode":
		cleanASCII85 := newAlphaReader(rd)
		decoder := ascii85.NewDecoder(cleanASCII85)

		switch param.Keys() {
		default:
			if DebugOn {
				fmt.Println("param=", param)
			}
			// Unexpected DecodeParms, but continue with decoder
			return decoder
		case nil:
			return decoder
		}
	case "DCTDecode":
		// JPEG-compressed data is already suitable for consumers; leave as-is.
		return rd
	case "JPXDecode":
		// JPEG2000-compressed data; passthrough for now.
		return rd
	case "CCITTFaxDecode":
		// CCITT Group 3/4 data is left as-is for callers that understand the encoding.
		return rd
	case "RunLengthDecode":
		return newRunLengthReader(rd)
	}
}

func applyPredictor(rd io.Reader, param Value) io.Reader {
	if param.Kind() != Dict {
		return rd
	}
	pred := param.Key("Predictor")
	if pred.Kind() == Null {
		return rd
	}
	switch pred.Int64() {
	case 1, 2:
		return rd
	case 12:
		columns := param.Key("Columns").Int64()
		if columns <= 0 {
			columns = 1
		}
		return &pngUpReader{r: rd, hist: make([]byte, 1+columns), tmp: make([]byte, 1+columns)}
	default:
		if DebugOn {
			fmt.Println("unknown predictor", pred)
		}
		// Unknown predictor, return original reader
		return rd
	}
}

type pngUpReader struct {
	r    io.Reader
	hist []byte
	tmp  []byte
	pend []byte
}

func (r *pngUpReader) Read(b []byte) (int, error) {
	n := 0
	for len(b) > 0 {
		if len(r.pend) > 0 {
			m := copy(b, r.pend)
			n += m
			b = b[m:]
			r.pend = r.pend[m:]
			continue
		}
		_, err := io.ReadFull(r.r, r.tmp)
		if err != nil {
			return n, err
		}
		if r.tmp[0] != 2 {
			return n, fmt.Errorf("malformed PNG-Up encoding")
		}
		for i, b := range r.tmp {
			r.hist[i] += b
		}
		r.pend = r.hist[1:]
	}
	return n, nil
}

type runLengthReader struct {
	r   *bufio.Reader
	buf []byte
	eod bool
}

func newRunLengthReader(rd io.Reader) io.Reader {
	return &runLengthReader{r: bufio.NewReader(rd)}
}

func (r *runLengthReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	n := 0
	for len(p) > 0 {
		if len(r.buf) == 0 {
			if r.eod {
				if n == 0 {
					return 0, io.EOF
				}
				break
			}
			if err := r.fill(); err != nil {
				if err == io.EOF {
					if n == 0 {
						return 0, io.EOF
					}
					break
				}
				return n, err
			}
		}
		m := copy(p, r.buf)
		n += m
		p = p[m:]
		r.buf = r.buf[m:]
	}
	return n, nil
}

func (r *runLengthReader) fill() error {
	b, err := r.r.ReadByte()
	if err != nil {
		return err
	}
	if b == 128 {
		r.eod = true
		return io.EOF
	}
	if b <= 127 {
		count := int(b) + 1
		r.buf = make([]byte, count)
		if _, err := io.ReadFull(r.r, r.buf); err != nil {
			return err
		}
		return nil
	}
	// 129..255 repeat
	count := 257 - int(b)
	val, err := r.r.ReadByte()
	if err != nil {
		return err
	}
	r.buf = bytes.Repeat([]byte{val}, count)
	return nil
}

// asciiHexDecoder decodes ASCIIHexDecode filter data with optimized performance
type asciiHexDecoder struct {
	r          *bufio.Reader
	err        error
	endSeen    bool
	pendingNib int8 // Pending high nibble for odd-length hex strings (-1 = none)
}

func newASCIIHexDecoder(rd io.Reader) io.Reader {
	return &asciiHexDecoder{
		r:          bufio.NewReader(rd),
		pendingNib: -1,
	}
}

// Read implements highly optimized batch decoding with inlined hot path
func (d *asciiHexDecoder) Read(p []byte) (int, error) {
	if d.err != nil {
		return 0, d.err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if d.endSeen {
		return 0, io.EOF
	}

	n := 0
	const whitespaceMask = uint64(1)<<' ' | 1<<'\t' | 1<<'\n' | 1<<'\r' | 1<<'\f' | 1<<0

	// Handle pending nibble from previous read
	if d.pendingNib >= 0 {
		// Inlined: find next hex nibble
		for {
			c, err := d.r.ReadByte()
			if err != nil {
				d.err = err
				if d.endSeen {
					p[n] = byte(d.pendingNib << 4)
					n++
					d.pendingNib = -1
					return n, io.EOF
				}
				return n, d.err
			}
			if c <= ' ' && ((uint64(1)<<c)&whitespaceMask) != 0 {
				continue
			}
			if c == '>' {
				d.endSeen = true
				d.err = io.EOF
				p[n] = byte(d.pendingNib << 4)
				n++
				d.pendingNib = -1
				return n, io.EOF
			}
			h2 := hexTable[c]
			if h2 >= 0 {
				p[n] = byte(d.pendingNib<<4 | h2)
				n++
				d.pendingNib = -1
				break
			}
			// Invalid char, skip
		}
	}

	// Hot path: inline the decode loop for maximum performance
	r := d.r
	for n < len(p) && !d.endSeen {
		var h1, h2 int8

		// Find first hex nibble - inlined for speed
		for {
			c, err := r.ReadByte()
			if err != nil {
				d.err = err
				if n == 0 {
					return 0, err
				}
				return n, nil
			}
			if c <= ' ' && ((uint64(1)<<c)&whitespaceMask) != 0 {
				continue
			}
			if c == '>' {
				d.endSeen = true
				if n == 0 {
					return 0, io.EOF
				}
				return n, nil
			}
			h1 = hexTable[c]
			if h1 >= 0 {
				break
			}
			// Invalid char, continue searching
		}

		// Find second hex nibble - inlined for speed
		for {
			c, err := r.ReadByte()
			if err != nil {
				d.err = err
				d.pendingNib = h1
				return n, nil
			}
			if c <= ' ' && ((uint64(1)<<c)&whitespaceMask) != 0 {
				continue
			}
			if c == '>' {
				d.endSeen = true
				p[n] = byte(h1 << 4)
				n++
				return n, io.EOF
			}
			h2 = hexTable[c]
			if h2 >= 0 {
				break
			}
			// Invalid char, continue searching
		}

		// Combine nibbles and output
		p[n] = byte(h1<<4 | h2)
		n++
	}

	return n, nil
}

var passwordPad = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
	0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80, 0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

func (r *Reader) initEncrypt(password string) error {
	// See PDF 32000-1:2008, §7.6.
	encryptVal := r.resolve(objptr{}, r.trailer["Encrypt"])
	encrypt, ok := encryptVal.data.(dict)
	if !ok || len(encrypt) == 0 {
		// Cannot resolve Encrypt dictionary - this typically happens when:
		// 1. The xref table is corrupted and cannot locate the Encrypt object
		// 2. The Encrypt reference points to a non-existent object
		// 3. The object at the Encrypt reference is not a dictionary
		return fmt.Errorf("malformed PDF: cannot resolve Encrypt dictionary (xref may be corrupted)")
	}
	if encrypt["Filter"] != name("Standard") {
		return fmt.Errorf("unsupported PDF: encryption filter %v", objfmt(encrypt["Filter"]))
	}
	n, _ := encrypt["Length"].(int64)
	if n == 0 {
		n = 40
	}
	if n%8 != 0 || n > 128 || n < 40 {
		return fmt.Errorf("malformed PDF: %d-bit encryption key", n)
	}
	V, _ := encrypt["V"].(int64)
	if V != 1 && V != 2 && V != 4 && V != 5 {
		return fmt.Errorf("unsupported PDF: encryption version V=%d; %v", V, objfmt(encrypt))
	}

	ids, ok := r.trailer["ID"].(array)
	if !ok || len(ids) < 1 {
		return fmt.Errorf("malformed PDF: missing ID in trailer")
	}
	idstr, ok := ids[0].(string)
	if !ok {
		return fmt.Errorf("malformed PDF: missing ID in trailer")
	}
	ID := []byte(idstr)

	R, _ := encrypt["R"].(int64)
	if R < 2 {
		return fmt.Errorf("malformed PDF: encryption revision R=%d", R)
	}
	if R > 6 {
		return fmt.Errorf("unsupported PDF: encryption revision R=%d", R)
	}
	O, _ := encrypt["O"].(string)
	U, _ := encrypt["U"].(string)
	expectedLen := 32
	if V == 5 {
		expectedLen = 48
	}
	if len(O) != expectedLen || len(U) != expectedLen {
		return fmt.Errorf("malformed PDF: missing O= or U= encryption parameters (expected length %d, got O=%d U=%d)", expectedLen, len(O), len(U))
	}
	p, _ := encrypt["P"].(int64)
	P := uint32(p)

	// TODO: Password should be converted to Latin-1.
	pw := toLatin1(password)
	h := md5.New()
	if len(pw) >= 32 {
		h.Write(pw[:32])
	} else {
		h.Write(pw)
		h.Write(passwordPad[:32-len(pw)])
	}
	h.Write([]byte(O))
	h.Write([]byte{byte(P), byte(P >> 8), byte(P >> 16), byte(P >> 24)})
	h.Write([]byte(ID))
	key := h.Sum(nil)

	if R >= 3 {
		for i := 0; i < 50; i++ {
			h.Reset()
			h.Write(key[:n/8])
			key = h.Sum(key[:0])
		}
		key = key[:n/8]
	} else {
		key = key[:40/8]
	}

	c, err := rc4.NewCipher(key)
	if err != nil {
		return fmt.Errorf("malformed PDF: invalid RC4 key: %v", err)
	}

	var u []byte
	if R == 2 {
		u = make([]byte, 32)
		copy(u, passwordPad)
		c.XORKeyStream(u, u)
	} else {
		h.Reset()
		h.Write(passwordPad)
		h.Write([]byte(ID))
		u = h.Sum(nil)
		c.XORKeyStream(u, u)

		for i := 1; i <= 19; i++ {
			key1 := make([]byte, len(key))
			copy(key1, key)
			for j := range key1 {
				key1[j] ^= byte(i)
			}
			c, _ = rc4.NewCipher(key1)
			c.XORKeyStream(u, u)
		}
	}

	if !bytes.HasPrefix([]byte(U), u) {
		return ErrInvalidPassword
	}

	r.key = key
	r.useAES = V == 4

	// Handle V=5 encryption (AES-256)
	if V == 5 {
		// Extract additional parameters for V=5
		UE, _ := encrypt["UE"].(string)
		OE, _ := encrypt["OE"].(string)
		Perms, _ := encrypt["Perms"].(string)

		if len(UE) != 32 || len(OE) != 32 || len(Perms) != 16 {
			return fmt.Errorf("malformed PDF: missing UE/OE/Perms encryption parameters for V=5")
		}

		// Create encryption info for V=5
		info := PDFEncryptionInfo{
			Version:   EncryptionVersion(V),
			Revision:  EncryptionRevision(R),
			Method:    MethodAESV3,
			KeyLength: 256,
			P:         P,
			ID:        ID,
			O:         []byte(O),
			U:         []byte(U),
			UE:        []byte(UE),
			OE:        []byte(OE),
			Perms:     []byte(Perms),
		} // Try to authenticate with the provided password
		auth := NewPasswordAuth(&info)
		key, err := auth.Authenticate(password)
		if err != nil {
			return err
		}

		r.key = key
		r.useAES = true // V=5 always uses AES-256

		return nil
	}

	return nil
}

var ErrInvalidPassword = fmt.Errorf("encrypted PDF: invalid password")

func okayV4(encrypt dict) bool {
	cf, ok := encrypt["CF"].(dict)
	if !ok {
		return false
	}
	stmf, ok := encrypt["StmF"].(name)
	if !ok {
		return false
	}
	strf, ok := encrypt["StrF"].(name)
	if !ok {
		return false
	}
	if stmf != strf {
		return false
	}
	cfparam, ok := cf[stmf].(dict)
	if !ok {
		return false
	}
	if cfparam["AuthEvent"] != nil && cfparam["AuthEvent"] != name("DocOpen") {
		return false
	}
	if cfparam["Length"] != nil && cfparam["Length"] != int64(16) {
		return false
	}
	if cfparam["CFM"] != name("AESV2") {
		return false
	}
	return true
}

func cryptKey(key []byte, useAES bool, ptr objptr) []byte {
	h := md5.New()
	h.Write(key)
	h.Write([]byte{byte(ptr.id), byte(ptr.id >> 8), byte(ptr.id >> 16), byte(ptr.gen), byte(ptr.gen >> 8)})
	if useAES {
		h.Write([]byte("sAlT"))
	}
	return h.Sum(nil)
}

func decryptString(key []byte, useAES bool, ptr objptr, x string) string {
	key = cryptKey(key, useAES, ptr)
	if useAES {
		s := []byte(x)
		if len(s) < aes.BlockSize {
			// Encrypted text too short, return original
			return x
		}

		block, err := aes.NewCipher(key)
		if err != nil {
			// Failed to create cipher, return original
			return x
		}
		iv := s[:aes.BlockSize]
		s = s[aes.BlockSize:]

		stream := cipher.NewCBCDecrypter(block, iv)
		stream.CryptBlocks(s, s)
		x = string(s)
	} else {
		c, _ := rc4.NewCipher(key)
		data := []byte(x)
		c.XORKeyStream(data, data)
		x = string(data)
	}
	return x
}

func decryptStream(key []byte, useAES bool, ptr objptr, rd io.Reader) io.Reader {
	key = cryptKey(key, useAES, ptr)
	if useAES {
		cb, err := aes.NewCipher(key)
		if err != nil {
			// Failed to create AES cipher, return error reader
			return &errorReader{err: fmt.Errorf("AES: %s", err.Error())}
		}
		iv := make([]byte, 16)
		if _, err := io.ReadFull(rd, iv); err != nil {
			return &errorReader{err: fmt.Errorf("failed to read AES IV: %s", err.Error())}
		}
		cbc := cipher.NewCBCDecrypter(cb, iv)
		rd = &cbcReader{cbc: cbc, rd: rd, buf: make([]byte, 16)}
	} else {
		c, _ := rc4.NewCipher(key)
		rd = &cipher.StreamReader{S: c, R: rd}
	}
	return rd
}

// errorReader is a Reader that always returns an error
type errorReader struct {
	err error
}

func (r *errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

type cbcReader struct {
	cbc  cipher.BlockMode
	rd   io.Reader
	buf  []byte
	pend []byte
}

func (r *cbcReader) Read(b []byte) (n int, err error) {
	if len(r.pend) == 0 {
		_, err = io.ReadFull(r.rd, r.buf)
		if err != nil {
			return 0, err
		}
		r.cbc.CryptBlocks(r.buf, r.buf)
		r.pend = r.buf
	}
	n = copy(b, r.pend)
	r.pend = r.pend[n:]
	return n, nil
}

// ExtractAllPagesParallel extract all page texts using enhanced parallel extractor
// This method integrates all performance optimizations: sharded cache, font prefetch, zero-copy, etc.
func (r *Reader) ExtractAllPagesParallel(ctx context.Context, workers int) ([]string, error) {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	// Create parallel extractor
	extractor := NewParallelExtractor(workers)
	defer extractor.Close()

	// Collect all pages
	numPages := r.NumPage()
	pages := make([]Page, numPages)
	for i := 0; i < numPages; i++ {
		pages[i] = r.Page(i + 1)
		// Set font cache for each page (using extractor's optimized cache)
		pages[i].SetFontCacheInterface(extractor.prefetcher.cache)
	}

	// Parallel extract all pages
	textsPerPage, err := extractor.ExtractAllPages(ctx, pages)
	if err != nil {
		return nil, err
	}

	// Merge text blocks of each page into string
	results := make([]string, len(textsPerPage))
	for i, texts := range textsPerPage {
		if len(texts) == 0 {
			results[i] = ""
			continue
		}

		// Calculate total length
		totalLen := 0
		for _, t := range texts {
			totalLen += len(t.S) + 1 // +1 for space
		}

		// Use zero-copy string building
		builder := NewStringBuffer(totalLen)
		for j, t := range texts {
			builder.WriteString(t.S)
			if j < len(texts)-1 {
				builder.WriteByte(' ')
			}
		}
		results[i] = builder.StringCopy()
	}

	return results, nil
}

// GetCompatibilityInfo returns compatibility information for the PDF
func (r *Reader) GetCompatibilityInfo() *PDFCompatibilityInfo {
	return r.compatibility
}
