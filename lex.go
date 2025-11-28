// Copyright 2014 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Reading of PDF tokens and objects from a raw byte stream.

package pdf

import (
	"fmt"
	"io"
	"strconv"
)

// A token is a PDF token in the input stream, one of the following Go types:
//
//	bool, a PDF boolean
//	int64, a PDF integer
//	float64, a PDF real
//	string, a PDF string literal
//	keyword, a PDF keyword
//	name, a PDF name without the leading slash
type token interface{}

// A name is a PDF name, without the leading slash.
type name string

// A keyword is a PDF keyword.
// Delimiter tokens used in higher-level syntax,
// such as "<<", ">>", "[", "]", "{", "}", are also treated as keywords.
type keyword string

// A buffer holds buffered input bytes from the PDF file.
type buffer struct {
	r           io.Reader // source of data
	buf         []byte    // buffered data
	pos         int       // read index in buf
	offset      int64     // offset at end of buf; aka offset of next read
	tmp         []byte    // scratch space for accumulating token
	unread      []token   // queue of read but then unread tokens
	allowEOF    bool
	allowObjptr bool
	allowStream bool
	eof         bool
	readErr     error // stores read error instead of panicking
	key         []byte
	useAES      bool
	objptr      objptr
	// Context support for cancellation
	ctxChecker *contextChecker
	limits     *ParseLimits
}

// newBuffer returns a new buffer reading from r at the given offset.
func newBuffer(r io.Reader, offset int64) *buffer {
	b := GetPDFBuffer()
	b.r = r
	b.offset = offset
	return b
}

func (b *buffer) seek(offset int64) {
	b.offset = offset
	b.buf = b.buf[:0]
	b.pos = 0
	b.unread = b.unread[:0]
}

func (b *buffer) readByte() byte {
	if b.pos >= len(b.buf) {
		b.reload()
		if b.pos >= len(b.buf) {
			return '\n'
		}
	}
	c := b.buf[b.pos]
	b.pos++
	return c
}

func (b *buffer) reload() bool {
	// Defensive check: buffer must have a valid reader
	// If b.r is nil, it indicates a buffer pool corruption or use-after-Put bug
	if b.r == nil {
		// Store error instead of panicking - this buffer was incorrectly reused
		b.readErr = fmt.Errorf("malformed PDF: buffer reader is nil (possible buffer pool corruption at offset %d)", b.offset)
		b.eof = true
		b.buf = b.buf[:0]
		b.pos = 0
		return false
	}

	n := cap(b.buf) - int(b.offset%int64(cap(b.buf)))
	n, err := b.r.Read(b.buf[:n])
	if n == 0 && err != nil {
		b.buf = b.buf[:0]
		b.pos = 0
		if b.allowEOF && err == io.EOF {
			b.eof = true
			return false
		}
		// Store error instead of panicking - treat as EOF for reading
		b.readErr = fmt.Errorf("malformed PDF: reading at offset %d: %v", b.offset, err)
		b.eof = true
		return false
	}
	b.offset += int64(n)
	b.buf = b.buf[:n]
	b.pos = 0
	return true
}

func (b *buffer) seekForward(offset int64) {
	for b.offset < offset {
		if !b.reload() {
			return
		}
	}
	b.pos = len(b.buf) - int(b.offset-offset)
}

func (b *buffer) readOffset() int64 {
	return b.offset - int64(len(b.buf)) + int64(b.pos)
}

func (b *buffer) unreadByte() {
	if b.pos > 0 {
		b.pos--
	}
}

func (b *buffer) unreadToken(t token) {
	b.unread = append(b.unread, t)
}

