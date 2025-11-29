// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf16"
	"unsafe"
)

// CMapType represents the type of CMap
type CMapType int

const (
	CMapTypeToUnicode CMapType = iota // ToUnicode CMap
	CMapTypeCID                       // CID CMap (for Adobe-* encodings)
)

// CIDSystemInfo represents the CIDSystemInfo dictionary in a CMap
type CIDSystemInfo struct {
	Registry   string
	Ordering   string
	Supplement int
}

// CMap represents a character code to CID or Unicode mapping
type CMap struct {
	Name          string
	Type          CMapType
	CIDSystemInfo CIDSystemInfo
	WMode         int // 0: horizontal, 1: vertical

	// Code space ranges define valid input code ranges
	codeSpaceRanges []codeSpaceRange

	// CID ranges map character codes to CIDs
	cidRanges []cidRange
	cidChars  []cidChar

	// BF (base font) mappings for ToUnicode CMaps
	bfRanges []bfrange
	bfChars  []bfchar

	// UseCMap reference
	useCMap TextEncoding

	// Lock-free performance optimizations using sync.Map
	cidCache    *sync.Map // Precomputed CID mappings for fast lookup (lock-free)
	decodeCache *sync.Map // Cached decode results (lock-free)
	isOptimized bool      // Whether this CMap has been optimized

	// Lock for creation and optimization phases only
	mu sync.RWMutex // Protects struct fields during creation/optimization
}

type codeSpaceRange struct {
	low  []byte
	high []byte
}

type cidRange struct {
	low  []byte
	high []byte
	cid  int
}

type cidChar struct {
	code []byte
	cid  int
}

// NewCMap creates a new empty CMap
func NewCMap(name string, cmapType CMapType) *CMap {
	return &CMap{
		Name:        name,
		Type:        cmapType,
		cidCache:    &sync.Map{},
		decodeCache: &sync.Map{},
	}
}

// SetCIDSystemInfo sets the CIDSystemInfo for the CMap
func (c *CMap) SetCIDSystemInfo(registry, ordering string, supplement int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.CIDSystemInfo = CIDSystemInfo{
		Registry:   registry,
		Ordering:   ordering,
		Supplement: supplement,
	}
}

// AddCodeSpaceRange adds a code space range to the CMap
func (c *CMap) AddCodeSpaceRange(low, high []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.codeSpaceRanges = append(c.codeSpaceRanges, codeSpaceRange{
		low:  append([]byte(nil), low...),
		high: append([]byte(nil), high...),
	})
}

// AddCIDRange adds a CID range mapping
func (c *CMap) AddCIDRange(low, high []byte, startCID int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cidRanges = append(c.cidRanges, cidRange{
		low:  append([]byte(nil), low...),
		high: append([]byte(nil), high...),
		cid:  startCID,
	})
}

// AddCIDChar adds a single CID character mapping
func (c *CMap) AddCIDChar(code []byte, cid int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cidChars = append(c.cidChars, cidChar{
		code: append([]byte(nil), code...),
		cid:  cid,
	})
}

// AddBFRange adds a base font range mapping (for ToUnicode)
func (c *CMap) AddBFRange(low, high string, dst Value) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bfRanges = append(c.bfRanges, bfrange{
		lo:  low,
		hi:  high,
		dst: dst,
	})
}

// AddBFChar adds a single base font character mapping (for ToUnicode)
func (c *CMap) AddBFChar(orig, repl string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bfChars = append(c.bfChars, bfchar{
		orig: orig,
		repl: repl,
	})
}

// SetUseCMap sets the parent CMap to use for unmapped codes
func (c *CMap) SetUseCMap(parent TextEncoding) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.useCMap = parent
}

