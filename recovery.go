// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pdf recovery functions for handling malformed PDFs.

package pdf

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// IntegrityStatus represents the result of a PDF integrity check
type IntegrityStatus struct {
	// IsValid indicates whether the PDF is valid enough to parse
	IsValid bool
	// IsTruncated indicates whether the file appears to be truncated
	IsTruncated bool
	// HasValidHeader indicates whether a valid PDF header was found
	HasValidHeader bool
	// HasValidEOF indicates whether a valid %%EOF marker was found
	HasValidEOF bool
	// HasStartxref indicates whether a startxref marker was found
	HasStartxref bool
	// HasXref indicates whether xref table or stream was found
	HasXref bool
	// HasTrailer indicates whether trailer dictionary was found
	HasTrailer bool
	// EstimatedObjects is the estimated number of objects in the file
	EstimatedObjects int
	// Issues contains descriptions of any problems found
	Issues []string
}

// CheckIntegrity performs a quick integrity check on a PDF file
func CheckIntegrity(f io.ReaderAt, size int64) *IntegrityStatus {
	status := &IntegrityStatus{
		IsValid: true,
	}

	// Check minimum size
	if size < 20 {
		status.IsValid = false
		status.Issues = append(status.Issues, "file too small to be a valid PDF")
		return status
	}

	// Read header
	header := make([]byte, 1024)
	headerLen := 1024
	if size < int64(headerLen) {
		headerLen = int(size)
	}
	f.ReadAt(header[:headerLen], 0)
	header = header[:headerLen]

	// Check PDF header
	if idx := bytes.Index(header, []byte("%PDF-")); idx >= 0 {
		status.HasValidHeader = true
		// Check for excessive junk before header (tolerance: 1024 bytes)
		if idx > 1024 {
			status.Issues = append(status.Issues, fmt.Sprintf("PDF header found at offset %d", idx))
		}
	} else {
		status.IsValid = false
		status.HasValidHeader = false
		status.Issues = append(status.Issues, "missing PDF header")
		return status
	}

	// Read end of file
	endChunk := int64(4096)
	if size < endChunk {
		endChunk = size
	}
	tail := make([]byte, endChunk)
	f.ReadAt(tail, size-endChunk)

	// Check for %%EOF marker
	if bytes.Contains(tail, []byte("%%EOF")) {
		status.HasValidEOF = true
	} else {
		status.IsTruncated = true
		status.Issues = append(status.Issues, "missing %%EOF marker (file may be truncated)")
	}

	// Check for startxref
	if bytes.Contains(tail, []byte("startxref")) {
		status.HasStartxref = true
	} else {
		status.Issues = append(status.Issues, "missing startxref marker")
	}

	// Check for xref table or stream
	if bytes.Contains(tail, []byte("xref")) || bytes.Contains(tail, []byte("/Type /XRef")) || bytes.Contains(tail, []byte("/Type/XRef")) {
		status.HasXref = true
	} else {
		status.Issues = append(status.Issues, "xref table/stream not found in expected location")
	}

	// Check for trailer
	if bytes.Contains(tail, []byte("trailer")) || status.HasXref {
		status.HasTrailer = true
	} else {
		status.Issues = append(status.Issues, "trailer not found")
	}

	// Estimate object count by sampling
	sampleSize := int64(512 * 1024) // 512KB sample
	if size < sampleSize {
		sampleSize = size
	}
	sample := make([]byte, sampleSize)
	f.ReadAt(sample, 0)

	// Count "obj" occurrences
	objCount := bytes.Count(sample, []byte(" obj"))
	if size > sampleSize {
		// Extrapolate
		objCount = int(float64(objCount) * float64(size) / float64(sampleSize))
	}
	status.EstimatedObjects = objCount

	// Determine overall validity
	if !status.HasValidHeader {
		status.IsValid = false
	} else if !status.HasStartxref && !status.HasXref {
		// Can potentially recover, but mark as problematic
		status.IsValid = len(status.Issues) < 3
	}

	return status
}

// RecoveryOptions controls how PDF recovery is attempted
type RecoveryOptions struct {
	// MaxSearchSize limits how many bytes to search for recovery
	MaxSearchSize int64
	// AllowTruncated attempts to recover truncated files
	AllowTruncated bool
	// AllowMissingXref attempts to rebuild xref from object markers
	AllowMissingXref bool
	// AllowMissingTrailer attempts to recover without trailer
	AllowMissingTrailer bool
	// Verbose enables detailed recovery logging
	Verbose bool
}