func (b *buffer) readToken() token {
	if n := len(b.unread); n > 0 {
		t := b.unread[n-1]
		b.unread = b.unread[:n-1]
		return t
	}

	// Find first non-space, non-comment byte.
	c := b.readByte()
	for {
		if isSpace(c) {
			if b.eof {
				return io.EOF
			}
			c = b.readByte()
		} else if c == '%' {
			for c != '\r' && c != '\n' {
				c = b.readByte()
			}
		} else {
			break
		}
	}

	switch c {
	case '<':
		if b.readByte() == '<' {
			return keyword("<<")
		}
		b.unreadByte()
		return b.readHexStringSIMDAdvanced()

	case '(':
		return b.readLiteralString()

	case '[', ']', '{', '}':
		return keyword(string(c))

	case '/':
		return b.readName()

	case '>':
		if b.readByte() == '>' {
			return keyword(">>")
		}
		b.unreadByte()
		fallthrough

	default:
		if isDelim(c) {
			// Tolerate unexpected delimiter in corrupted PDFs
			// Return nil to signal end of token stream
			return nil
		}
		b.unreadByte()
		return b.readKeyword()
	}
}

// readHexString reads a hex string from the buffer.
// This optimized version uses batch reading, memory pre-allocation, and fast whitespace checking.
func (b *buffer) readHexString() token {
	tmp := b.tmp[:0]

	maxBytes := 0
	if b.limits != nil && b.limits.MaxHexStringBytes > 0 {
		maxBytes = b.limits.MaxHexStringBytes
	}

	// Pre-allocate tmp slice to reduce allocations for known sizes
	if maxBytes > 0 && maxBytes <= 1024*1024 { // Reasonable pre-allocation limit
		tmp = make([]byte, 0, maxBytes)
	}

	// Cancellation check interval - check every N bytes decoded
	checkInterval := 256
	if b.ctxChecker != nil && b.ctxChecker.checkInterval > 0 && b.ctxChecker.checkInterval < checkInterval {
		checkInterval = b.ctxChecker.checkInterval
	}
	bytesDecoded := 0

	for {
		// Check size limit
		if maxBytes > 0 && len(tmp) >= maxBytes {
			b.skipToHexEnd()
			b.tmp = tmp
			return string(tmp)
		}

		// Fast batch processing: read directly from buffer
		for b.pos < len(b.buf) {
			c := b.buf[b.pos]
			b.pos++

			// Skip whitespace using fast bit mask check
			// Bit mask for common whitespace: space(32), tab(9), lf(10), cr(13), ff(12), null(0)
			if c <= ' ' && ((uint64(1)<<c)&0x100002600) != 0 {
				continue
			}

			// End of hex string
			if c == '>' {
				b.tmp = tmp
				return string(tmp)
			}

			// Decode first hex digit
			x1 := hexTable[c]
			if x1 < 0 {
				continue // Invalid hex char, skip
			}

			// Find second hex digit
			var x2 int8 = -1
			for b.pos < len(b.buf) {
				c2 := b.buf[b.pos]
				b.pos++

				// Skip whitespace using fast bit mask
				if c2 <= ' ' && ((uint64(1)<<c2)&0x100002600) != 0 {
					continue
				}

				// End of hex string with odd digit
				if c2 == '>' {
					tmp = append(tmp, byte(x1<<4))
					b.tmp = tmp
					return string(tmp)
				}

				x2 = hexTable[c2]
				break
			}

			// Need to reload buffer for second digit
			if x2 < 0 && b.pos >= len(b.buf) {
				if !b.reload() {
					// EOF - odd digit
					tmp = append(tmp, byte(x1<<4))
					b.tmp = tmp
					return string(tmp)
				}
				// Continue searching for second digit
				for b.pos < len(b.buf) {
					c2 := b.buf[b.pos]
					b.pos++
					if c2 <= ' ' && ((uint64(1)<<c2)&0x100002600) != 0 {
						continue
					}
					if c2 == '>' {
						tmp = append(tmp, byte(x1<<4))
						b.tmp = tmp
						return string(tmp)
					}
					x2 = hexTable[c2]
					break
				}
			}

			if x2 >= 0 {
				tmp = append(tmp, byte(x1<<4|x2))
				bytesDecoded++

				// Periodic cancellation check
				if bytesDecoded >= checkInterval {
					bytesDecoded = 0
					if b.ctxChecker != nil && b.ctxChecker.CheckNow() {
						b.tmp = tmp
						return nil
					}
				}
			}
			// Check size limit after adding byte
			if maxBytes > 0 && len(tmp) >= maxBytes {
				b.skipToHexEnd()
				b.tmp = tmp
				return string(tmp)
			}
		}

		// Need more data - also check cancellation before blocking on reload
		if b.ctxChecker != nil && b.ctxChecker.CheckNow() {
			b.tmp = tmp
			return nil
		}
		if !b.reload() {
			b.tmp = tmp
			return string(tmp)
		}
	}
}

