// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"strings"
	"sync"
)

// CJKFontInfo contains information about CJK fonts
type CJKFontInfo struct {
	Name       string // Font name
	Registry   string // Registry (e.g., "Adobe")
	Ordering   string // Ordering (e.g., "GB1", "CNS1", "Japan1", "Korea1")
	Supplement int    // Supplement number
	IsVertical bool   // Whether vertical writing mode
	WMode      int    // Writing mode: 0=horizontal, 1=vertical
}

// CJKGlyphMetrics contains glyph metrics for CJK fonts
type CJKGlyphMetrics struct {
	Width       float64 // Horizontal advance width
	Height      float64 // Vertical advance height
	VOriginX    float64 // Vertical origin X
	VOriginY    float64 // Vertical origin Y
	HasVertical bool    // Whether vertical metrics are available
}

// CIDFontDescriptor contains font descriptor information for CID fonts
type CIDFontDescriptor struct {
	FontName     string
	FontFamily   string
	Flags        int
	FontBBox     [4]float64
	ItalicAngle  float64
	Ascent       float64
	Descent      float64
	Leading      float64
	CapHeight    float64
	XHeight      float64
	StemV        float64
	StemH        float64
	AvgWidth     float64
	MaxWidth     float64
	MissingWidth float64
}

// ExtendedCIDFont extends CIDFont with additional CJK-specific features
type ExtendedCIDFont struct {
	*CIDFont
	mu          sync.RWMutex
	V           Value
	info        *CJKFontInfo
	descriptor  *CIDFontDescriptor
	vWidths     map[int]float64    // CID -> vertical width
	vOrigins    map[int][2]float64 // CID -> [vx, vy]
	defaultW    float64
	defaultW2   [3]float64 // [w1y, vx, vy] for DW2
	cidToGIDArr []uint16
}

// NewExtendedCIDFont creates a new ExtendedCIDFont from a PDF value
func NewExtendedCIDFont(v Value) *ExtendedCIDFont {
	cf := &ExtendedCIDFont{
		CIDFont:   NewCIDFont(),
		V:         v,
		vWidths:   make(map[int]float64),
		vOrigins:  make(map[int][2]float64),
		defaultW:  1000,
		defaultW2: [3]float64{-880, 500, 880}, // Default DW2 values
	}
	cf.SetDefaultWidth(1000) // Set initial default width
	cf.parse()
	return cf
}

func (cf *ExtendedCIDFont) parse() {
	cf.mu.Lock()
	defer cf.mu.Unlock()

	// Parse CIDSystemInfo
	sysInfo := cf.V.Key("CIDSystemInfo")
	if sysInfo.Kind() == Dict {
		cf.info = &CJKFontInfo{
			Registry:   sysInfo.Key("Registry").Text(),
			Ordering:   sysInfo.Key("Ordering").Text(),
			Supplement: int(sysInfo.Key("Supplement").Int64()),
		}
	}

	// Parse font descriptor
	fontDesc := cf.V.Key("FontDescriptor")
	if fontDesc.Kind() == Dict {
		cf.descriptor = parseCIDFontDescriptor(fontDesc)
	}

	// Parse default width
	if dw := cf.V.Key("DW"); dw.Kind() == Integer || dw.Kind() == Real {
		cf.defaultW = dw.Float64()
		cf.SetDefaultWidth(int(cf.defaultW))
	}

	// Parse widths array (W)
	cf.parseWidths()

	// Parse vertical default (DW2)
	if dw2 := cf.V.Key("DW2"); dw2.Kind() == Array && dw2.Len() == 3 {
		cf.defaultW2[0] = dw2.Index(0).Float64()
		cf.defaultW2[1] = dw2.Index(1).Float64()
		cf.defaultW2[2] = dw2.Index(2).Float64()
	}

	// Parse vertical widths (W2)
	cf.parseVerticalWidths()

	// Parse CIDToGIDMap
	cf.parseCIDToGIDMap()
}

func (cf *ExtendedCIDFont) parseWidths() {
	w := cf.V.Key("W")
	if w.Kind() != Array {
		return
	}

	i := 0
	for i < w.Len() {
		first := w.Index(i)
		if first.Kind() != Integer {
			i++
			continue
		}
		startCID := int(first.Int64())
		i++

		if i >= w.Len() {
			break
		}

		second := w.Index(i)
		if second.Kind() == Array {
			// Format: c [w1 w2 ... wn]
			for j := 0; j < second.Len(); j++ {
				cf.SetWidth(startCID+j, int(second.Index(j).Float64()))
			}
			i++
		} else if second.Kind() == Integer {
			// Format: c_first c_last w
			endCID := int(second.Int64())
			i++
			if i >= w.Len() {
				break
			}
			width := int(w.Index(i).Float64())
			for cid := startCID; cid <= endCID; cid++ {
				cf.SetWidth(cid, width)
			}
			i++
		} else {
			i++
		}
	}
}