// DefaultRecoveryOptions returns sensible defaults for recovery
func DefaultRecoveryOptions() *RecoveryOptions {
	return &RecoveryOptions{
		MaxSearchSize:       50 << 20, // 50MB
		AllowTruncated:      true,
		AllowMissingXref:    true,
		AllowMissingTrailer: true,
		Verbose:             DebugOn,
	}
}

// findStartxrefEnhanced uses multiple strategies to find startxref
// Returns the offset of startxref keyword and the xref offset value
func findStartxrefEnhanced(f io.ReaderAt, size int64, opts *RecoveryOptions) (startxrefPos int64, xrefOffset int64, err error) {
	if opts == nil {
		opts = DefaultRecoveryOptions()
	}

	// Strategy 1: Search from the end (standard location)
	searchSizes := []int64{1024, 4096, 16384, 65536, 256 * 1024}

	for _, searchSize := range searchSizes {
		if searchSize > size {
			searchSize = size
		}

		buf := make([]byte, searchSize)
		readOffset := size - searchSize
		if readOffset < 0 {
			readOffset = 0
		}

		n, _ := f.ReadAt(buf, readOffset)
		buf = buf[:n]

		// Try to find startxref
		pos, xref := parseStartxref(buf)
		if pos >= 0 {
			return readOffset + int64(pos), xref, nil
		}
	}

	// Strategy 2: Search the entire file for the last startxref
	if size <= opts.MaxSearchSize {
		data := make([]byte, size)
		if _, err := f.ReadAt(data, 0); err != nil && err != io.EOF {
			return -1, 0, err
		}

		pos, xref := parseStartxref(data)
		if pos >= 0 {
			return int64(pos), xref, nil
		}
	}

	return -1, 0, fmt.Errorf("startxref not found in file")
}

// parseStartxref finds the last startxref in buffer and returns its position and value
func parseStartxref(buf []byte) (pos int, xrefOffset int64) {
	// Find all occurrences of startxref
	searchBuf := buf
	lastPos := -1
	lastOffset := int64(-1)

	for {
		idx := bytesLastIndexOptimized(searchBuf, []byte("startxref"))
		if idx < 0 {
			break
		}

		// Verify it's on its own line
		validStart := idx == 0 || searchBuf[idx-1] == '\n' || searchBuf[idx-1] == '\r'
		if !validStart {
			searchBuf = searchBuf[:idx]
			continue
		}

		// Parse the offset value after startxref
		afterStartxref := idx + len("startxref")
		if afterStartxref >= len(searchBuf) {
			searchBuf = searchBuf[:idx]
			continue
		}

		// Skip whitespace
		numStart := afterStartxref
		for numStart < len(searchBuf) && isSpace(searchBuf[numStart]) {
			numStart++
		}

		// Find end of number
		numEnd := numStart
		for numEnd < len(searchBuf) && searchBuf[numEnd] >= '0' && searchBuf[numEnd] <= '9' {
			numEnd++
		}

		if numEnd > numStart {
			if offset, err := strconv.ParseInt(string(searchBuf[numStart:numEnd]), 10, 64); err == nil {
				lastPos = idx
				lastOffset = offset
				// Found a valid one, but keep searching for a later one
				searchBuf = searchBuf[:idx]
				continue
			}
		}

		searchBuf = searchBuf[:idx]
	}

	return lastPos, lastOffset
}

// findXrefTableDirect searches for xref table directly in file content
func findXrefTableDirect(data []byte) (offset int64, err error) {
	// Look for "xref" keyword at the start of a line
	patterns := [][]byte{
		[]byte("\nxref\n"),
		[]byte("\nxref\r"),
		[]byte("\rxref\n"),
		[]byte("\rxref\r"),
		[]byte("\nxref "),
		[]byte("\rxref "),
	}

	// Find the last occurrence (main xref table)
	lastIdx := -1
	for _, pattern := range patterns {
		idx := bytesLastIndexOptimized(data, pattern)
		if idx > lastIdx {
			lastIdx = idx
		}
	}

	if lastIdx >= 0 {
		return int64(lastIdx + 1), nil // +1 to skip the leading newline
	}

	// Also check at the very beginning of file
	if bytes.HasPrefix(data, []byte("xref\n")) || bytes.HasPrefix(data, []byte("xref\r")) {
		return 0, nil
	}

	return -1, fmt.Errorf("xref table not found")
}