// readHexStringSIMD provides SIMD-like batch processing for hex string parsing
// This function uses optimized batch operations for better performance on large strings
func (b *buffer) readHexStringSIMD() token {
	tmp := b.tmp[:0]

	maxBytes := 0
	if b.limits != nil && b.limits.MaxHexStringBytes > 0 {
		maxBytes = b.limits.MaxHexStringBytes
	}

	// Pre-allocate tmp slice to reduce allocations for known sizes
	if maxBytes > 0 && maxBytes <= 1024*1024 { // Reasonable pre-allocation limit
		tmp = make([]byte, 0, maxBytes)
	}

	// Cancellation check interval - check every N bytes decoded
	checkInterval := 256
	if b.ctxChecker != nil && b.ctxChecker.checkInterval > 0 && b.ctxChecker.checkInterval < checkInterval {
		checkInterval = b.ctxChecker.checkInterval
	}
	bytesDecoded := 0

	// Bit mask for whitespace characters
	const whitespaceMask = uint64(1)<<' ' | 1<<'\t' | 1<<'\n' | 1<<'\r' | 1<<'\f' | 1<<0

	for {
		// Check size limit
		if maxBytes > 0 && len(tmp) >= maxBytes {
			b.skipToHexEnd()
			b.tmp = tmp
			return string(tmp)
		}

		// Fast batch processing: read directly from buffer
		for b.pos < len(b.buf) {
			c := b.buf[b.pos]
			b.pos++

			// Skip whitespace using fast bit mask check
			if c <= ' ' && ((uint64(1)<<c)&whitespaceMask) != 0 {
				continue
			}

			// End of hex string
			if c == '>' {
				b.tmp = tmp
				return string(tmp)
			}

			// Decode first hex digit
			x1 := hexTable[c]
			if x1 < 0 {
				continue // Invalid hex char, skip
			}

			// Find second hex digit - optimized for common case where digits are adjacent
			var x2 int8 = -1
			for b.pos < len(b.buf) {
				c2 := b.buf[b.pos]
				b.pos++

				// Skip whitespace using fast bit mask
				if c2 <= ' ' && ((uint64(1)<<c2)&whitespaceMask) != 0 {
					continue
				}

				// End of hex string with odd digit
				if c2 == '>' {
					tmp = append(tmp, byte(x1<<4))
					b.tmp = tmp
					return string(tmp)
				}

				x2 = hexTable[c2]
				break
			}

			// Need to reload buffer for second digit
			if x2 < 0 && b.pos >= len(b.buf) {
				if !b.reload() {
					// EOF - odd digit
					tmp = append(tmp, byte(x1<<4))
					b.tmp = tmp
					return string(tmp)
				}
				// Continue searching for second digit
				for b.pos < len(b.buf) {
					c2 := b.buf[b.pos]
					b.pos++
					if c2 <= ' ' && ((uint64(1)<<c2)&whitespaceMask) != 0 {
						continue
					}
					if c2 == '>' {
						tmp = append(tmp, byte(x1<<4))
						b.tmp = tmp
						return string(tmp)
					}
					x2 = hexTable[c2]
					break
				}
			}

			if x2 >= 0 {
				tmp = append(tmp, byte(x1<<4|x2))
				bytesDecoded++

				// Periodic cancellation check
				if bytesDecoded >= checkInterval {
					bytesDecoded = 0
					if b.ctxChecker != nil && b.ctxChecker.CheckNow() {
						b.tmp = tmp
						return nil
					}
				}
			}
			// Check size limit after adding byte
			if maxBytes > 0 && len(tmp) >= maxBytes {
				b.skipToHexEnd()
				b.tmp = tmp
				return string(tmp)
			}
		}

		// Need more data - also check cancellation before blocking on reload
		if b.ctxChecker != nil && b.ctxChecker.CheckNow() {
			b.tmp = tmp
			return nil
		}
		if !b.reload() {
			b.tmp = tmp
			return string(tmp)
		}
	}
}

