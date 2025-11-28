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
	"unicode/utf16"
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

	mu sync.RWMutex
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
		Name: name,
		Type: cmapType,
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

// LookupCID looks up the CID for a given character code
func (c *CMap) LookupCID(code []byte) (int, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

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

// Decode implements TextEncoding interface for ToUnicode CMaps
func (c *CMap) Decode(raw string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result strings.Builder
	result.Grow(len(raw) * 2)

	i := 0
	for i < len(raw) {
		matched := false

		// Try different byte lengths based on code space ranges
		for length := 4; length >= 1 && !matched; length-- {
			if i+length > len(raw) {
				continue
			}

			code := raw[i : i+length]

			// Check bfchar mappings
			for _, bf := range c.bfChars {
				if bf.orig == code {
					result.WriteString(cmapDecodeUTF16BE(bf.repl))
					i += length
					matched = true
					break
				}
			}
			if matched {
				break
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
					break
				}
			}
			if matched {
				break
			}
		}

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
		RegisterPredefinedCMap("Identity-H", &PredefinedCMap{&CMap{
			Name:    "Identity-H",
			WMode:   0,
			useCMap: enc,
		}})
	}
	if enc, ok := predefinedCMaps["Identity-V"]; ok {
		RegisterPredefinedCMap("Identity-V", &PredefinedCMap{&CMap{
			Name:    "Identity-V",
			WMode:   1,
			useCMap: enc,
		}})
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
	RegisterPredefinedCMap("GBK-EUC-H", &PredefinedCMap{cm})

	// GBK-EUC-V: GBK encoding to CID vertical
	cmV := NewCMap("GBK-EUC-V", CMapTypeCID)
	cmV.SetCIDSystemInfo("Adobe", "GB1", 2)
	cmV.WMode = 1
	cmV.AddCodeSpaceRange([]byte{0x00}, []byte{0x80})
	cmV.AddCodeSpaceRange([]byte{0x81, 0x40}, []byte{0xFE, 0xFE})
	RegisterPredefinedCMap("GBK-EUC-V", &PredefinedCMap{cmV})

	// UniGB-UCS2-H: Unicode to CID horizontal
	uniH := NewCMap("UniGB-UCS2-H", CMapTypeCID)
	uniH.SetCIDSystemInfo("Adobe", "GB1", 4)
	uniH.WMode = 0
	uniH.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	RegisterPredefinedCMap("UniGB-UCS2-H", &PredefinedCMap{uniH})

	// UniGB-UCS2-V: Unicode to CID vertical
	uniV := NewCMap("UniGB-UCS2-V", CMapTypeCID)
	uniV.SetCIDSystemInfo("Adobe", "GB1", 4)
	uniV.WMode = 1
	uniV.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	RegisterPredefinedCMap("UniGB-UCS2-V", &PredefinedCMap{uniV})

	// UniGB-UTF16-H
	utf16H := NewCMap("UniGB-UTF16-H", CMapTypeCID)
	utf16H.SetCIDSystemInfo("Adobe", "GB1", 5)
	utf16H.WMode = 0
	utf16H.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
	RegisterPredefinedCMap("UniGB-UTF16-H", &PredefinedCMap{utf16H})

	// UniGB-UTF16-V
	utf16V := NewCMap("UniGB-UTF16-V", CMapTypeCID)
	utf16V.SetCIDSystemInfo("Adobe", "GB1", 5)
	utf16V.WMode = 1
	utf16V.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})
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