// findXrefStreamDirect searches for xref stream objects directly
func findXrefStreamDirect(data []byte) (offset int64, err error) {
	// Look for /Type /XRef pattern which indicates xref stream
	patterns := [][]byte{
		[]byte("/Type/XRef"),
		[]byte("/Type /XRef"),
		[]byte("/Type  /XRef"),
	}

	// Find all candidates
	var candidates []int
	for _, pattern := range patterns {
		idx := 0
		for {
			pos := bytes.Index(data[idx:], pattern)
			if pos < 0 {
				break
			}
			candidates = append(candidates, idx+pos)
			idx = idx + pos + len(pattern)
		}
	}

	if len(candidates) == 0 {
		return -1, fmt.Errorf("xref stream not found")
	}

	// Use the last candidate (most likely the main xref)
	lastCandidate := candidates[len(candidates)-1]

	// Search backward to find the object start
	searchStart := lastCandidate - 200
	if searchStart < 0 {
		searchStart = 0
	}

	searchArea := data[searchStart:lastCandidate]

	// Find " obj" pattern
	objPatterns := [][]byte{[]byte(" obj"), []byte("\nobj"), []byte("\robj")}
	bestIdx := -1
	for _, p := range objPatterns {
		idx := bytesLastIndexOptimized(searchArea, p)
		if idx > bestIdx {
			bestIdx = idx
		}
	}

	if bestIdx < 0 {
		return -1, fmt.Errorf("could not find object start for xref stream")
	}

	// Find line start
	lineStart := bestIdx
	for lineStart > 0 && searchArea[lineStart-1] != '\n' && searchArea[lineStart-1] != '\r' {
		lineStart--
	}

	return int64(searchStart + lineStart), nil
}

// rebuildXrefFromObjects scans the file and builds xref from object markers
func rebuildXrefFromObjects(data []byte) ([]xref, error) {
	entries := make(map[uint32]xref)

	// Find all "N M obj" patterns
	search := 0
	objMarker := []byte(" obj")

	for {
		idx := bytes.Index(data[search:], objMarker)
		if idx < 0 {
			break
		}

		pos := search + idx

		// Find line start
		lineStart := pos
		for lineStart > 0 && data[lineStart-1] != '\n' && data[lineStart-1] != '\r' {
			lineStart--
		}

		// Parse object ID and generation
		line := string(data[lineStart:pos])
		fields := strings.Fields(line)

		if len(fields) >= 2 {
			if id, err := strconv.ParseUint(fields[len(fields)-2], 10, 32); err == nil {
				if gen, err := strconv.ParseUint(fields[len(fields)-1], 10, 16); err == nil {
					ptr := objptr{uint32(id), uint16(gen)}
					if _, exists := entries[ptr.id]; !exists {
						entries[ptr.id] = xref{ptr: ptr, offset: int64(lineStart)}
					}
				}
			}
		}

		search = pos + len(objMarker)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no valid objects found")
	}

	// Convert to slice
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

	return table, nil
}

// findTrailerDict searches for trailer dictionary in data
func findTrailerDict(data []byte) (dict, error) {
	// Look for "trailer" keyword
	idx := bytesLastIndexOptimized(data, []byte("trailer"))
	if idx >= 0 {
		// Parse the dictionary following trailer
		afterTrailer := idx + len("trailer")

		// Skip whitespace
		for afterTrailer < len(data) && isSpace(data[afterTrailer]) {
			afterTrailer++
		}

		if afterTrailer < len(data) && data[afterTrailer] == '<' {
			buf := newBuffer(bytes.NewReader(data[afterTrailer:]), int64(afterTrailer))
			buf.allowEOF = true
			obj := buf.readObject()
			PutPDFBuffer(buf)

			if d, ok := obj.(dict); ok {
				return d, nil
			}
		}
	}

	return nil, fmt.Errorf("trailer dictionary not found")
}