// readHexStringSIMDAdvanced provides advanced SIMD-optimized hex string parsing
// This function uses true SIMD operations for maximum performance
func (b *buffer) readHexStringSIMDAdvanced() token {
	tmp := b.tmp[:0]

	maxBytes := 0
	if b.limits != nil && b.limits.MaxHexStringBytes > 0 {
		maxBytes = b.limits.MaxHexStringBytes
	}

	// Pre-allocate tmp slice to reduce allocations for known sizes
	if maxBytes > 0 && maxBytes <= 1024*1024 { // Reasonable pre-allocation limit
		tmp = make([]byte, 0, maxBytes)
	}

	// Bit mask for whitespace characters
	const whitespaceMask = uint64(1)<<' ' | 1<<'\t' | 1<<'\n' | 1<<'\r' | 1<<'\f' | 1<<0

	// Collect hex data for batch processing
	var hexData []byte
	collecting := true

	for collecting {
		// Check size limit
		if maxBytes > 0 && len(tmp) >= maxBytes {
			b.skipToHexEnd()
			b.tmp = tmp
			return string(tmp)
		}

		// Fast batch processing: read directly from buffer
		for b.pos < len(b.buf) {
			c := b.buf[b.pos]
			b.pos++

			// Skip whitespace using fast bit mask check
			if c <= ' ' && ((uint64(1)<<c)&whitespaceMask) != 0 {
				continue
			}

			// End of hex string
			if c == '>' {
				collecting = false
				break
			}

			// Collect hex character
			hexData = append(hexData, c)
		}

		// Need more data - also check cancellation before blocking on reload
		if b.ctxChecker != nil && b.ctxChecker.CheckNow() {
			b.tmp = tmp
			return nil
		}
		if !b.reload() {
			collecting = false
		}
	}

	// Process collected hex data using SIMD operations
	if len(hexData) > 0 {
		result, err := HexDecodeSIMD("<" + string(hexData) + ">")
		if err == nil && len(result) > 0 {
			// Apply size limit before appending
			if maxBytes > 0 && len(tmp)+len(result) > maxBytes {
				result = result[:maxBytes-len(tmp)]
			}
			tmp = append(tmp, result...)
		}
	}

	b.tmp = tmp
	return string(tmp)
}

// skipToHexEnd skips to the end of a hex string ('>') efficiently
func (b *buffer) skipToHexEnd() {
	for {
		for b.pos < len(b.buf) {
			if b.buf[b.pos] == '>' {
				b.pos++
				return
			}
			b.pos++
		}
		if !b.reload() {
			return
		}
	}
}

func unhex(b byte) int {
	return int(hexTable[b])
}

// hexTable is a lookup table for hex decoding.
// Values: 0-15 for valid hex digits, 255 for invalid characters.
// Using a lookup table is significantly faster than switch statements.
var hexTable = [256]int8{
	'0': 0, '1': 1, '2': 2, '3': 3, '4': 4, '5': 5, '6': 6, '7': 7, '8': 8, '9': 9,
	'a': 10, 'b': 11, 'c': 12, 'd': 13, 'e': 14, 'f': 15,
	'A': 10, 'B': 11, 'C': 12, 'D': 13, 'E': 14, 'F': 15,
}