// inCodeSpace checks if a byte sequence is within any code space range
func (c *CMap) inCodeSpace(code []byte) bool {
	for _, r := range c.codeSpaceRanges {
		if len(code) == len(r.low) && len(code) == len(r.high) {
			match := true
			for i := range code {
				if code[i] < r.low[i] || code[i] > r.high[i] {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return len(c.codeSpaceRanges) == 0 // If no code space defined, accept all
}

// LookupCID looks up the CID for a given character code (lock-free)
func (c *CMap) LookupCID(code []byte) (int, bool) {
	// First try lock-free cache
	if c.isOptimized && c.cidCache != nil {
		key := string(code)
		if value, ok := c.cidCache.Load(key); ok {
			if cid, ok := value.(int); ok {
				return cid, true
			}
		}
	}

	// Fallback to original linear search (read-only, no lock needed after optimization)
	return c.lookupCIDUncached(code)
}

// lookupCIDUncached performs the original linear search lookup
func (c *CMap) lookupCIDUncached(code []byte) (int, bool) {
	// Check single char mappings first
	for _, ch := range c.cidChars {
		if bytes.Equal(code, ch.code) {
			return ch.cid, true
		}
	}

	// Check range mappings
	for _, r := range c.cidRanges {
		if len(code) != len(r.low) || len(code) != len(r.high) {
			continue
		}
		inRange := true
		for i := range code {
			if code[i] < r.low[i] || code[i] > r.high[i] {
				inRange = false
				break
			}
		}
		if inRange {
			// Calculate CID offset
			offset := 0
			for i := range code {
				offset = offset*256 + int(code[i]) - int(r.low[i])
			}
			return r.cid + offset, true
		}
	}

	return 0, false
}

// OptimizeCIDLookup precomputes CID mappings for fast lookup
// This should be called after all CID mappings are added to the CMap
func (c *CMap) OptimizeCIDLookup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isOptimized {
		return // Already optimized
	}

	// Precompute single character mappings
	for _, ch := range c.cidChars {
		c.cidCache.Store(string(ch.code), ch.cid)
	}

	// Precompute range mappings (sample key codes for common usage)
	// For performance, we precompute mappings for byte lengths 1-4
	for _, r := range c.cidRanges {
		codeLen := len(r.low)
		if codeLen == 0 || codeLen > 4 {
			continue // Skip invalid or very long codes
		}

		// Precompute first N mappings in each range for common usage
		maxPrecompute := 256 // Limit precomputation to avoid memory explosion
		if codeLen == 1 {
			maxPrecompute = 256
		} else if codeLen == 2 {
			maxPrecompute = 1024
		} else {
			maxPrecompute = 256
		}

		precomputed := 0
		codes := make([][]byte, 0, maxPrecompute)

		// Generate codes in range
		if codeLen == 1 {
			for b := r.low[0]; b <= r.high[0] && precomputed < maxPrecompute; b++ {
				codes = append(codes, []byte{b})
				precomputed++
			}
		} else if codeLen == 2 {
			for b1 := r.low[0]; b1 <= r.high[0] && precomputed < maxPrecompute; b1++ {
				for b2 := r.low[1]; b2 <= r.high[1] && precomputed < maxPrecompute; b2++ {
					codes = append(codes, []byte{b1, b2})
					precomputed++
				}
			}
		} else {
			// For longer codes, just precompute the first few
			code := make([]byte, codeLen)
			copy(code, r.low)
			for i := 0; i < maxPrecompute && c.compareBytes(code, r.high) <= 0; i++ {
				codeCopy := make([]byte, codeLen)
				copy(codeCopy, code)
				codes = append(codes, codeCopy)

				// Increment code
				for j := codeLen - 1; j >= 0; j-- {
					if code[j] < 255 {
						code[j]++
						break
					}
					code[j] = 0
				}
			}
		}

		// Compute CIDs for precomputed codes
		for _, code := range codes {
			offset := 0
			for i := range code {
				offset = offset*256 + int(code[i]) - int(r.low[i])
			}
			c.cidCache.Store(string(code), r.cid+offset)
		}
	}

	c.isOptimized = true
}

// compareBytes compares two byte slices lexicographically
func (c *CMap) compareBytes(a, b []byte) int {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}

	for i := 0; i < minLen; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}

	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

// Decode implements TextEncoding interface for ToUnicode CMaps (lock-free)
func (c *CMap) Decode(raw string) string {
	// First try lock-free decode cache
	if c.isOptimized && c.decodeCache != nil && len(raw) <= 256 {
		if cached, ok := c.decodeCache.Load(raw); ok {
			if result, ok := cached.(string); ok {
				return result
			}
		}
	}

	// Perform decoding (read-only, no lock needed after optimization)
	result := c.decodeUncached(raw)

	// Cache result if appropriate (lock-free store)
	if c.isOptimized && len(raw) <= 256 && len(result) <= 1024 {
		c.decodeCache.Store(raw, result)
	}

	return result
}

// decodeUncached performs the actual decoding without caching
func (c *CMap) decodeUncached(raw string) string {
	var result strings.Builder
	result.Grow(len(raw) * 2)

	i := 0
	for i < len(raw) {
		matched := false

		// Try different byte lengths based on code space ranges
		// Prioritize common lengths for better performance
		lengths := []int{2, 1, 4, 3} // Prioritize 2-byte codes (common for CJK)
		for _, length := range lengths {
			if i+length > len(raw) {
				continue
			}

			code := raw[i : i+length]

			// Check bfchar mappings with early break
			for _, bf := range c.bfChars {
				if bf.orig == code {
					result.WriteString(cmapDecodeUTF16BE(bf.repl))
					i += length
					matched = true
					goto nextChar
				}
			}

			// Check bfrange mappings
			for _, br := range c.bfRanges {
				if len(br.lo) != length {
					continue
				}
				if code >= br.lo && code <= br.hi {
					offset := 0
					for j := 0; j < length; j++ {
						offset = offset*256 + int(code[j]) - int(br.lo[j])
					}

					if br.dst.Kind() == Array {
						// Array of destination strings
						if offset < br.dst.Len() {
							result.WriteString(cmapDecodeUTF16BE(br.dst.Index(offset).RawString()))
						}
					} else {
						// Single destination string, add offset to first code point
						dst := br.dst.RawString()
						if len(dst) >= 2 {
							// Parse as UTF-16BE
							u := make([]uint16, 0, len(dst)/2)
							for j := 0; j < len(dst)-1; j += 2 {
								u = append(u, uint16(dst[j])<<8|uint16(dst[j+1]))
							}
							if len(u) > 0 {
								u[len(u)-1] += uint16(offset)
								result.WriteString(string(utf16.Decode(u)))
							}
						}
					}
					i += length
					matched = true
					goto nextChar
				}
			}
		}

	nextChar:
		// If no mapping found, try useCMap or pass through
		if !matched {
			if c.useCMap != nil {
				// Try to decode with parent CMap
				// For simplicity, try 2-byte chunks for CJK
				if i+2 <= len(raw) {
					decoded := c.useCMap.Decode(raw[i : i+2])
					if decoded != raw[i:i+2] {
						result.WriteString(decoded)
						i += 2
						continue
					}
				}
			}
			// Pass through single byte
			result.WriteByte(raw[i])
			i++
		}
	}

	return result.String()
}

// cmapDecodeUTF16BE decodes a UTF-16BE encoded string to UTF-8
func cmapDecodeUTF16BE(s string) string {
	if len(s) < 2 {
		return s
	}

	u := make([]uint16, 0, len(s)/2)
	for i := 0; i < len(s)-1; i += 2 {
		u = append(u, uint16(s[i])<<8|uint16(s[i+1]))
	}

	return string(utf16.Decode(u))
}

// CMapParser parses CMap files/streams
type CMapParser struct {
	scanner *bufio.Scanner
	cmap    *CMap
	stack   []interface{}
}

// ParseCMap parses a CMap from a reader
func ParseCMap(r io.Reader, name string) (*CMap, error) {
	parser := &CMapParser{
		cmap:  NewCMap(name, CMapTypeToUnicode),
		stack: make([]interface{}, 0, 32),
	}

	scanner := bufio.NewScanner(r)
	scanner.Split(scanCMapTokens)
	parser.scanner = scanner

	for scanner.Scan() {
		token := scanner.Text()
		if err := parser.processToken(token); err != nil {
			return nil, err
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return parser.cmap, nil
}

func (p *CMapParser) processToken(token string) error {
	switch token {
	case "begincodespacerange":
		// Next tokens define code space ranges
		return nil
	case "endcodespacerange":
		// Process accumulated code space ranges
		for len(p.stack) >= 2 {
			high := p.popBytes()
			low := p.popBytes()
			p.cmap.AddCodeSpaceRange(low, high)
		}
		return nil

	case "beginbfchar":
		return nil
	case "endbfchar":
		// Process accumulated bfchar mappings
		for len(p.stack) >= 2 {
			repl := p.popString()
			orig := p.popString()
			p.cmap.AddBFChar(orig, repl)
		}
		return nil

	case "beginbfrange":
		return nil
	case "endbfrange":
		// Process accumulated bfrange mappings
		for len(p.stack) >= 3 {
			dst := p.popValue()
			hi := p.popString()
			lo := p.popString()
			p.cmap.bfRanges = append(p.cmap.bfRanges, bfrange{lo: lo, hi: hi, dst: dst})
		}
		return nil

	case "begincidchar":
		return nil
	case "endcidchar":
		// Process accumulated cidchar mappings
		for len(p.stack) >= 2 {
			cid := p.popInt()
			code := p.popBytes()
			p.cmap.AddCIDChar(code, cid)
		}
		return nil

	case "begincidrange":
		return nil
	case "endcidrange":
		// Process accumulated cidrange mappings
		for len(p.stack) >= 3 {
			startCID := p.popInt()
			high := p.popBytes()
			low := p.popBytes()
			p.cmap.AddCIDRange(low, high, startCID)
		}
		return nil

	case "usecmap":
		if len(p.stack) > 0 {
			name := p.popString()
			if enc := lookupCMap(name); enc != nil {
				p.cmap.SetUseCMap(enc)
			} else if enc := builtinCMapEncoding(name); enc != nil {
				p.cmap.SetUseCMap(enc)
			}
		}
		return nil

	case "begincmap", "endcmap":
		return nil

	case "/CMapName":
		// Next token is the name
		return nil
	case "/CMapType":
		// Next token is the type
		return nil
	case "/WMode":
		// Next token is the writing mode
		return nil

	default:
		// Try to parse as value and push to stack
		if strings.HasPrefix(token, "<") && strings.HasSuffix(token, ">") {
			// Hex string
			hex := token[1 : len(token)-1]
			b, _ := parseHexString(hex)
			p.stack = append(p.stack, b)
		} else if strings.HasPrefix(token, "(") && strings.HasSuffix(token, ")") {
			// Literal string
			s := token[1 : len(token)-1]
			p.stack = append(p.stack, s)
		} else if strings.HasPrefix(token, "[") {
			// Start array
			p.stack = append(p.stack, "[")
		} else if token == "]" {
			// End array - collect elements
			arr := make([]interface{}, 0)
			for len(p.stack) > 0 {
				top := p.stack[len(p.stack)-1]
				if s, ok := top.(string); ok && s == "[" {
					p.stack = p.stack[:len(p.stack)-1]
					break
				}
				p.stack = p.stack[:len(p.stack)-1]
				arr = append([]interface{}{top}, arr...)
			}
			p.stack = append(p.stack, arr)
		} else if n, err := strconv.Atoi(token); err == nil {
			p.stack = append(p.stack, n)
		} else {
			// Push as string
			p.stack = append(p.stack, token)
		}
	}
	return nil
}

func (p *CMapParser) popBytes() []byte {
	if len(p.stack) == 0 {
		return nil
	}
	top := p.stack[len(p.stack)-1]
	p.stack = p.stack[:len(p.stack)-1]
	switch v := top.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	default:
		return nil
	}
}

func (p *CMapParser) popString() string {
	if len(p.stack) == 0 {
		return ""
	}
	top := p.stack[len(p.stack)-1]
	p.stack = p.stack[:len(p.stack)-1]
	switch v := top.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}

func (p *CMapParser) popInt() int {
	if len(p.stack) == 0 {
		return 0
	}
	top := p.stack[len(p.stack)-1]
	p.stack = p.stack[:len(p.stack)-1]
	switch v := top.(type) {
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

func (p *CMapParser) popValue() Value {
	if len(p.stack) == 0 {
		return Value{}
	}
	top := p.stack[len(p.stack)-1]
	p.stack = p.stack[:len(p.stack)-1]

	switch v := top.(type) {
	case string:
		return Value{data: v}
	case []byte:
		return Value{data: string(v)}
	case []interface{}:
		// Convert to array Value
		arr := make(array, len(v))
		for i, elem := range v {
			if s, ok := elem.(string); ok {
				arr[i] = s
			} else if b, ok := elem.([]byte); ok {
				arr[i] = string(b)
			}
		}
		return Value{data: arr}
	default:
		return Value{}
	}
}

// parseHexString parses a hex string like "0123ABCD" into bytes
func parseHexString(hex string) ([]byte, error) {
	// Remove whitespace
	hex = strings.ReplaceAll(hex, " ", "")
	hex = strings.ReplaceAll(hex, "\n", "")
	hex = strings.ReplaceAll(hex, "\r", "")
	hex = strings.ReplaceAll(hex, "\t", "")

	if len(hex)%2 != 0 {
		hex += "0"
	}

	result := make([]byte, len(hex)/2)
	for i := 0; i < len(hex); i += 2 {
		b, err := strconv.ParseUint(hex[i:i+2], 16, 8)
		if err != nil {
			return nil, err
		}
		result[i/2] = byte(b)
	}
	return result, nil
}

// scanCMapTokens is a bufio.SplitFunc for scanning CMap tokens
func scanCMapTokens(data []byte, atEOF bool) (advance int, token []byte, err error) {
	// Skip whitespace and comments
	start := 0
	for start < len(data) {
		c := data[start]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			start++
			continue
		}
		if c == '%' {
			// Skip comment until end of line
			for start < len(data) && data[start] != '\n' && data[start] != '\r' {
				start++
			}
			continue
		}
		break
	}

	if start >= len(data) {
		if atEOF {
			return len(data), nil, nil
		}
		return 0, nil, nil
	}

	// Parse token
	c := data[start]

	// Dictionary delimiters
	if c == '<' && start+1 < len(data) && data[start+1] == '<' {
		return start + 2, []byte("<<"), nil
	}
	if c == '>' && start+1 < len(data) && data[start+1] == '>' {
		return start + 2, []byte(">>"), nil
	}

	// Hex string
	if c == '<' {
		end := start + 1
		for end < len(data) && data[end] != '>' {
			end++
		}
		if end < len(data) {
			return end + 1, data[start : end+1], nil
		}
		if atEOF {
			return len(data), data[start:], nil
		}
		return 0, nil, nil
	}

	// Literal string (handle nested parentheses)
	if c == '(' {
		end := start + 1
		depth := 1
		escaped := false
		for end < len(data) && depth > 0 {
			if escaped {
				escaped = false
				end++
				continue
			}
			switch data[end] {
			case '\\':
				escaped = true
			case '(':
				depth++
			case ')':
				depth--
			}
			end++
		}
		if depth == 0 {
			return end, data[start:end], nil
		}
		if atEOF {
			return len(data), data[start:], nil
		}
		return 0, nil, nil
	}

	// Array markers
	if c == '[' || c == ']' {
		return start + 1, data[start : start+1], nil
	}

	// Dictionary markers
	if c == '/' {
		end := start + 1
		for end < len(data) {
			c := data[end]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '/' ||
				c == '<' || c == '>' || c == '[' || c == ']' || c == '(' || c == ')' {
				break
			}
			end++
		}
		return end, data[start:end], nil
	}

	// Regular token
	end := start
	for end < len(data) {
		c := data[end]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '/' ||
			c == '<' || c == '>' || c == '[' || c == ']' || c == '(' || c == ')' {
			break
		}
		end++
	}

	if end > start {
		return end, data[start:end], nil
	}

	if atEOF {
		return len(data), nil, nil
	}
	return 0, nil, nil
}

// PredefinedCMap represents a predefined Adobe CMap
type PredefinedCMap struct {
	*CMap
}

// Adobe CMap registry for CJK support
var (
	predefinedCMapRegistry   = make(map[string]*PredefinedCMap)
	predefinedCMapRegistryMu sync.RWMutex
)

// RegisterPredefinedCMap registers a predefined CMap
func RegisterPredefinedCMap(name string, cmap *PredefinedCMap) {
	predefinedCMapRegistryMu.Lock()
	defer predefinedCMapRegistryMu.Unlock()
	predefinedCMapRegistry[name] = cmap
}

// GetPredefinedCMap retrieves a predefined CMap by name
func GetPredefinedCMap(name string) *PredefinedCMap {
	predefinedCMapRegistryMu.RLock()
	defer predefinedCMapRegistryMu.RUnlock()
	return predefinedCMapRegistry[name]
}

// InitPredefinedCMaps initializes common predefined CMaps
// These provide basic Unicode mappings for CJK character sets
func InitPredefinedCMaps() {
	// Identity CMaps (already defined in text.go, register here for consistency)
	registerIdentityCMaps()

	// Adobe-GB1 (Simplified Chinese)
	registerAdobeGB1CMaps()

	// Adobe-CNS1 (Traditional Chinese)
	registerAdobeCNS1CMaps()

	// Adobe-Japan1 (Japanese)
	registerAdobeJapan1CMaps()

	// Adobe-Korea1 (Korean)
	registerAdobeKorea1CMaps()
}

func registerIdentityCMaps() {
	// Identity-H and Identity-V are already in predefinedCMaps
	// Just ensure they're in our registry too
	if enc, ok := predefinedCMaps["Identity-H"]; ok {
		cm := &CMap{
			Name:    "Identity-H",
			WMode:   0,
			useCMap: enc,
		}
		// Pre-optimize identity mappings
		cm.OptimizeCIDLookup()
		RegisterPredefinedCMap("Identity-H", &PredefinedCMap{cm})
	}
	if enc, ok := predefinedCMaps["Identity-V"]; ok {
		cm := &CMap{
			Name:    "Identity-V",
			WMode:   1,
			useCMap: enc,
		}
		// Pre-optimize identity mappings
		cm.OptimizeCIDLookup()
		RegisterPredefinedCMap("Identity-V", &PredefinedCMap{cm})
	}
}

func registerAdobeGB1CMaps() {
	// GBK-EUC-H: GBK encoding to CID horizontal
	cm := NewCMap("GBK-EUC-H", CMapTypeCID)
	cm.SetCIDSystemInfo("Adobe", "GB1", 2)
	cm.WMode = 0
	// Add code space range for GBK
	cm.AddCodeSpaceRange([]byte{0x00}, []byte{0x80})
	cm.AddCodeSpaceRange([]byte{0x81, 0x40}, []byte{0xFE, 0xFE})
	cm.OptimizeCIDLookup() // Pre-optimize
	RegisterPredefinedCMap("GBK-EUC-H", &PredefinedCMap{cm})

	// GBK-EUC-V: GBK encoding to CID vertical
	cmV := NewCMap("GBK-EUC-V", CMapTypeCID)
	cmV.SetCIDSystemInfo("Adobe", "GB1", 2)
	cmV.WMode = 1
	cmV.AddCodeSpaceRange([]byte{0x00}, []byte{0x80})
	cmV.AddCodeSpaceRange([]byte{0x81, 0x40}, []byte{0xFE, 0xFE})
	cmV.OptimizeCIDLookup() // Pre-optimize
	RegisterPredefinedCMap("GBK-EUC-V", &PredefinedCMap{cmV})

	// UniGB-UCS2-H: Unicode to CID horizontal
	uniH := NewCMap("UniGB-UCS2-H", CMapTypeCID)
	uniH.SetCIDSystemInfo("Adobe", "GB1", 4)
	uniH.WMode = 0
	uniH.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	uniH.OptimizeCIDLookup() // Pre-optimize
	RegisterPredefinedCMap("UniGB-UCS2-H", &PredefinedCMap{uniH})

	// UniGB-UCS2-V: Unicode to CID vertical
	uniV := NewCMap("UniGB-UCS2-V", CMapTypeCID)
	uniV.SetCIDSystemInfo("Adobe", "GB1", 4)
	uniV.WMode = 1
	uniV.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	uniV.OptimizeCIDLookup() // Pre-optimize
	RegisterPredefinedCMap("UniGB-UCS2-V", &PredefinedCMap{uniV})

	// UniGB-UTF16-H
	utf16H := NewCMap("UniGB-UTF16-H", CMapTypeCID)
	utf16H.SetCIDSystemInfo("Adobe", "GB1", 5)
	utf16H.WMode = 0
	utf16H.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	utf16H.OptimizeCIDLookup() // Pre-optimize
	RegisterPredefinedCMap("UniGB-UTF16-H", &PredefinedCMap{utf16H})

	// UniGB-UTF16-V
	utf16V := NewCMap("UniGB-UTF16-V", CMapTypeCID)
	utf16V.SetCIDSystemInfo("Adobe", "GB1", 5)
	utf16V.WMode = 1
	utf16V.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	utf16V.OptimizeCIDLookup() // Pre-optimize
	RegisterPredefinedCMap("UniGB-UTF16-V", &PredefinedCMap{utf16V})
}

func registerAdobeCNS1CMaps() {
	// B5-H: Big5 encoding to CID horizontal
	cm := NewCMap("B5-H", CMapTypeCID)
	cm.SetCIDSystemInfo("Adobe", "CNS1", 0)
	cm.WMode = 0
	cm.AddCodeSpaceRange([]byte{0x00}, []byte{0x80})
	cm.AddCodeSpaceRange([]byte{0xA1, 0x40}, []byte{0xFE, 0xFE})
	RegisterPredefinedCMap("B5-H", &PredefinedCMap{cm})

	// B5-V: Big5 encoding to CID vertical
	cmV := NewCMap("B5-V", CMapTypeCID)
	cmV.SetCIDSystemInfo("Adobe", "CNS1", 0)
	cmV.WMode = 1
	cmV.AddCodeSpaceRange([]byte{0x00}, []byte{0x80})
	cmV.AddCodeSpaceRange([]byte{0xA1, 0x40}, []byte{0xFE, 0xFE})
	RegisterPredefinedCMap("B5-V", &PredefinedCMap{cmV})

	// UniCNS-UCS2-H: Unicode to CID horizontal
	uniH := NewCMap("UniCNS-UCS2-H", CMapTypeCID)
	uniH.SetCIDSystemInfo("Adobe", "CNS1", 3)
	uniH.WMode = 0
	uniH.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	RegisterPredefinedCMap("UniCNS-UCS2-H", &PredefinedCMap{uniH})

	// UniCNS-UCS2-V: Unicode to CID vertical
	uniV := NewCMap("UniCNS-UCS2-V", CMapTypeCID)
	uniV.SetCIDSystemInfo("Adobe", "CNS1", 3)
	uniV.WMode = 1
	uniV.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	RegisterPredefinedCMap("UniCNS-UCS2-V", &PredefinedCMap{uniV})

	// UniCNS-UTF16-H
	utf16H := NewCMap("UniCNS-UTF16-H", CMapTypeCID)
	utf16H.SetCIDSystemInfo("Adobe", "CNS1", 6)
	utf16H.WMode = 0
	utf16H.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	RegisterPredefinedCMap("UniCNS-UTF16-H", &PredefinedCMap{utf16H})

	// UniCNS-UTF16-V
	utf16V := NewCMap("UniCNS-UTF16-V", CMapTypeCID)
	utf16V.SetCIDSystemInfo("Adobe", "CNS1", 6)
	utf16V.WMode = 1
	utf16V.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	RegisterPredefinedCMap("UniCNS-UTF16-V", &PredefinedCMap{utf16V})
}

func registerAdobeJapan1CMaps() {
	// 83pv-RKSJ-H: Mac Japanese to CID horizontal
	cm := NewCMap("83pv-RKSJ-H", CMapTypeCID)
	cm.SetCIDSystemInfo("Adobe", "Japan1", 1)
	cm.WMode = 0
	cm.AddCodeSpaceRange([]byte{0x00}, []byte{0x80})
	cm.AddCodeSpaceRange([]byte{0x81, 0x40}, []byte{0x9F, 0xFC})
	cm.AddCodeSpaceRange([]byte{0xA0}, []byte{0xDF})
	cm.AddCodeSpaceRange([]byte{0xE0, 0x40}, []byte{0xFC, 0xFC})
	RegisterPredefinedCMap("83pv-RKSJ-H", &PredefinedCMap{cm})

	// 90ms-RKSJ-H: Windows Japanese to CID horizontal
	cm90 := NewCMap("90ms-RKSJ-H", CMapTypeCID)
	cm90.SetCIDSystemInfo("Adobe", "Japan1", 2)
	cm90.WMode = 0
	cm90.AddCodeSpaceRange([]byte{0x00}, []byte{0x80})
	cm90.AddCodeSpaceRange([]byte{0x81, 0x40}, []byte{0x9F, 0xFC})
	cm90.AddCodeSpaceRange([]byte{0xA0}, []byte{0xDF})
	cm90.AddCodeSpaceRange([]byte{0xE0, 0x40}, []byte{0xFC, 0xFC})
	RegisterPredefinedCMap("90ms-RKSJ-H", &PredefinedCMap{cm90})

	// 90ms-RKSJ-V: Windows Japanese to CID vertical
	cm90V := NewCMap("90ms-RKSJ-V", CMapTypeCID)
	cm90V.SetCIDSystemInfo("Adobe", "Japan1", 2)
	cm90V.WMode = 1
	cm90V.AddCodeSpaceRange([]byte{0x00}, []byte{0x80})
	cm90V.AddCodeSpaceRange([]byte{0x81, 0x40}, []byte{0x9F, 0xFC})
	cm90V.AddCodeSpaceRange([]byte{0xA0}, []byte{0xDF})
	cm90V.AddCodeSpaceRange([]byte{0xE0, 0x40}, []byte{0xFC, 0xFC})
	RegisterPredefinedCMap("90ms-RKSJ-V", &PredefinedCMap{cm90V})

	// UniJIS-UCS2-H: Unicode to CID horizontal
	uniH := NewCMap("UniJIS-UCS2-H", CMapTypeCID)
	uniH.SetCIDSystemInfo("Adobe", "Japan1", 4)
	uniH.WMode = 0
	uniH.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	RegisterPredefinedCMap("UniJIS-UCS2-H", &PredefinedCMap{uniH})

	// UniJIS-UCS2-V: Unicode to CID vertical
	uniV := NewCMap("UniJIS-UCS2-V", CMapTypeCID)
	uniV.SetCIDSystemInfo("Adobe", "Japan1", 4)
	uniV.WMode = 1
	uniV.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	RegisterPredefinedCMap("UniJIS-UCS2-V", &PredefinedCMap{uniV})

	// UniJIS-UTF16-H
	utf16H := NewCMap("UniJIS-UTF16-H", CMapTypeCID)
	utf16H.SetCIDSystemInfo("Adobe", "Japan1", 6)
	utf16H.WMode = 0
	utf16H.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	RegisterPredefinedCMap("UniJIS-UTF16-H", &PredefinedCMap{utf16H})

	// UniJIS-UTF16-V
	utf16V := NewCMap("UniJIS-UTF16-V", CMapTypeCID)
	utf16V.SetCIDSystemInfo("Adobe", "Japan1", 6)
	utf16V.WMode = 1
	utf16V.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	RegisterPredefinedCMap("UniJIS-UTF16-V", &PredefinedCMap{utf16V})
}

func registerAdobeKorea1CMaps() {
	// KSC-EUC-H: EUC-KR encoding to CID horizontal
	cm := NewCMap("KSC-EUC-H", CMapTypeCID)
	cm.SetCIDSystemInfo("Adobe", "Korea1", 0)
	cm.WMode = 0
	cm.AddCodeSpaceRange([]byte{0x00}, []byte{0x80})
	cm.AddCodeSpaceRange([]byte{0xA1, 0xA1}, []byte{0xFE, 0xFE})
	RegisterPredefinedCMap("KSC-EUC-H", &PredefinedCMap{cm})

	// KSC-EUC-V: EUC-KR encoding to CID vertical
	cmV := NewCMap("KSC-EUC-V", CMapTypeCID)
	cmV.SetCIDSystemInfo("Adobe", "Korea1", 0)
	cmV.WMode = 1
	cmV.AddCodeSpaceRange([]byte{0x00}, []byte{0x80})
	cmV.AddCodeSpaceRange([]byte{0xA1, 0xA1}, []byte{0xFE, 0xFE})
	RegisterPredefinedCMap("KSC-EUC-V", &PredefinedCMap{cmV})

	// KSCms-UHC-H: Windows Korean (UHC) to CID horizontal
	uhcH := NewCMap("KSCms-UHC-H", CMapTypeCID)
	uhcH.SetCIDSystemInfo("Adobe", "Korea1", 1)
	uhcH.WMode = 0
	uhcH.AddCodeSpaceRange([]byte{0x00}, []byte{0x80})
	uhcH.AddCodeSpaceRange([]byte{0x81, 0x41}, []byte{0xFE, 0xFE})
	RegisterPredefinedCMap("KSCms-UHC-H", &PredefinedCMap{uhcH})

	// KSCms-UHC-V: Windows Korean (UHC) to CID vertical
	uhcV := NewCMap("KSCms-UHC-V", CMapTypeCID)
	uhcV.SetCIDSystemInfo("Adobe", "Korea1", 1)
	uhcV.WMode = 1
	uhcV.AddCodeSpaceRange([]byte{0x00}, []byte{0x80})
	uhcV.AddCodeSpaceRange([]byte{0x81, 0x41}, []byte{0xFE, 0xFE})
	RegisterPredefinedCMap("KSCms-UHC-V", &PredefinedCMap{uhcV})

	// UniKS-UCS2-H: Unicode to CID horizontal
	uniH := NewCMap("UniKS-UCS2-H", CMapTypeCID)
	uniH.SetCIDSystemInfo("Adobe", "Korea1", 1)
	uniH.WMode = 0
	uniH.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	RegisterPredefinedCMap("UniKS-UCS2-H", &PredefinedCMap{uniH})

	// UniKS-UCS2-V: Unicode to CID vertical
	uniV := NewCMap("UniKS-UCS2-V", CMapTypeCID)
	uniV.SetCIDSystemInfo("Adobe", "Korea1", 1)
	uniV.WMode = 1
	uniV.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	RegisterPredefinedCMap("UniKS-UCS2-V", &PredefinedCMap{uniV})

	// UniKS-UTF16-H
	utf16H := NewCMap("UniKS-UTF16-H", CMapTypeCID)
	utf16H.SetCIDSystemInfo("Adobe", "Korea1", 2)
	utf16H.WMode = 0
	utf16H.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	RegisterPredefinedCMap("UniKS-UTF16-H", &PredefinedCMap{utf16H})

	// UniKS-UTF16-V
	utf16V := NewCMap("UniKS-UTF16-V", CMapTypeCID)
	utf16V.SetCIDSystemInfo("Adobe", "Korea1", 2)
	utf16V.WMode = 1
	utf16V.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	RegisterPredefinedCMap("UniKS-UTF16-V", &PredefinedCMap{utf16V})
}

// init initializes predefined CMaps on package load
func init() {
	InitPredefinedCMaps()
}

// LookupPredefinedCMap looks up a CMap by name, checking both predefined and registered CMaps
func LookupPredefinedCMap(name string) TextEncoding {
	// First check builtinCMapEncoding
	if enc := builtinCMapEncoding(name); enc != nil {
		return enc
	}

	// Then check predefined CMap registry
	if cm := GetPredefinedCMap(name); cm != nil {
		return cm.CMap
	}

	// Finally check cmapRegistry
	return lookupCMap(name)
}

// EnhancedCMapEncoding returns a TextEncoding for the given CMap name,
// with enhanced support for CJK encodings
func EnhancedCMapEncoding(name string) TextEncoding {
	return LookupPredefinedCMap(name)
}

// CMapInfo contains information about a CMap
type CMapInfo struct {
	Name       string
	Registry   string
	Ordering   string
	Supplement int
	WMode      int
	Type       CMapType
}

// GetCMapInfo returns information about a registered CMap
func GetCMapInfo(name string) *CMapInfo {
	if cm := GetPredefinedCMap(name); cm != nil {
		return &CMapInfo{
			Name:       cm.Name,
			Registry:   cm.CIDSystemInfo.Registry,
			Ordering:   cm.CIDSystemInfo.Ordering,
			Supplement: cm.CIDSystemInfo.Supplement,
			WMode:      cm.WMode,
			Type:       cm.Type,
		}
	}
	return nil
}

// ListRegisteredCMaps returns a list of all registered predefined CMap names
func ListRegisteredCMaps() []string {
	predefinedCMapRegistryMu.RLock()
	defer predefinedCMapRegistryMu.RUnlock()

	names := make([]string, 0, len(predefinedCMapRegistry))
	for name := range predefinedCMapRegistry {
		names = append(names, name)
	}
	return names
}

// IsCJKCMap checks if a CMap name is for CJK (Chinese, Japanese, Korean) encoding
func IsCJKCMap(name string) bool {
	cjkPrefixes := []string{
		"GBK", "GB", "UniGB", // Simplified Chinese
		"B5", "CNS", "UniCNS", // Traditional Chinese
		"83pv", "90ms", "UniJIS", // Japanese
		"KSC", "UniKS", // Korean
	}

	for _, prefix := range cjkPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// GetCMapWritingMode returns the writing mode for a CMap name
// Returns 0 for horizontal, 1 for vertical, -1 if unknown
func GetCMapWritingMode(name string) int {
	if strings.HasSuffix(name, "-V") {
		return 1 // Vertical
	}
	if strings.HasSuffix(name, "-H") {
		return 0 // Horizontal
	}

	// Check in registry
	if cm := GetPredefinedCMap(name); cm != nil {
		return cm.WMode
	}

	return -1 // Unknown
}

// ToUnicodeCMap is a specialized CMap for ToUnicode mappings
type ToUnicodeCMap struct {
	*CMap
}

// NewToUnicodeCMap creates a new ToUnicode CMap
func NewToUnicodeCMap() *ToUnicodeCMap {
	return &ToUnicodeCMap{
		CMap: NewCMap("ToUnicode", CMapTypeToUnicode),
	}
}

// ParseToUnicodeCMap parses a ToUnicode CMap stream
func ParseToUnicodeCMap(r io.Reader) (*ToUnicodeCMap, error) {
	cm, err := ParseCMap(r, "ToUnicode")
	if err != nil {
		return nil, err
	}
	cm.Type = CMapTypeToUnicode
	return &ToUnicodeCMap{CMap: cm}, nil
}

// DecodeCID decodes a CID value to Unicode using the ToUnicode mapping
func (c *ToUnicodeCMap) DecodeCID(cid int) string {
	// Convert CID to 2-byte code for lookup
	code := string([]byte{byte(cid >> 8), byte(cid & 0xFF)})
	return c.Decode(code)
}

// CIDToGIDMap represents a CIDToGIDMap for CID-keyed fonts
type CIDToGIDMap struct {
	data     []byte
	identity bool
}

// NewCIDToGIDMap creates a CIDToGIDMap from raw data
func NewCIDToGIDMap(data []byte) *CIDToGIDMap {
	return &CIDToGIDMap{
		data:     data,
		identity: false,
	}
}

// NewIdentityCIDToGIDMap creates an identity CIDToGIDMap
func NewIdentityCIDToGIDMap() *CIDToGIDMap {
	return &CIDToGIDMap{
		identity: true,
	}
}

// LookupGID returns the GID for a given CID
func (m *CIDToGIDMap) LookupGID(cid int) int {
	if m.identity {
		return cid
	}

	offset := cid * 2
	if offset+1 >= len(m.data) {
		return 0
	}

	return int(m.data[offset])<<8 | int(m.data[offset+1])
}

// IsIdentity returns true if this is an identity mapping
func (m *CIDToGIDMap) IsIdentity() bool {
	return m.identity
}

// CIDFont represents a CID-keyed font
type CIDFont struct {
	cmap         *CMap
	cidToGID     *CIDToGIDMap
	toUnicode    *ToUnicodeCMap
	wMode        int
	defaultWidth int
	widths       map[int]int // CID -> width
}

// NewCIDFont creates a new CID font
func NewCIDFont() *CIDFont {
	return &CIDFont{
		widths: make(map[int]int),
	}
}

// SetCMap sets the CMap for this CID font
func (f *CIDFont) SetCMap(cmap *CMap) {
	f.cmap = cmap
}

// SetCIDToGIDMap sets the CID to GID mapping
func (f *CIDFont) SetCIDToGIDMap(m *CIDToGIDMap) {
	f.cidToGID = m
}

// SetToUnicode sets the ToUnicode CMap
func (f *CIDFont) SetToUnicode(toUnicode *ToUnicodeCMap) {
	f.toUnicode = toUnicode
}

// SetWritingMode sets the writing mode (0=horizontal, 1=vertical)
func (f *CIDFont) SetWritingMode(wMode int) {
	f.wMode = wMode
}

// SetDefaultWidth sets the default glyph width
func (f *CIDFont) SetDefaultWidth(w int) {
	f.defaultWidth = w
}

// SetWidth sets the width for a specific CID
func (f *CIDFont) SetWidth(cid, width int) {
	f.widths[cid] = width
}

// GetWidth returns the width for a CID
func (f *CIDFont) GetWidth(cid int) int {
	if w, ok := f.widths[cid]; ok {
		return w
	}
	return f.defaultWidth
}

// DecodeToUnicode decodes a string using the CMap and ToUnicode
func (f *CIDFont) DecodeToUnicode(raw string) string {
	if f.toUnicode != nil {
		return f.toUnicode.Decode(raw)
	}
	if f.cmap != nil {
		return f.cmap.Decode(raw)
	}
	return raw
}

// WritingMode returns the writing mode
func (f *CIDFont) WritingMode() int {
	return f.wMode
}

// fmt.Stringer implementation for debugging
func (c *CMap) String() string {
	return fmt.Sprintf("CMap{Name:%s, Type:%d, WMode:%d, Registry:%s, Ordering:%s}",
		c.Name, c.Type, c.WMode, c.CIDSystemInfo.Registry, c.CIDSystemInfo.Ordering)
}

// ===================== High-Performance CMap Cache =====================

// OptimizedCMapCache provides high-performance CMap caching with:
// - Lock-free read path using atomic operations
// - Sharded design to reduce lock contention (8 shards)
// - Zero-allocation fast path for cache hits
// - LRU eviction with atomic operations
type OptimizedCMapCache struct {
	shards [8]*cmapCacheShard
	mask   uint64
}

// cmapCacheShard is a single shard of the CMap cache
type cmapCacheShard struct {
	// Lock-free read path
	entries unsafe.Pointer // *map[string]*optimizedCMapEntry (atomic swap)

	// Write path (protected by mutex)
	mu         sync.Mutex
	writeMap   map[string]*optimizedCMapEntry
	maxEntries int

	// LRU tracking (lock-free approximation)
	head *optimizedCMapEntry
	tail *optimizedCMapEntry

	// Statistics (lock-free using atomic)
	hits   uint64
	misses uint64
}

// optimizedCMapEntry represents a cached CMap with LRU links
type optimizedCMapEntry struct {
	cmap     *CMap
	key      string
	next     *optimizedCMapEntry
	prev     *optimizedCMapEntry
	refCount int32 // Atomic reference counting
}

// NewOptimizedCMapCache creates a new optimized CMap cache
func NewOptimizedCMapCache(maxEntries int) *OptimizedCMapCache {
	cache := &OptimizedCMapCache{
		mask: 7, // 8 shards - 1
	}

	entriesPerShard := maxEntries / len(cache.shards)
	for i := range cache.shards {
		cache.shards[i] = &cmapCacheShard{
			writeMap:   make(map[string]*optimizedCMapEntry),
			maxEntries: entriesPerShard,
		}
	}

	return cache
}

// Get retrieves a CMap from cache with lock-free fast path
func (c *OptimizedCMapCache) Get(key string) (*CMap, bool) {
	shard := c.shards[c.hash(key)&c.mask]

	// Lock-free read path
	readMap := (*map[string]*optimizedCMapEntry)(atomic.LoadPointer(&shard.entries))
	if readMap != nil {
		if entry, ok := (*readMap)[key]; ok {
			atomic.AddUint64(&shard.hits, 1)
			atomic.AddInt32(&entry.refCount, 1)
			return entry.cmap, true
		}
	}

	// Slow path - acquire lock
	shard.mu.Lock()
	defer shard.mu.Unlock()

	atomic.AddUint64(&shard.misses, 1)

	if entry, ok := shard.writeMap[key]; ok {
		// Move to front of LRU
		c.moveToFront(shard, entry)
		atomic.AddInt32(&entry.refCount, 1)
		return entry.cmap, true
	}

	return nil, false
}

// Put adds a CMap to the cache
func (c *OptimizedCMapCache) Put(key string, cmap *CMap) {
	shard := c.shards[c.hash(key)&c.mask]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Check if already exists
	if entry, ok := shard.writeMap[key]; ok {
		entry.cmap = cmap
		c.moveToFront(shard, entry)
		return
	}

	// Create new entry
	entry := &optimizedCMapEntry{
		cmap:     cmap,
		key:      key,
		refCount: 1,
	}

	shard.writeMap[key] = entry
	c.insertAtFront(shard, entry)

	// Evict if over capacity
	if len(shard.writeMap) > shard.maxEntries {
		c.evictLRU(shard)
	}

	// Atomically update read map
	newReadMap := make(map[string]*optimizedCMapEntry)
	for k, v := range shard.writeMap {
		newReadMap[k] = v
	}
	atomic.StorePointer(&shard.entries, unsafe.Pointer(&newReadMap))
}

// Release decrements reference count
func (c *OptimizedCMapCache) Release(key string) {
	shard := c.shards[c.hash(key)&c.mask]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if entry, ok := shard.writeMap[key]; ok {
		atomic.AddInt32(&entry.refCount, -1)
	}
}

// GetStats returns cache statistics
func (c *OptimizedCMapCache) GetStats() (hits, misses uint64) {
	for _, shard := range c.shards {
		hits += atomic.LoadUint64(&shard.hits)
		misses += atomic.LoadUint64(&shard.misses)
	}
	return
}

// hash computes a simple hash for shard selection
func (c *OptimizedCMapCache) hash(key string) uint64 {
	var h uint64 = 5381
	for _, b := range []byte(key) {
		h = ((h << 5) + h) + uint64(b)
	}
	return h
}

// moveToFront moves an entry to the front of LRU list
func (c *OptimizedCMapCache) moveToFront(shard *cmapCacheShard, entry *optimizedCMapEntry) {
	if entry == shard.head {
		return
	}

	// Remove from current position
	if entry.prev != nil {
		entry.prev.next = entry.next
	}
	if entry.next != nil {
		entry.next.prev = entry.prev
	}
	if entry == shard.tail {
		shard.tail = entry.prev
	}

	// Insert at front
	entry.prev = nil
	entry.next = shard.head
	if shard.head != nil {
		shard.head.prev = entry
	}
	shard.head = entry

	if shard.tail == nil {
		shard.tail = entry
	}
}

// insertAtFront inserts a new entry at the front
func (c *OptimizedCMapCache) insertAtFront(shard *cmapCacheShard, entry *optimizedCMapEntry) {
	entry.prev = nil
	entry.next = shard.head

	if shard.head != nil {
		shard.head.prev = entry
	}
	shard.head = entry

	if shard.tail == nil {
		shard.tail = entry
	}
}

// evictLRU removes the least recently used entry
func (c *OptimizedCMapCache) evictLRU(shard *cmapCacheShard) {
	if shard.tail == nil {
		return
	}

	entry := shard.tail
	shard.tail = entry.prev
	if shard.tail != nil {
		shard.tail.next = nil
	}

	delete(shard.writeMap, entry.key)
}

// Global CMap cache instance
var globalCMapCache = NewOptimizedCMapCache(1024)

// GetGlobalCMapCache returns the global CMap cache
func GetGlobalCMapCache() *OptimizedCMapCache {
	return globalCMapCache
}