// findTrailerFromXrefStream extracts trailer info from xref stream
func findTrailerFromXrefStream(data []byte, streamOffset int64) (dict, error) {
	buf := newBuffer(bytes.NewReader(data[streamOffset:]), streamOffset)
	buf.allowEOF = true
	obj := buf.readObject()
	PutPDFBuffer(buf)

	if objdef, ok := obj.(objdef); ok {
		if strm, ok := objdef.obj.(stream); ok {
			if strm.hdr["Type"] == name("XRef") {
				// Extract trailer-equivalent fields
				trailer := make(dict)
				trailerKeys := []name{"Size", "Root", "Info", "ID", "Encrypt", "Prev"}
				for _, key := range trailerKeys {
					if val := strm.hdr[key]; val != nil {
						trailer[key] = val
					}
				}

				if trailer["Size"] != nil && trailer["Root"] != nil {
					return trailer, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("could not extract trailer from xref stream")
}

// RecoverPDF attempts to recover a malformed PDF
func RecoverPDF(f io.ReaderAt, size int64, opts *RecoveryOptions) (*Reader, error) {
	if opts == nil {
		opts = DefaultRecoveryOptions()
	}

	if opts.Verbose {
		fmt.Println("Attempting PDF recovery...")
	}

	// Try standard parsing first
	r, err := NewReader(f, size)
	if err == nil {
		return r, nil
	}

	if opts.Verbose {
		fmt.Printf("Standard parsing failed: %v\n", err)
	}

	// Read file data for recovery
	if size > opts.MaxSearchSize {
		return nil, fmt.Errorf("file too large for recovery (%d bytes)", size)
	}

	data := make([]byte, size)
	if _, err := f.ReadAt(data, 0); err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read file: %v", err)
	}

	// Verify PDF header
	headerLen := 1024
	if len(data) < headerLen {
		headerLen = len(data)
	}
	if !bytes.Contains(data[:headerLen], []byte("%PDF-")) {
		return nil, fmt.Errorf("not a PDF file: missing header")
	}

	// Create reader
	r = &Reader{
		f:              f,
		end:            size,
		fontCache:      NewFontCache(),
		cacheCap:       2000,
		objStreamCache: make(map[uint32]map[int64]int64),
	}

	// Try to find and parse xref
	var xrefTable []xref
	var trailer dict

	// Strategy 1: Try to find xref stream
	if xrefOffset, err := findXrefStreamDirect(data); err == nil {
		if opts.Verbose {
			fmt.Printf("Found xref stream at offset %d\n", xrefOffset)
		}

		buf := newBuffer(bytes.NewReader(data[xrefOffset:]), xrefOffset)
		buf.allowEOF = true
		if xr, _, tr, err := readXrefStream(r, buf); err == nil {
			xrefTable = xr
			trailer = tr
		}
		PutPDFBuffer(buf)
	}

	// Strategy 2: Try to find traditional xref table
	if xrefTable == nil {
		if xrefOffset, err := findXrefTableDirect(data); err == nil {
			if opts.Verbose {
				fmt.Printf("Found xref table at offset %d\n", xrefOffset)
			}

			buf := newBuffer(bytes.NewReader(data[xrefOffset:]), xrefOffset)
			buf.allowEOF = true
			if tok := buf.readToken(); tok == keyword("xref") {
				if xr, _, tr, err := readXrefTable(r, buf); err == nil {
					xrefTable = xr
					trailer = tr
				}
			}
			PutPDFBuffer(buf)
		}
	}

	// Strategy 3: Rebuild xref from object markers
	if xrefTable == nil && opts.AllowMissingXref {
		if opts.Verbose {
			fmt.Println("Rebuilding xref from object markers...")
		}

		if xr, err := rebuildXrefFromObjects(data); err == nil {
			xrefTable = xr
		}
	}

	if xrefTable == nil {
		return nil, fmt.Errorf("failed to recover xref table")
	}

	r.xref = xrefTable

	// Find trailer if not already found
	if trailer == nil {
		// Try traditional trailer
		if tr, err := findTrailerDict(data); err == nil {
			trailer = tr
		}
	}

	// Try to extract from xref stream if still not found
	if trailer == nil {
		if xrefOffset, err := findXrefStreamDirect(data); err == nil {
			if tr, err := findTrailerFromXrefStream(data, xrefOffset); err == nil {
				trailer = tr
			}
		}
	}

	// Synthesize minimal trailer if allowed
	if trailer == nil && opts.AllowMissingTrailer {
		if opts.Verbose {
			fmt.Println("Synthesizing minimal trailer...")
		}

		trailer = make(dict)
		trailer["Size"] = int64(len(xrefTable))

		// Try to find Root object
		if rootRef := findRootObject(data); rootRef != (objptr{}) {
			trailer["Root"] = rootRef
		}
	}

	if trailer == nil || trailer["Root"] == nil {
		return nil, fmt.Errorf("failed to recover trailer (missing Root)")
	}

	r.trailer = trailer

	if opts.Verbose {
		fmt.Printf("Recovery successful: %d objects, Root=%v\n", len(xrefTable), trailer["Root"])
	}

	return r, nil
}

// recoverPDFInternal attempts recovery on an existing Reader
// This is used as a fallback when standard xref parsing fails
func recoverPDFInternal(r *Reader, opts *RecoveryOptions) error {
	if opts == nil {
		opts = DefaultRecoveryOptions()
	}

	if opts.Verbose {
		fmt.Println("Attempting internal PDF recovery...")
	}

	// Read file data for recovery
	size := r.end
	if size > opts.MaxSearchSize {
		return fmt.Errorf("file too large for recovery (%d bytes)", size)
	}

	data := make([]byte, size)
	if _, err := r.f.ReadAt(data, 0); err != nil && err != io.EOF {
		return fmt.Errorf("failed to read file: %v", err)
	}

	// Try to find and parse xref
	var xrefTable []xref
	var trailer dict

	// Strategy 1: Try to find xref stream
	if xrefOffset, err := findXrefStreamDirect(data); err == nil {
		if opts.Verbose {
			fmt.Printf("Found xref stream at offset %d\n", xrefOffset)
		}

		buf := newBuffer(bytes.NewReader(data[xrefOffset:]), xrefOffset)
		buf.allowEOF = true
		if xr, _, tr, err := readXrefStream(r, buf); err == nil {
			xrefTable = xr
			trailer = tr
		}
		PutPDFBuffer(buf)
	}

	// Strategy 2: Try to find traditional xref table
	if xrefTable == nil {
		if xrefOffset, err := findXrefTableDirect(data); err == nil {
			if opts.Verbose {
				fmt.Printf("Found xref table at offset %d\n", xrefOffset)
			}

			buf := newBuffer(bytes.NewReader(data[xrefOffset:]), xrefOffset)
			buf.allowEOF = true
			if xr, _, tr, err := readXref(r, buf); err == nil {
				xrefTable = xr
				trailer = tr
			}
			PutPDFBuffer(buf)
		}
	}

	// Strategy 3: Rebuild from object markers
	if xrefTable == nil && opts.AllowMissingXref {
		if opts.Verbose {
			fmt.Println("Rebuilding xref from object markers...")
		}
		xrefTable, _ = rebuildXrefFromObjects(data)
	}

	// Strategy 4: Try to recover trailer
	if trailer == nil && opts.AllowMissingTrailer {
		if opts.Verbose {
			fmt.Println("Recovering trailer...")
		}
		trailer, _ = findTrailerDict(data)

		if trailer == nil {
			// Try to find trailer from xref stream
			// Find the xref offset first
			var xrefOff int64
			if off, err := findXrefStreamDirect(data); err == nil {
				xrefOff = off
			}
			trailer, _ = findTrailerFromXrefStream(data, xrefOff)
		}
	}

	// Last resort: create minimal trailer
	if trailer == nil && len(xrefTable) > 0 {
		trailer = make(dict)
		trailer["Size"] = int64(len(xrefTable))

		// Try to find Root object
		if rootRef := findRootObject(data); rootRef != (objptr{}) {
			trailer["Root"] = rootRef
		}
	}

	if trailer == nil || trailer["Root"] == nil {
		return fmt.Errorf("failed to recover trailer (missing Root)")
	}

	r.xref = xrefTable
	r.trailer = trailer

	if opts.Verbose {
		fmt.Printf("Internal recovery successful: %d objects, Root=%v\n", len(xrefTable), trailer["Root"])
	}

	return nil
}

// findRootObject searches for the document catalog object
func findRootObject(data []byte) objptr {
	// Look for /Type /Catalog
	patterns := [][]byte{
		[]byte("/Type/Catalog"),
		[]byte("/Type /Catalog"),
	}

	for _, pattern := range patterns {
		idx := bytes.Index(data, pattern)
		if idx < 0 {
			continue
		}

		// Search backward for object definition
		searchStart := idx - 200
		if searchStart < 0 {
			searchStart = 0
		}

		searchArea := data[searchStart:idx]
		objIdx := bytesLastIndexOptimized(searchArea, []byte(" obj"))
		if objIdx < 0 {
			continue
		}

		// Find line start
		lineStart := objIdx
		for lineStart > 0 && searchArea[lineStart-1] != '\n' && searchArea[lineStart-1] != '\r' {
			lineStart--
		}

		// Parse object ID
		line := strings.Fields(string(searchArea[lineStart:objIdx]))
		if len(line) >= 2 {
			if id, err := strconv.ParseUint(line[len(line)-2], 10, 32); err == nil {
				if gen, err := strconv.ParseUint(line[len(line)-1], 10, 16); err == nil {
					return objptr{uint32(id), uint16(gen)}
				}
			}
		}
	}

	return objptr{}
}

// minInt returns the smaller of two int values
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// minInt64 returns the smaller of two int64 values
func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