func init() {
	// Initialize non-hex values to -1
	for i := range hexTable {
		if hexTable[i] == 0 && i != '0' {
			hexTable[i] = -1
		}
	}
}

func (b *buffer) readLiteralString() token {
	tmp := b.tmp[:0]
	depth := 1
Loop:
	for !b.eof {
		c := b.readByte()
		switch c {
		default:
			tmp = append(tmp, c)
		case '(':
			depth++
			tmp = append(tmp, c)
		case ')':
			if depth--; depth == 0 {
				break Loop
			}
			tmp = append(tmp, c)
		case '\\':
			switch c = b.readByte(); c {
			case 'n':
				tmp = append(tmp, '\n')
			case 'r':
				tmp = append(tmp, '\r')
			case 'b':
				tmp = append(tmp, '\b')
			case 't':
				tmp = append(tmp, '\t')
			case 'f':
				tmp = append(tmp, '\f')
			case '(', ')', '\\':
				tmp = append(tmp, c)
			case '\r':
				if b.readByte() != '\n' {
					b.unreadByte()
				}
				fallthrough
			case '\n':
				// no append
			case '0', '1', '2', '3', '4', '5', '6', '7':
				x := int(c - '0')
				for i := 0; i < 2; i++ {
					c = b.readByte()
					if c < '0' || c > '7' {
						b.unreadByte()
						break
					}
					x = x*8 + int(c-'0')
				}
				// Per PDF spec, octal codes should be in range 0-377 (0-255)
				// If out of range, mask to byte value
				tmp = append(tmp, byte(x&0xFF))
			default:
				// PDF spec: if the character following the backslash is not recognized,
				// the backslash is ignored and the character is treated literally
				tmp = append(tmp, c)
			}
		}
	}
	b.tmp = tmp
	return string(tmp)
}

func (b *buffer) readName() token {
	tmp := b.tmp[:0]
	for {
		c := b.readByte()
		if isDelim(c) || isSpace(c) {
			b.unreadByte()
			break
		}
		if c == '#' {
			c1 := b.readByte()
			if isDelim(c1) || isSpace(c1) {
				// Malformed: # at end of name or followed by delimiter
				// Treat # as literal character and unread the delimiter
				b.unreadByte()
				tmp = append(tmp, '#')
				continue
			}
			c2 := b.readByte()
			if isDelim(c2) || isSpace(c2) {
				// Malformed: only one hex digit after #
				// Treat as single hex digit followed by 0 (similar to hex string handling)
				b.unreadByte()
				x := unhex(c1)
				if x < 0 {
					// Not a valid hex digit, treat # and c1 as literals
					tmp = append(tmp, '#', c1)
					continue
				}
				tmp = append(tmp, byte(x<<4))
				continue
			}
			x := unhex(c1)<<4 | unhex(c2)
			if x < 0 {
				// Invalid hex digits, treat all as literal characters
				tmp = append(tmp, '#', c1, c2)
				continue
			}
			tmp = append(tmp, byte(x))
			continue
		}
		tmp = append(tmp, c)
	}
	b.tmp = tmp
	return name(string(tmp))
}