func (cf *ExtendedCIDFont) parseVerticalWidths() {
	w2 := cf.V.Key("W2")
	if w2.Kind() != Array {
		return
	}

	i := 0
	for i < w2.Len() {
		first := w2.Index(i)
		if first.Kind() != Integer {
			i++
			continue
		}
		startCID := int(first.Int64())
		i++

		if i >= w2.Len() {
			break
		}

		second := w2.Index(i)
		if second.Kind() == Array {
			// Format: c [w1_1y v_1x v_1y w2_1y v_2x v_2y ...]
			for j := 0; j+2 < second.Len(); j += 3 {
				cid := startCID + j/3
				cf.vWidths[cid] = second.Index(j).Float64()
				cf.vOrigins[cid] = [2]float64{
					second.Index(j + 1).Float64(),
					second.Index(j + 2).Float64(),
				}
			}
			i++
		} else if second.Kind() == Integer {
			// Format: c_first c_last w1_1y v_1x v_1y
			endCID := int(second.Int64())
			i++
			if i+2 >= w2.Len() {
				break
			}
			w1y := w2.Index(i).Float64()
			vx := w2.Index(i + 1).Float64()
			vy := w2.Index(i + 2).Float64()
			for cid := startCID; cid <= endCID; cid++ {
				cf.vWidths[cid] = w1y
				cf.vOrigins[cid] = [2]float64{vx, vy}
			}
			i += 3
		} else {
			i++
		}
	}
}

func (cf *ExtendedCIDFont) parseCIDToGIDMap() {
	cidToGID := cf.V.Key("CIDToGIDMap")
	if cidToGID.Kind() == Name && cidToGID.Name() == "Identity" {
		// Identity mapping - CID equals GID
		return
	}

	if cidToGID.Kind() != Stream {
		return
	}

	// Read the stream data
	data := cidToGID.Reader()
	if data == nil {
		return
	}
	defer data.Close() // Important: close reader to prevent resource leak

	buf := make([]byte, 65536*2) // Max 64K CIDs, 2 bytes each
	n, _ := data.Read(buf)
	buf = buf[:n]

	// Parse as big-endian uint16 pairs
	cf.cidToGIDArr = make([]uint16, n/2)
	for i := 0; i < n/2; i++ {
		cf.cidToGIDArr[i] = uint16(buf[i*2])<<8 | uint16(buf[i*2+1])
	}
}

// VerticalWidth returns the vertical width of the given CID
func (cf *ExtendedCIDFont) VerticalWidth(cid int) float64 {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	if w, ok := cf.vWidths[cid]; ok {
		return w
	}
	return cf.defaultW2[0]
}

// VerticalOrigin returns the vertical origin of the given CID
func (cf *ExtendedCIDFont) VerticalOrigin(cid int) (float64, float64) {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	if vo, ok := cf.vOrigins[cid]; ok {
		return vo[0], vo[1]
	}
	// Default: half of horizontal width, and DW2[2]
	w := float64(cf.GetWidth(cid))
	return w / 2, cf.defaultW2[2]
}

// GID returns the GID for the given CID
func (cf *ExtendedCIDFont) GID(cid int) uint16 {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	if cf.cidToGIDArr == nil || cid >= len(cf.cidToGIDArr) {
		return uint16(cid) // Identity mapping
	}
	return cf.cidToGIDArr[cid]
}

// Info returns the CJK font info
func (cf *ExtendedCIDFont) Info() *CJKFontInfo {
	cf.mu.RLock()
	defer cf.mu.RUnlock()
	return cf.info
}

// Descriptor returns the font descriptor
func (cf *ExtendedCIDFont) Descriptor() *CIDFontDescriptor {
	cf.mu.RLock()
	defer cf.mu.RUnlock()
	return cf.descriptor
}

// IsVertical returns true if this font uses vertical writing mode
func (cf *ExtendedCIDFont) IsVertical() bool {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	if cf.info != nil {
		return cf.info.IsVertical
	}
	return cf.WritingMode() == 1
}

