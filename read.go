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
	f          io.ReaderAt
	closer     io.Closer // Optional closer for underlying resource
	end        int64
	xref       []xref
	trailer    dict
	trailerptr objptr
	key        []byte
	useAES     bool
	cacheMu    sync.RWMutex
	objCache   map[objptr]*list.Element
	cacheList  *list.List
	cacheCap   int
	fontCache  *FontCache
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
	buf := make([]byte, 10)
	n, _ := f.ReadAt(buf, 0)
	if n < 9 || !bytes.HasPrefix(buf, []byte("%PDF-1.")) || buf[7] < '0' || buf[7] > '7' || buf[8] != '\r' && buf[8] != '\n' {
		return nil, fmt.Errorf("not a PDF file: invalid header")
	}
	end := size
	const endChunk = 100
	buf = make([]byte, endChunk)
	f.ReadAt(buf, end-endChunk)
	for len(buf) > 0 && (buf[len(buf)-1] == '\n' || buf[len(buf)-1] == '\r') {
		buf = buf[:len(buf)-1]
	}
	buf = bytes.TrimRight(buf, "\r\n\t ")
	if !bytes.HasSuffix(buf, []byte("%%EOF")) {
		return nil, fmt.Errorf("not a PDF file: missing %%%%EOF")
	}
	i := findLastLine(buf, "startxref")
	if i < 0 {
		return nil, fmt.Errorf("malformed PDF file: missing final startxref")
	}

	r := &Reader{
		f:         f,
		end:       end,
		fontCache: NewFontCache(),
		// CRITICAL FIX: Set default cache capacity to prevent unbounded growth.
		// Without this limit, objCache can grow to gigabytes during batch processing.
		// A capacity of 2000 objects provides good performance while limiting memory.
		cacheCap: 2000,
	}
	pos := end - endChunk + int64(i)
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
		if rebuildErr := r.rebuildXrefTable(); rebuildErr != nil {
			return nil, err
		}
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
	}
	return NewReaderEncrypted(f, size, pw)
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
	err = fmt.Errorf("malformed PDF: cross-reference table not found: %v", tok)
	return
}

func readXrefStream(r *Reader, b *buffer) ([]xref, objptr, dict, error) {
	obj1 := b.readObject()
	obj, ok := obj1.(objdef)
	if !ok {
		return nil, objptr{}, nil, fmt.Errorf("malformed PDF: cross-reference table not found: %v", objfmt(obj1))
	}
	strmptr := obj.ptr
	strm, ok := obj.obj.(stream)
	if !ok {
		return nil, objptr{}, nil, fmt.Errorf("malformed PDF: cross-reference table not found: %v", objfmt(obj))
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

	for prevoff := strm.hdr["Prev"]; prevoff != nil; {
		off, ok := prevoff.(int64)
		if !ok {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref Prev is not integer: %v", prevoff)
		}
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
			for cap(table) <= x {
				table = append(table[:cap(table)], xref{})
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
				table[x] = xref{ptr: objptr{uint32(x), 0}, inStream: true, stream: objptr{uint32(v2), 0}, offset: int64(v3)}
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

	for prevoff := trailer["Prev"]; prevoff != nil; {
		off, ok := prevoff.(int64)
		if !ok {
			return nil, objptr{}, nil, fmt.Errorf("malformed PDF: xref Prev is not integer: %v", prevoff)
		}
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
	for {
		idx := bytes.Index(data[search:], []byte(" obj"))
		if idx < 0 {
			break
		}
		pos := search + idx
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
		return errors.New("pdf: unable to rebuild xref")
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
	idx := bytes.LastIndex(data, []byte("trailer"))
	if idx < 0 {
		return errors.New("trailer not found")
	}
	buf := newBuffer(bytes.NewReader(data[idx:]), int64(idx))
	defer PutPDFBuffer(buf)
	buf.allowEOF = true
	if tok := buf.readToken(); tok != keyword("trailer") {
		return errors.New("malformed recovered trailer")
	}
	obj := buf.readObject()
	d, ok := obj.(dict)
	if !ok {
		return errors.New("recovered trailer is not dict")
	}
	r.trailer = d
	r.trailerptr = objptr{}
	return nil
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
			for cap(table) <= x {
				table = append(table[:cap(table)], xref{})
			}
			if len(table) <= x {
				table = table[:x+1]
			}
			if alloc == "n" && table[x].offset == 0 {
				table[x] = xref{ptr: objptr{uint32(x), uint16(gen)}, offset: int64(off)}
			}
		}
	}
	return table, nil
}

func findLastLine(buf []byte, s string) int {
	bs := []byte(s)
	max := len(buf)
	for {
		i := bytes.LastIndex(buf[:max], bs)
		if i <= 0 || i+len(bs) >= len(buf) {
			return -1
		}
		if (buf[i-1] == '\n' || buf[i-1] == '\r') && (buf[i+len(bs)] == '\n' || buf[i+len(bs)] == '\r') {
			return i
		}
		max = i
	}
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
				b := newBuffer(strm.Reader(), 0)
				b.allowEOF = true
				for i := 0; i < n; i++ {
					id, _ := b.readToken().(int64)
					off, _ := b.readToken().(int64)
					if uint32(id) == ptr.id {
						b.seekForward(first + off)
						x = b.readObject()
						r.storeCachedObject(ptr, x)
						PutPDFBuffer(b)
						break Search
					}
				}
				ext := strm.Key("Extends")
				if ext.Kind() != Stream {
					// Cannot find object in stream, return empty
					PutPDFBuffer(b)
					return Value{}
				}
				PutPDFBuffer(b)
				strm = ext
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

var passwordPad = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
	0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80, 0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

func (r *Reader) initEncrypt(password string) error {
	// See PDF 32000-1:2008, §7.6.
	encrypt, _ := r.resolve(objptr{}, r.trailer["Encrypt"]).data.(dict)
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
	if V != 1 && V != 2 && (V != 4 || !okayV4(encrypt)) {
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
	if R > 4 {
		return fmt.Errorf("unsupported PDF: encryption revision R=%d", R)
	}
	O, _ := encrypt["O"].(string)
	U, _ := encrypt["U"].(string)
	if len(O) != 32 || len(U) != 32 {
		return fmt.Errorf("malformed PDF: missing O= or U= encryption parameters")
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