// Common PDF keywords - pre-allocated to avoid repeated string allocations
var commonKeywords = map[string]keyword{
	"stream":    "stream",
	"endstream": "endstream",
	"obj":       "obj",
	"endobj":    "endobj",
	"R":         "R",
	"null":      "null",
	"xref":      "xref",
	"trailer":   "trailer",
	"startxref": "startxref",
	"<<":        "<<",
	">>":        ">>",
	"[":         "[",
	"]":         "]",
	"n":         "n",
	"f":         "f",
	"BT":        "BT",
	"ET":        "ET",
	"Tf":        "Tf",
	"Td":        "Td",
	"TD":        "TD",
	"Tm":        "Tm",
	"T*":        "T*",
	"Tj":        "Tj",
	"TJ":        "TJ",
	"'":         "'",
	"\"":        "\"",
	"q":         "q",
	"Q":         "Q",
	"cm":        "cm",
	"Do":        "Do",
	"re":        "re",
	"m":         "m",
	"l":         "l",
	"c":         "c",
	"h":         "h",
	"S":         "S",
	"s":         "s",
	"B":         "B",
	"b":         "b",
	"W":         "W",
	"W*":        "W*",
	"gs":        "gs",
	"CS":        "CS",
	"cs":        "cs",
	"SC":        "SC",
	"sc":        "sc",
	"SCN":       "SCN",
	"scn":       "scn",
	"G":         "G",
	"g":         "g",
	"RG":        "RG",
	"rg":        "rg",
	"K":         "K",
	"k":         "k",
}

func (b *buffer) readKeyword() token {
	tmp := b.tmp[:0]
	for {
		c := b.readByte()
		if isDelim(c) || isSpace(c) {
			b.unreadByte()
			break
		}
		tmp = append(tmp, c)
	}
	b.tmp = tmp

	// Fast path for common cases without string allocation
	if len(tmp) <= 10 {
		// Check for "true" and "false" without allocation
		if len(tmp) == 4 && tmp[0] == 't' && tmp[1] == 'r' && tmp[2] == 'u' && tmp[3] == 'e' {
			return true
		}
		if len(tmp) == 5 && tmp[0] == 'f' && tmp[1] == 'a' && tmp[2] == 'l' && tmp[3] == 's' && tmp[4] == 'e' {
			return false
		}
	}

	// Check if it's a number first (common case) without string allocation
	if len(tmp) > 0 {
		firstChar := tmp[0]
		if (firstChar >= '0' && firstChar <= '9') || firstChar == '+' || firstChar == '-' || firstChar == '.' {
			// Likely a number, check more carefully
			isNum := true
			hasDot := firstChar == '.'
			for i := 1; i < len(tmp); i++ {
				c := tmp[i]
				if c == '.' {
					if hasDot {
						isNum = false
						break
					}
					hasDot = true
				} else if c < '0' || c > '9' {
					isNum = false
					break
				}
			}
			if isNum {
				s := string(tmp)
				if hasDot {
					x, err := strconv.ParseFloat(s, 64)
					if err == nil {
						return x
					}
				} else {
					x, err := strconv.ParseInt(s, 10, 64)
					if err == nil {
						return x
					}
				}
			}
		}
	}

	// For keywords, try to use interned version
	s := string(tmp)
	if kw, ok := commonKeywords[s]; ok {
		return kw
	}
	return keyword(s)
}

func isInteger(s string) bool {
	if len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		s = s[1:]
	}
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || '9' < c {
			return false
		}
	}
	return true
}

func isReal(s string) bool {
	if len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		s = s[1:]
	}
	if len(s) == 0 {
		return false
	}
	ndot := 0
	for _, c := range s {
		if c == '.' {
			ndot++
			continue
		}
		if c < '0' || '9' < c {
			return false
		}
	}
	return ndot == 1
}

// An object is a PDF syntax object, one of the following Go types:
//
//	bool, a PDF boolean
//	int64, a PDF integer
//	float64, a PDF real
//	string, a PDF string literal
//	name, a PDF name without the leading slash
//	dict, a PDF dictionary
//	array, a PDF array
//	stream, a PDF stream
//	objptr, a PDF object reference
//	objdef, a PDF object definition
//
// An object may also be nil, to represent the PDF null.
type object interface{}

type dict map[name]object

type array []object

type stream struct {
	hdr    dict
	ptr    objptr
	offset int64
}

type objptr struct {
	id  uint32
	gen uint16
}

type objdef struct {
	ptr objptr
	obj object
}