// parseCIDFontDescriptor parses a FontDescriptor dictionary
func parseCIDFontDescriptor(v Value) *CIDFontDescriptor {
	if v.Kind() != Dict {
		return nil
	}

	fd := &CIDFontDescriptor{
		FontName:     v.Key("FontName").Name(),
		FontFamily:   v.Key("FontFamily").Text(),
		Flags:        int(v.Key("Flags").Int64()),
		ItalicAngle:  v.Key("ItalicAngle").Float64(),
		Ascent:       v.Key("Ascent").Float64(),
		Descent:      v.Key("Descent").Float64(),
		Leading:      v.Key("Leading").Float64(),
		CapHeight:    v.Key("CapHeight").Float64(),
		XHeight:      v.Key("XHeight").Float64(),
		StemV:        v.Key("StemV").Float64(),
		StemH:        v.Key("StemH").Float64(),
		AvgWidth:     v.Key("AvgWidth").Float64(),
		MaxWidth:     v.Key("MaxWidth").Float64(),
		MissingWidth: v.Key("MissingWidth").Float64(),
	}

	bbox := v.Key("FontBBox")
	if bbox.Kind() == Array && bbox.Len() == 4 {
		fd.FontBBox[0] = bbox.Index(0).Float64()
		fd.FontBBox[1] = bbox.Index(1).Float64()
		fd.FontBBox[2] = bbox.Index(2).Float64()
		fd.FontBBox[3] = bbox.Index(3).Float64()
	}

	return fd
}

// CJKFontRegistry is a registry of CJK fonts
type CJKFontRegistry struct {
	mu    sync.RWMutex
	fonts map[string]*CJKFontInfo
}

var globalCJKFontRegistry = &CJKFontRegistry{
	fonts: make(map[string]*CJKFontInfo),
}

// RegisterCJKFont registers a CJK font
func RegisterCJKFont(name string, info *CJKFontInfo) {
	globalCJKFontRegistry.mu.Lock()
	defer globalCJKFontRegistry.mu.Unlock()
	globalCJKFontRegistry.fonts[name] = info
}

// GetCJKFontInfo returns information about a CJK font
func GetCJKFontInfo(name string) *CJKFontInfo {
	globalCJKFontRegistry.mu.RLock()
	defer globalCJKFontRegistry.mu.RUnlock()
	return globalCJKFontRegistry.fonts[name]
}

// IsCJKFont returns true if the font name suggests a CJK font
func IsCJKFont(fontName string) bool {
	lower := strings.ToLower(fontName)
	cjkIndicators := []string{
		"simsun", "simhei", "simkai", "fangsong", "kaiti", "heiti", "songti",
		"mingliu", "pminglu", "dfkai", "dflihei",
		"mshei", "msson", "msfan",
		"hiragi", "mincho", "gothic", "kozuka", "kozgo", "kozmn",
		"msmincho", "msgothic", "meiryo",
		"batang", "dotum", "gulim", "gungsuh", "malgun",
		"stxihei", "stheiti", "stfangsong", "stkaiti", "stsong",
		"adobesong", "adobeheiti", "adobekaiti", "adobefangsong",
		"notosans", "notosanscjk", "notoserifcjk",
		"sourcehansans", "sourcehanserif",
	}

	for _, ind := range cjkIndicators {
		if strings.Contains(lower, ind) {
			return true
		}
	}

	return false
}

// DetectCJKOrdering detects the CJK ordering from font name
func DetectCJKOrdering(fontName string) string {
	lower := strings.ToLower(fontName)

	// Simplified Chinese indicators
	scIndicators := []string{"simsun", "simhei", "simkai", "fangsong", "stxihei", "stheiti", "stfangsong", "stkaiti", "stsong", "songti", "heiti", "adobesong", "adobeheiti", "adobekaiti", "adobefangsong", "sourcehansanssc", "sourcehanserifsc", "notosanssc", "notoserifsc"}
	for _, ind := range scIndicators {
		if strings.Contains(lower, ind) {
			return "GB1"
		}
	}

	// Traditional Chinese indicators
	tcIndicators := []string{"mingliu", "pminglu", "dfkai", "dflihei", "mshei", "msson", "sourcehansanstc", "sourcehanseriftc", "notosanstc", "notoseriftc"}
	for _, ind := range tcIndicators {
		if strings.Contains(lower, ind) {
			return "CNS1"
		}
	}

	// Japanese indicators
	jpIndicators := []string{"hiragi", "mincho", "gothic", "kozuka", "kozgo", "kozmn", "msmincho", "msgothic", "meiryo", "sourcehansansjp", "sourcehanserifserijp", "notosansjp", "notoserifjp"}
	for _, ind := range jpIndicators {
		if strings.Contains(lower, ind) {
			return "Japan1"
		}
	}

	// Korean indicators
	krIndicators := []string{"batang", "dotum", "gulim", "gungsuh", "malgun", "sourcehansanskr", "sourcehanserifkr", "notosanskr", "notoserifkr"}
	for _, ind := range krIndicators {
		if strings.Contains(lower, ind) {
			return "Korea1"
		}
	}

	return ""
}

// VerticalTextTransform transforms text coordinates for vertical writing
type VerticalTextTransform struct {
	Enabled bool
	OriginX float64
	OriginY float64
	Angle   float64 // Rotation angle in degrees (typically 90 or -90)
}