// Hard limit to prevent runaway array allocations on malformed content streams.
// Real-world PDFs keep operator argument arrays small (kerning, matrices, etc.),
// so 100k elements is a generous cap while avoiding multi-GB slices.
const maxArrayElements = 100_000

func (b *buffer) readObject() object {
	tok := b.readToken()
	if kw, ok := tok.(keyword); ok {
		switch kw {
		case "null":
			return nil
		case "<<":
			return b.readDict()
		case "[":
			return b.readArray()
		case ">>":
			// stop the object
			return nil
		case "endobj", "endstream", "stream":
			// Tolerate these keywords appearing unexpectedly in corrupted PDFs
			return nil
		}
		// Return the keyword itself for other unexpected keywords
		// This allows the caller to handle it appropriately
		return nil
	}

	// Only decrypt strings if we have a valid encryption key (non-empty)
	// Note: b.key may be non-nil but empty from buffer pool, so we must check len(b.key) > 0
	if str, ok := tok.(string); ok && len(b.key) > 0 && b.objptr.id != 0 {
		tok = decryptString(b.key, b.useAES, b.objptr, str)
	}

	if !b.allowObjptr {
		return tok
	}

	if t1, ok := tok.(int64); ok && int64(uint32(t1)) == t1 {
		tok2 := b.readToken()
		if t2, ok := tok2.(int64); ok && int64(uint16(t2)) == t2 {
			tok3 := b.readToken()
			switch tok3 {
			case keyword("R"):
				return objptr{uint32(t1), uint16(t2)}
			case keyword("obj"):
				old := b.objptr
				b.objptr = objptr{uint32(t1), uint16(t2)}
				obj := b.readObject()
				if _, ok := obj.(stream); !ok {
					tok4 := b.readToken()
					if tok4 != keyword("endobj") {
						// Tolerate missing endobj - common in corrupted PDFs
						// Just unread the token and continue
						if tok4 != nil && tok4 != io.EOF {
							b.unreadToken(tok4)
						}
					}
				}
				b.objptr = old
				return objdef{objptr{uint32(t1), uint16(t2)}, obj}
			}
			b.unreadToken(tok3)
		}
		b.unreadToken(tok2)
	}
	return tok
}

func (b *buffer) readArray() object {
	var x array
	for {
		tok := b.readToken()
		if tok == nil || tok == keyword("]") {
			break
		}
		if tok == io.EOF {
			// Tolerate unterminated array, return what we have
			break
		}
		if len(x) >= maxArrayElements {
			// Array too large, stop parsing and return what we have
			break
		}
		b.unreadToken(tok)
		x = append(x, b.readObject())
	}
	return x
}

func (b *buffer) readDict() object {
	x := make(dict)
	for {
		tok := b.readToken()
		if tok == nil || tok == keyword(">>") {
			break
		}
		if tok == io.EOF {
			break
		}
		n, ok := tok.(name)
		if !ok {
			// When encountering non-name key, possibly corrupted or missing ">>"/"stream", fall back and end current dictionary to avoid panic
			b.unreadToken(tok)
			break
		}
		x[n] = b.readObject()
	}

	if !b.allowStream {
		return x
	}

	tok := b.readToken()
	if tok != keyword("stream") {
		b.unreadToken(tok)
		return x
	}

	switch b.readByte() {
	case '\r':
		if b.readByte() != '\n' {
			b.unreadByte()
		}
	case '\n':
		// ok
	default:
		// Some corrupted PDFs lack newline after stream, tolerate and fall back one byte to treat it as data start
		b.unreadByte()
	}

	return stream{x, b.objptr, b.readOffset()}
}

func isSpace(b byte) bool {
	switch b {
	case '\x00', '\t', '\n', '\f', '\r', ' ':
		return true
	}
	return false
}

func isDelim(b byte) bool {
	switch b {
	case '<', '>', '(', ')', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}