// TransformGlyph transforms a single glyph position for vertical writing
func (vt *VerticalTextTransform) TransformGlyph(x, y, w, h float64) (nx, ny, nw, nh float64) {
	if !vt.Enabled {
		return x, y, w, h
	}

	// For vertical writing, rotate 90 degrees clockwise
	// and adjust origin
	nx = vt.OriginX + y - vt.OriginY
	ny = vt.OriginY - (x - vt.OriginX) - w
	nw = h
	nh = w

	return nx, ny, nw, nh
}

// GetVerticalVariant returns the vertical variant of a character if available
func GetVerticalVariant(r rune) rune {
	if variant, ok := verticalVariants[r]; ok {
		return variant
	}
	return r
}

// verticalVariants maps horizontal punctuation to vertical variants
var verticalVariants = map[rune]rune{
	'（': '︵', // Parentheses
	'）': '︶',
	'〈': '︿', // Angle brackets
	'〉': '﹀',
	'《': '︽', // Double angle brackets
	'》': '︾',
	'「': '﹁', // Corner brackets
	'」': '﹂',
	'『': '﹃', // White corner brackets
	'』': '﹄',
	'【': '︻', // Black lenticular brackets
	'】': '︼',
	'〔': '︹', // Tortoise shell brackets
	'〕': '︺',
	'〖': '︗', // White lenticular brackets
	'〗': '︘',
	'—': '︱', // Em dash
	'…': '︙', // Ellipsis
	'、': '︑', // Ideographic comma
	'。': '︒', // Ideographic full stop
	'，': '︐', // Comma (for vertical use)
}

// ShouldRotateGlyph returns true if the glyph should be rotated in vertical text
func ShouldRotateGlyph(r rune) bool {
	// CJK ideographs and some symbols should not be rotated
	if (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Unified Ideographs Extension A
		(r >= 0x20000 && r <= 0x2A6DF) || // CJK Unified Ideographs Extension B
		(r >= 0x2A700 && r <= 0x2B73F) || // CJK Unified Ideographs Extension C
		(r >= 0x2B740 && r <= 0x2B81F) || // CJK Unified Ideographs Extension D
		(r >= 0x2B820 && r <= 0x2CEAF) || // CJK Unified Ideographs Extension E
		(r >= 0x2CEB0 && r <= 0x2EBEF) || // CJK Unified Ideographs Extension F
		(r >= 0x30000 && r <= 0x3134F) || // CJK Unified Ideographs Extension G
		(r >= 0x31350 && r <= 0x323AF) || // CJK Unified Ideographs Extension H
		(r >= 0x3000 && r <= 0x303F) || // CJK Symbols and Punctuation
		(r >= 0xFF00 && r <= 0xFFEF) || // Halfwidth and Fullwidth Forms
		(r >= 0x3040 && r <= 0x309F) || // Hiragana
		(r >= 0x30A0 && r <= 0x30FF) || // Katakana
		(r >= 0x31F0 && r <= 0x31FF) || // Katakana Phonetic Extensions
		(r >= 0xAC00 && r <= 0xD7AF) || // Hangul Syllables
		(r >= 0x1100 && r <= 0x11FF) { // Hangul Jamo
		return false
	}

	// Latin and other scripts should be rotated
	return true
}

// CJKTextProcessor processes CJK text for proper rendering
type CJKTextProcessor struct {
	isVertical bool
	font       *ExtendedCIDFont
}

// NewCJKTextProcessor creates a new CJK text processor
func NewCJKTextProcessor(font *ExtendedCIDFont, isVertical bool) *CJKTextProcessor {
	return &CJKTextProcessor{
		isVertical: isVertical,
		font:       font,
	}
}

// ProcessText processes CJK text, handling vertical writing and character variants
func (p *CJKTextProcessor) ProcessText(text string) string {
	if !p.isVertical {
		return text
	}

	runes := []rune(text)
	result := make([]rune, len(runes))

	for i, r := range runes {
		result[i] = GetVerticalVariant(r)
	}

	return string(result)
}

// GetGlyphMetrics returns the metrics for a glyph in the current writing mode
func (p *CJKTextProcessor) GetGlyphMetrics(cid int) CJKGlyphMetrics {
	metrics := CJKGlyphMetrics{}

	if p.font == nil {
		metrics.Width = 1000
		return metrics
	}

	metrics.Width = float64(p.font.GetWidth(cid))

	if p.isVertical {
		metrics.Height = p.font.VerticalWidth(cid)
		metrics.VOriginX, metrics.VOriginY = p.font.VerticalOrigin(cid)
		metrics.HasVertical = true
	}

	return metrics
}
