// Copyright 2014 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// parseFontStyles parses font name to detect bold, italic, underline styles
func parseFontStyles(fontName string) (bold, italic, underline bool) {
	// Optimized: avoid ToLower allocation by checking both cases inline
	n := len(fontName)

	// Check for "bold" or "black" (case-insensitive, no allocation)
	for i := 0; i+3 < n; i++ {
		c := fontName[i]
		// Check for "bold"
		if (c == 'B' || c == 'b') &&
			(fontName[i+1] == 'O' || fontName[i+1] == 'o') &&
			(fontName[i+2] == 'L' || fontName[i+2] == 'l') &&
			(fontName[i+3] == 'D' || fontName[i+3] == 'd') {
			bold = true
			break
		}
		// Check for "black" (also considered bold)
		if i+4 < n &&
			(c == 'B' || c == 'b') &&
			(fontName[i+1] == 'L' || fontName[i+1] == 'l') &&
			(fontName[i+2] == 'A' || fontName[i+2] == 'a') &&
			(fontName[i+3] == 'C' || fontName[i+3] == 'c') &&
			(fontName[i+4] == 'K' || fontName[i+4] == 'k') {
			bold = true
			break
		}
	}

	// Check for "italic" or "oblique" (case-insensitive, no allocation)
	for i := 0; i+5 < n; i++ {
		c := fontName[i]
		// Check for "italic"
		if (c == 'I' || c == 'i') &&
			(fontName[i+1] == 'T' || fontName[i+1] == 't') &&
			(fontName[i+2] == 'A' || fontName[i+2] == 'a') &&
			(fontName[i+3] == 'L' || fontName[i+3] == 'l') &&
			(fontName[i+4] == 'I' || fontName[i+4] == 'i') &&
			(fontName[i+5] == 'C' || fontName[i+5] == 'c') {
			italic = true
			break
		}
		// Check for "oblique"
		if i+6 < n &&
			(c == 'O' || c == 'o') &&
			(fontName[i+1] == 'B' || fontName[i+1] == 'b') &&
			(fontName[i+2] == 'L' || fontName[i+2] == 'l') &&
			(fontName[i+3] == 'I' || fontName[i+3] == 'i') &&
			(fontName[i+4] == 'Q' || fontName[i+4] == 'q') &&
			(fontName[i+5] == 'U' || fontName[i+5] == 'u') &&
			(fontName[i+6] == 'E' || fontName[i+6] == 'e') {
			italic = true
			break
		}
	}

	underline = false
	return
}

// A Page represent a single page in a PDF file.
// The methods interpret a Page dictionary stored in V.
type Page struct {
	V         Value
	fontCache FontCacheInterface // Optional font cache for performance optimization (interface supports both implementations)
}

// Cleanup releases resources held by the Page, specifically the fontCache reference.
// Call this after processing a page to prevent memory leaks in batch operations.
// This method is safe to call multiple times.
func (p *Page) Cleanup() {
	p.fontCache = nil
}

// Page returns the page for the given page number.
// Page numbers are indexed starting at 1, not 0.
// If the page is not found, Page returns a Page with p.V.IsNull().
func (r *Reader) Page(num int) Page {
	num-- // now 0-indexed
	page := r.Trailer().Key("Root").Key("Pages")
Search:
	for page.Key("Type").Name() == "Pages" {
		count := int(page.Key("Count").Int64())
		if count < num {
			return Page{V: Value{}}
		}
		kids := page.Key("Kids")
		for i := 0; i < kids.Len(); i++ {
			kid := kids.Index(i)
			if kid.Key("Type").Name() == "Pages" {
				c := int(kid.Key("Count").Int64())
				if num < c {
					page = kid
					continue Search
				}
				num -= c
				continue
			}
			if kid.Key("Type").Name() == "Page" {
				if num == 0 {
					return Page{V: kid}
				}
				num--
			}
		}
		break
	}
	return Page{V: Value{}}
}

// NumPage returns the number of pages in the PDF file.
func (r *Reader) NumPage() int {
	return int(r.Trailer().Key("Root").Key("Pages").Key("Count").Int64())
}

// SetFontCache sets a font cache for this page to improve performance
// during text extraction by reusing parsed fonts.
// Deprecated: Use SetFontCacheInterface for better flexibility.
func (p *Page) SetFontCache(cache *GlobalFontCache) {
	p.fontCache = cache
}

// SetFontCacheInterface sets a font cache using the interface
// This supports both GlobalFontCache and OptimizedFontCache
func (p *Page) SetFontCacheInterface(cache FontCacheInterface) {
	p.fontCache = cache
}

// GetPlainText returns all the text in the PDF file
func (r *Reader) GetPlainText() (reader io.Reader, err error) {
	pages := r.NumPage()

	// Set a reasonable object cache capacity to prevent unlimited growth
	// For sequential page processing, limit cache to prevent memory explosion
	if r.GetCacheCapacity() <= 0 {
		cacheSize := pages * 10
		if cacheSize > 5000 {
			cacheSize = 5000 // Cap at 5000 objects
		}
		r.SetCacheCapacity(cacheSize)
	}

	var buf bytes.Buffer
	fonts := make(map[string]*Font)
	for i := 1; i <= pages; i++ {
		p := r.Page(i)
		for _, name := range p.Fonts() { // cache fonts so we don't continually parse charmap
			if _, ok := fonts[name]; !ok {
				f := p.Font(name)
				fonts[name] = &f
			}
		}
		text, err := p.GetPlainText(context.Background(), fonts)
		if err != nil {
			return &bytes.Buffer{}, err
		}
		buf.WriteString(text)

		// CRITICAL FIX: Clear Page's fontCache reference after each page to prevent accumulation
		p.Cleanup()
	}

	// CRITICAL FIX: Clear the fonts map and trigger GC after all pages processed
	// This releases memory from accumulated Font objects
	fonts = nil

	return &buf, nil
}

// GetStyledTexts returns list all sentences in an array, that are included styles
func (r *Reader) GetStyledTexts() (sentences []Text, err error) {
	totalPage := r.NumPage()
	for pageIndex := 1; pageIndex <= totalPage; pageIndex++ {
		p := r.Page(pageIndex)

		if p.V.IsNull() || p.V.Key("Contents").Kind() == Null {
			continue
		}
		var lastTextStyle Text
		texts := p.Content().Text
		for _, text := range texts {
			if lastTextStyle == (Text{}) {
				lastTextStyle = text
				continue
			}

			if IsSameSentence(lastTextStyle, text) {
				lastTextStyle.S = lastTextStyle.S + text.S
			} else {
				sentences = append(sentences, lastTextStyle)
				lastTextStyle = text
			}
		}
		if len(lastTextStyle.S) > 0 {
			sentences = append(sentences, lastTextStyle)
		}
	}

	return sentences, err
}

func (p Page) findInherited(key string) Value {
	for v := p.V; !v.IsNull(); v = v.Key("Parent") {
		if r := v.Key(key); !r.IsNull() {
			return r
		}
	}
	return Value{}
}

/*
func (p Page) MediaBox() Value {
	return p.findInherited("MediaBox")
}

func (p Page) CropBox() Value {
	return p.findInherited("CropBox")
}
*/

// Resources returns the resources dictionary associated with the page.
func (p Page) Resources() Value {
	return p.findInherited("Resources")
}

// Fonts returns a list of the fonts associated with the page.
func (p Page) Fonts() []string {
	return p.Resources().Key("Font").Keys()
}

// Font returns the font with the given name associated with the page.
func (p Page) Font(name string) Font {
	fontValue := p.Resources().Key("Font").Key(name)

	// Use global font cache if available
	if p.fontCache != nil {
		// Generate cache key from page resources and font name
		key := fmt.Sprintf("page:%v:font:%s", p.V, name)

		// Try to get from cache
		if cached, ok := p.fontCache.Get(key); ok {
			return *cached
		}

		// Create new font and cache it
		font := Font{fontValue, nil}
		p.fontCache.Set(key, &font)
		return font
	}

	// No cache available, return new font
	return Font{fontValue, nil}
}

// A Font represent a font in a PDF file.
// The methods interpret a Font dictionary stored in V.
type Font struct {
	V   Value
	enc TextEncoding
}

type fontScope struct {
	fonts  map[string]*Font
	parent *fontScope
}

func (s *fontScope) Get(name string) *Font {
	for scope := s; scope != nil; scope = scope.parent {
		if scope.fonts == nil {
			continue
		}
		if f, ok := scope.fonts[name]; ok {
			return f
		}
	}
	return nil
}

func (p Page) buildFontScope(resources Value, cache map[string]*Font, parent *fontScope) *fontScope {
	scope := &fontScope{parent: parent}
	fontDict := resources.Key("Font")
	if fontDict.Kind() != Dict {
		return scope
	}
	scope.fonts = make(map[string]*Font)
	for _, name := range fontDict.Keys() {
		if cache != nil {
			if f, ok := cache[name]; ok {
				scope.fonts[name] = f
				continue
			}
		}
		fontValue := fontDict.Key(name)
		font := &Font{fontValue, nil}
		scope.fonts[name] = font
		if cache != nil {
			cache[name] = font
		}
	}
	return scope
}

// BaseFont returns the font's name (BaseFont property).
func (f Font) BaseFont() string {
	return f.V.Key("BaseFont").Name()
}

// FirstChar returns the code point of the first character in the font.
func (f Font) FirstChar() int {
	return int(f.V.Key("FirstChar").Int64())
}

// LastChar returns the code point of the last character in the font.
func (f Font) LastChar() int {
	return int(f.V.Key("LastChar").Int64())
}

// Widths returns the widths of the glyphs in the font.
// In a well-formed PDF, len(f.Widths()) == f.LastChar()+1 - f.FirstChar().
func (f Font) Widths() []float64 {
	x := f.V.Key("Widths")
	var out []float64
	for i := 0; i < x.Len(); i++ {
		out = append(out, x.Index(i).Float64())
	}
	return out
}

// Width returns the width of the given code point.
func (f Font) Width(code int) float64 {
	first := f.FirstChar()
	last := f.LastChar()
	if code < first || last < code {
		return 0
	}
	return f.V.Key("Widths").Index(code - first).Float64()
}

// Encoder returns the encoding between font code point sequences and UTF-8.
// Pointer receiver is required so the computed encoder is cached on the shared
// Font instance instead of a copy. The previous value-receiver implementation
// rebuilt the encoder for every call, causing large allocations to pile up
// during batch extraction.
func (f *Font) Encoder() TextEncoding {
	if f == nil {
		return nil
	}

	if f.enc == nil { // caching the Encoder so we don't have to continually parse charmap
		f.enc = f.buildEncoder()
		if f.enc == nil {
			f.enc = &nopEncoder{}
		}
	}
	return f.enc
}

func (f *Font) buildEncoder() TextEncoding {
	if f.subtype() == "Type0" {
		if enc := f.type0Encoder(); enc != nil {
			return enc
		}
		return nil
	}
	if f.subtype() == "Type3" {
		if enc := f.cmapEncodingFromValue(f.V.Key("ToUnicode")); enc != nil {
			return enc
		}
		return f.simpleEncoder()
	}
	return f.simpleEncoder()
}

func (f *Font) simpleEncoder() TextEncoding {
	enc := f.V.Key("Encoding")
	switch enc.Kind() {
	case Name:
		switch enc.Name() {
		case "WinAnsiEncoding":
			return &byteEncoder{&winAnsiEncoding}
		case "MacRomanEncoding":
			return &byteEncoder{&macRomanEncoding}
		case "Identity-H":
			return f.charmapEncoding()
		default:
			if DebugOn {
				println("unknown encoding", enc.Name())
			}
			return &nopEncoder{}
		}
	case Dict:
		return &dictEncoder{enc.Key("Differences")}
	case Null:
		return f.charmapEncoding()
	case Stream:
		return f.cmapEncodingFromValue(enc)
	default:
		if DebugOn {
			println("unexpected encoding", enc.String())
		}
		return &nopEncoder{}
	}
}

func (f *Font) type0Encoder() TextEncoding {
	// Prefer ToUnicode if available
	if enc := f.cmapEncodingFromValue(f.V.Key("ToUnicode")); enc != nil {
		return enc
	}

	encoding := f.V.Key("Encoding")
	switch encoding.Kind() {
	case Stream:
		if enc := f.cmapEncodingFromValue(encoding); enc != nil {
			return enc
		}
	case Name:
		if enc := builtinCMapEncoding(encoding.Name()); enc != nil {
			return enc
		}
	case Null:
		// fall through to descendant or builtins
	default:
		if DebugOn {
			fmt.Printf("type0 encoding unexpected kind %s\n", encoding.String())
		}
	}

	// Some documents embed ToUnicode on the descendant font
	if desc := f.descendantFont(); desc.Kind() == Dict {
		if enc := f.cmapEncodingFromValue(desc.Key("ToUnicode")); enc != nil {
			return enc
		}
	}

	// Final fallback to Identity-H encoding
	fallback := "Identity-H"
	if f.writingMode() == 1 {
		fallback = "Identity-V"
	}
	if enc := builtinCMapEncoding(fallback); enc != nil {
		return enc
	}
	return nil
}

func (f Font) cmapEncodingFromValue(v Value) TextEncoding {
	if v.Kind() != Stream {
		return nil
	}
	m := readCmap(v)
	if m == nil {
		return nil
	}
	return m
}

func (f Font) subtype() string {
	return f.V.Key("Subtype").Name()
}

func (f Font) descendantFont() Value {
	desc := f.V.Key("DescendantFonts")
	if desc.Kind() != Array || desc.Len() == 0 {
		return Value{}
	}
	return desc.Index(0)
}

func (f Font) writingMode() int {
	desc := f.descendantFont()
	if desc.Kind() != Dict {
		return 0
	}
	return int(desc.Key("WMode").Int64())
}

func (f *Font) charmapEncoding() TextEncoding {
	if enc := f.cmapEncodingFromValue(f.V.Key("ToUnicode")); enc != nil {
		return enc
	}
	return &byteEncoder{&pdfDocEncoding}
}

type dictEncoder struct {
	v Value
}

func (e *dictEncoder) Decode(raw string) (text string) {
	r := make([]rune, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		ch := rune(raw[i])
		n := -1
		for j := 0; j < e.v.Len(); j++ {
			x := e.v.Index(j)
			if x.Kind() == Integer {
				n = int(x.Int64())
				continue
			}
			if x.Kind() == Name {
				if int(raw[i]) == n {
					r := nameToRune[x.Name()]
					if r != 0 {
						ch = r
						break
					}
				}
				n++
			}
		}
		r = append(r, ch)
	}
	return string(r)
}

// A TextEncoding represents a mapping between
// font code points and UTF-8 text.
type TextEncoding interface {
	// Decode returns the UTF-8 text corresponding to
	// the sequence of code points in raw.
	Decode(raw string) (text string)
}

type nopEncoder struct {
}

func (e *nopEncoder) Decode(raw string) (text string) {
	return raw
}

type byteEncoder struct {
	table *[256]rune
}

func (e *byteEncoder) Decode(raw string) (text string) {
	r := make([]rune, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		r = append(r, e.table[raw[i]])
	}
	return string(r)
}

type byteRange struct {
	low  string
	high string
}

type bfchar struct {
	orig string
	repl string
}

type bfrange struct {
	lo  string
	hi  string
	dst Value
}

type cmap struct {
	space   [4][]byteRange // codespace range
	bfrange []bfrange
	bfchar  []bfchar
	use     TextEncoding
}

var cmapRegistry sync.Map

func registerCMap(name string, enc TextEncoding) {
	if name == "" || enc == nil {
		return
	}
	cmapRegistry.Store(name, enc)
}

func lookupCMap(name string) TextEncoding {
	if name == "" {
		return nil
	}
	if v, ok := cmapRegistry.Load(name); ok {
		if enc, ok := v.(TextEncoding); ok {
			return enc
		}
	}
	return nil
}

func (m *cmap) Decode(raw string) (text string) {
	var r []rune
Parse:
	for len(raw) > 0 {
		for n := 1; n <= 4 && n <= len(raw); n++ { // number of digits in character replacement (1-4 possible)
			for _, space := range m.space[n-1] { // find matching codespace Ranges for number of digits
				if space.low <= raw[:n] && raw[:n] <= space.high { // see if value is in range
					text := raw[:n]
					raw = raw[n:]
					for _, bfchar := range m.bfchar { // check for matching bfchar
						if len(bfchar.orig) == n && bfchar.orig == text {
							r = append(r, []rune(utf16Decode(bfchar.repl))...)
							continue Parse
						}
					}
					for _, bfrange := range m.bfrange { // check for matching bfrange
						if len(bfrange.lo) == n && bfrange.lo <= text && text <= bfrange.hi {
							if bfrange.dst.Kind() == String {
								s := bfrange.dst.RawString()
								if bfrange.lo != text { // value isn't at the beginning of the range so scale result
									b := []byte(s)
									b[len(b)-1] += text[len(text)-1] - bfrange.lo[len(bfrange.lo)-1] // increment last byte by difference
									s = string(b)
								}
								r = append(r, []rune(utf16Decode(s))...)
								continue Parse
							}
							if bfrange.dst.Kind() == Array {
								n := text[len(text)-1] - bfrange.lo[len(bfrange.lo)-1]
								v := bfrange.dst.Index(int(n))
								if v.Kind() == String {
									s := v.RawString()
									r = append(r, []rune(utf16Decode(s))...)
									continue Parse
								}
								if DebugOn {
									fmt.Printf("array %v\n", bfrange.dst)
								}
							} else {
								if DebugOn {
									fmt.Printf("unknown dst %v\n", bfrange.dst)
								}
							}
							r = append(r, noRune)
							continue Parse
						}
					}
					if m.use != nil {
						if out := m.use.Decode(text); out != "" {
							r = append(r, []rune(out)...)
							continue Parse
						}
					}
					r = append(r, noRune)
					continue Parse
				}
			}
		}
		if DebugOn {
			println("no code space found")
		}
		r = append(r, noRune)
		raw = raw[1:]
	}
	return string(r)
}

func readCmap(toUnicode Value) *cmap {
	return readCmapWithContext(context.Background(), toUnicode)
}

// readCmapWithContext reads a cmap with context cancellation support.
// If ctx is nil, it uses context.Background().
func readCmapWithContext(ctx context.Context, toUnicode Value) *cmap {
	if ctx == nil {
		ctx = context.Background()
	}
	n := -1
	var m cmap
	ok := true
	var cmapName string
	InterpretWithContext(ctx, toUnicode, func(stk *Stack, op string) {
		if !ok {
			return
		}
		switch op {
		case "findresource":
			stk.Pop() // category
			stk.Pop() // key
			stk.Push(newDict())
		case "begincmap":
			stk.Push(newDict())
		case "endcmap":
			stk.Pop()
		case "begincodespacerange":
			n = int(stk.Pop().Int64())
		case "endcodespacerange":
			if n < 0 {
				if DebugOn {
					println("missing begincodespacerange")
				}
				ok = false
				return
			}
			for i := 0; i < n; i++ {
				hi, lo := stk.Pop().RawString(), stk.Pop().RawString()
				if len(lo) == 0 || len(lo) != len(hi) {
					if DebugOn {
						println("bad codespace range")
					}
					ok = false
					return
				}
				m.space[len(lo)-1] = append(m.space[len(lo)-1], byteRange{lo, hi})
			}
			n = -1
		case "beginbfchar":
			n = int(stk.Pop().Int64())
		case "endbfchar":
			if n < 0 {
				if DebugOn {
					println("missing beginbfchar")
				}
				ok = false
				return
			}
			for i := 0; i < n; i++ {
				repl, orig := stk.Pop().RawString(), stk.Pop().RawString()
				m.bfchar = append(m.bfchar, bfchar{orig, repl})
			}
		case "beginbfrange":
			n = int(stk.Pop().Int64())
		case "endbfrange":
			if n < 0 {
				if DebugOn {
					println("missing beginbfrange")
				}
				ok = false
				return
			}
			for i := 0; i < n; i++ {
				dst, srcHi, srcLo := stk.Pop(), stk.Pop().RawString(), stk.Pop().RawString()
				m.bfrange = append(m.bfrange, bfrange{srcLo, srcHi, dst})
			}
		case "usecmap":
			base := stk.Pop()
			name := base.Name()
			if name == "" {
				name = base.Text()
			}
			if name == "" {
				break
			}
			if enc := builtinCMapEncoding(name); enc != nil {
				m.use = enc
			} else if enc := lookupCMap(name); enc != nil {
				m.use = enc
			} else if DebugOn {
				fmt.Printf("unknown usecmap %s\n", name)
			}
		case "defineresource":
			category := stk.Pop().Name()
			value := stk.Pop()
			key := stk.Pop().Name()
			if category == "CMap" && key != "" {
				cmapName = key
			}
			stk.Push(value)
		default:
			if DebugOn {
				println("interp\t", op)
			}
		}
	})
	if !ok {
		return nil
	}
	if cmapName != "" {
		registerCMap(cmapName, &m)
	}
	return &m
}

type matrix [3][3]float64

var ident = matrix{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}

func (x matrix) mul(y matrix) matrix {
	var z matrix
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 3; k++ {
				z[i][j] += x[i][k] * y[k][j]
			}
		}
	}
	return z
}

func matrixFromValue(v Value) (matrix, bool) {
	if v.Kind() != Array || v.Len() != 6 {
		return matrix{}, false
	}
	var m matrix
	for i := 0; i < 6; i++ {
		m[i/2][i%2] = v.Index(i).Float64()
	}
	m[2][2] = 1
	return m, true
}

func applyMatrixToPoint(m matrix, x, y float64) (float64, float64) {
	px := x*m[0][0] + y*m[1][0] + m[2][0]
	py := x*m[0][1] + y*m[1][1] + m[2][1]
	return px, py
}

// A Text represents a single piece of text drawn on a page.
type Text struct {
	Font      string  // the font used
	FontSize  float64 // the font size, in points (1/72 of an inch)
	X         float64 // the X coordinate, in points, increasing left to right
	Y         float64 // the Y coordinate, in points, increasing bottom to top
	W         float64 // the width of the text, in points
	S         string  // the actual UTF-8 text
	Vertical  bool    // whether the text is drawn vertically
	Bold      bool    // whether the text is bold
	Italic    bool    // whether the text is italic
	Underline bool    // whether the text is underlined
}

// A Rect represents a rectangle.
type Rect struct {
	Min, Max Point
}

// A Point represents an X, Y pair.
type Point struct {
	X float64
	Y float64
}

// Content describes the basic content on a page: the text and any drawn rectangles.
type Content struct {
	Text []Text
	Rect []Rect
}

type gstate struct {
	Tc    float64
	Tw    float64
	Th    float64
	Tl    float64
	Tf    Font
	Tfs   float64
	Tmode int
	Trise float64
	Tm    matrix
	Tlm   matrix
	Trm   matrix
	CTM   matrix
}

// GetPlainText returns the page's all text without format.
// fonts can be passed in (to improve parsing performance) or left nil
// ctx can be used to cancel the extraction operation (pass context.Background() if not needed)
func (p Page) GetPlainText(ctx context.Context, fonts map[string]*Font) (string, error) {
	// Check if context is cancelled before starting expensive operation
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}

	// Handle in case the content page is empty
	if p.V.IsNull() || p.V.Key("Contents").Kind() == Null {
		return "", nil
	}

	content, err := p.contentWithFonts(fonts)
	if err != nil {
		return "", wrapError("extract page content", err)
	}

	text := textRunsToPlain(content.Text)

	// CRITICAL FIX: Clear fontCache reference after extraction to prevent memory leak.
	// Without this, each Page retains the entire fontCache indefinitely, causing
	// memory to grow from 400MB to 20-40GB when processing large batches.
	p.fontCache = nil

	return text, nil
}

// GetPlainTextWithSmartOrdering extracts plain text using an improved text ordering algorithm
// that handles multi-column layouts and complex reading orders.
// ctx can be used to cancel the extraction operation (pass context.Background() if not needed)
func (p Page) GetPlainTextWithSmartOrdering(ctx context.Context, fonts map[string]*Font) (string, error) {
	// Check if context is cancelled before starting expensive operation
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}

	// Handle in case the content page is empty
	if p.V.IsNull() || p.V.Key("Contents").Kind() == Null {
		return "", nil
	}

	content, err := p.contentWithFonts(fonts)
	if err != nil {
		return "", wrapError("extract page content", err)
	}

	text := SmartTextRunsToPlain(content.Text)

	// CRITICAL FIX: Clear fontCache reference after extraction to prevent memory leak
	p.fontCache = nil

	return text, nil
}

// GetPlainTextConcurrent extracts all pages concurrently using the specified number of workers.
func (r *Reader) GetPlainTextConcurrent(workers int) (io.Reader, error) {
	pages := r.NumPage()
	if pages == 0 {
		return &bytes.Buffer{}, nil
	}
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers > pages {
		workers = pages
	}
	results := make([]string, pages)
	jobs := make(chan int)
	errCh := make(chan error, 1)
	done := make(chan struct{})
	var once sync.Once
	cancel := func(err error) {
		once.Do(func() {
			if err != nil {
				errCh <- err
			}
			close(done)
		})
	}

	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for pageNum := range jobs {
			select {
			case <-done:
				return
			default:
			}
			page := r.Page(pageNum)
			text, err := page.GetPlainText(context.Background(), nil)
			if err != nil {
				cancel(err)
				return
			}
			results[pageNum-1] = text

			// CRITICAL FIX: Cleanup page resources after extraction
			page.Cleanup()
		}
	}

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go worker()
	}

pagesLoop:
	for i := 1; i <= pages; i++ {
		select {
		case <-done:
			break pagesLoop
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	var buf bytes.Buffer
	for _, text := range results {
		buf.WriteString(text)
	}
	return &buf, nil
}

func textRunsToPlain(texts []Text) string {
	if len(texts) == 0 {
		return ""
	}

	// work on a copy so callers of Content() are not affected by ordering changes
	runs := append([]Text(nil), texts...)
	sort.Sort(TextVertical(runs))

	const lineTolerance = 2.0
	var lines [][]Text
	var currentLine []Text
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
		currentLine = []Text{t}
		currentCoord = lineCoord
	}

	if len(currentLine) > 0 {
		sort.Slice(currentLine, func(i, j int) bool {
			return effectiveOrderCoord(currentLine[i]) < effectiveOrderCoord(currentLine[j])
		})
		lines = append(lines, currentLine)
	}

	// Use zero-copy StringBuffer to improve performance
	totalLen := 0
	for _, line := range lines {
		for _, t := range line {
			totalLen += len(t.S) + 1 // +1 for potential space
		}
		totalLen++ // for newline
	}

	builder := NewStringBuffer(totalLen)
	for i, line := range lines {
		appendLineZC(builder, line)
		if i < len(lines)-1 {
			builder.WriteByte('\n')
		}
	}
	result := builder.String()
	return TrimSpaceZeroCopy(result)
}

// appendLineZC zero-copy version of appendLine
func appendLineZC(builder *StringBuffer, line []Text) {
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
			prevEnd = t.Y
		} else {
			prevEnd = t.X + t.W
		}
		hasPrev = true
	}
}

func appendLine(builder *strings.Builder, line []Text) {
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
			prevEnd = t.Y
		} else {
			prevEnd = t.X + t.W
		}
		hasPrev = true
	}
}

//go:inline
func effectiveLineCoord(t Text) float64 {
	if t.Vertical {
		return t.X
	}
	return t.Y
}

//go:inline
func effectiveOrderCoord(t Text) float64 {
	if t.Vertical {
		return -t.Y
	}
	return t.X
}

// Column represents the contents of a column
type Column struct {
	Position int64
	Content  TextVertical
}

// Columns is a list of column
type Columns []*Column

// GetTextByColumn returns the page's all text grouped by column
func (p Page) GetTextByColumn() (Columns, error) {
	var result Columns
	var err error

	defer func() {
		if r := recover(); r != nil {
			result = Columns{}
			err = wrapError("extract text by column", fmt.Errorf("%v", r))
		}
	}()

	showText := func(enc TextEncoding, currentX, currentY float64, s string) {
		var textBuilder bytes.Buffer

		for _, ch := range enc.Decode(s) {
			_, err := textBuilder.WriteRune(ch)
			if err != nil {
				panic(err)
			}
		}
		text := Text{
			S: textBuilder.String(),
			X: currentX,
			Y: currentY,
		}

		var currentColumn *Column
		columnFound := false
		for _, column := range result {
			if int64(currentX) == column.Position {
				currentColumn = column
				columnFound = true
				break
			}
		}

		if !columnFound {
			currentColumn = &Column{
				Position: int64(currentX),
				Content:  TextVertical{},
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

// Row represents the contents of a row
type Row struct {
	Position int64
	Content  TextHorizontal
}

// Rows is a list of rows
type Rows []*Row

// GetTextByRow returns the page's all text grouped by rows
func (p Page) GetTextByRow() (Rows, error) {
	var result Rows
	var err error

	defer func() {
		if r := recover(); r != nil {
			result = Rows{}
			err = wrapError("extract text by row", fmt.Errorf("%v", r))
		}
	}()

	showText := func(enc TextEncoding, currentX, currentY float64, s string) {
		var textBuilder bytes.Buffer
		for _, ch := range enc.Decode(s) {
			_, err := textBuilder.WriteRune(ch)
			if err != nil {
				panic(err)
			}
		}

		// if DebugOn {
		// 	fmt.Println(textBuilder.String())
		// }

		text := Text{
			S: textBuilder.String(),
			X: currentX,
			Y: currentY,
		}

		var currentRow *Row
		rowFound := false
		for _, row := range result {
			if int64(currentY) == row.Position {
				currentRow = row
				rowFound = true
				break
			}
		}

		if !rowFound {
			currentRow = &Row{
				Position: int64(currentY),
				Content:  TextHorizontal{},
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

func (p Page) walkTextBlocks(walker func(enc TextEncoding, x, y float64, s string)) {
	// Handle in case the content page is empty
	if p.V.IsNull() || p.V.Key("Contents").Kind() == Null {
		return
	}

	scope := p.buildFontScope(p.Resources(), nil, nil)
	processor := textProcessor{
		page:   p,
		walker: walker,
	}
	processor.process(p.V.Key("Contents"), p.Resources(), scope, ident)
}

type textProcessor struct {
	page   Page
	walker func(enc TextEncoding, x, y float64, s string)
}

func (tp *textProcessor) process(strm Value, resources Value, scope *fontScope, ctm matrix) {
	if strm.Kind() == Null {
		return
	}
	var enc TextEncoding = &nopEncoder{}
	var currentX, currentY float64
	Interpret(strm, func(stk *Stack, op string) {
		n := stk.Len()
		args := make([]Value, n)
		for i := n - 1; i >= 0; i-- {
			args[i] = stk.Pop()
		}
		switch op {
		default:
			return
		case "T*": // move to start of next line
		case "Tf":
			if len(args) != 2 {
				panic("bad Tf operator")
			}
			if font := scope.Get(args[0].Name()); font != nil {
				enc = font.Encoder()
				if enc == nil {
					enc = &nopEncoder{}
				}
			} else {
				enc = &nopEncoder{}
			}
		case "\"":
			if len(args) != 3 {
				panic("bad \\\" operator")
			}
			fallthrough
		case "'":
			if len(args) != 1 {
				panic("bad ' operator")
			}
			fallthrough
		case "Tj":
			if len(args) != 1 {
				panic("bad Tj operator")
			}
			tp.emit(enc, currentX, currentY, args[0].RawString(), ctm)
		case "TJ":
			v := args[0]
			for i := 0; i < v.Len(); i++ {
				x := v.Index(i)
				if x.Kind() == String {
					tp.emit(enc, currentX, currentY, x.RawString(), ctm)
				}
			}
		case "Td":
			tp.emit(enc, currentX, currentY, "", ctm)
		case "Tm":
			if len(args) != 6 {
				panic("bad Tm operator")
			}
			currentX = args[4].Float64()
			currentY = args[5].Float64()
		case "Do":
			if len(args) != 1 {
				panic("bad Do operator")
			}
			tp.handleDo(args[0], resources, scope, ctm)
		}
	})
}

func (tp *textProcessor) emit(enc TextEncoding, x, y float64, raw string, ctm matrix) {
	if tp.walker == nil {
		return
	}
	tx, ty := applyMatrixToPoint(ctm, x, y)
	tp.walker(enc, tx, ty, raw)
}

func (tp *textProcessor) handleDo(arg Value, resources Value, scope *fontScope, ctm matrix) {
	name := arg.Name()
	if name == "" {
		return
	}
	xobjects := resources.Key("XObject")
	if xobjects.Kind() != Dict {
		return
	}
	xobj := xobjects.Key(name)
	if xobj.Kind() != Stream || xobj.Key("Subtype").Name() != "Form" {
		return
	}
	formRes := xobj.Key("Resources")
	if formRes.Kind() == Null {
		formRes = resources
	}
	childScope := tp.page.buildFontScope(formRes, nil, scope)
	childCTM := ctm
	if m, ok := matrixFromValue(xobj.Key("Matrix")); ok {
		childCTM = m.mul(childCTM)
	}
	tp.process(xobj, formRes, childScope, childCTM)
}

// Content returns the page's content.
func (p Page) Content() Content {
	content, _ := p.contentWithFonts(nil)
	return content
}

func (p Page) contentWithFonts(fonts map[string]*Font) (Content, error) {
	var content Content
	var err error
	var scope *fontScope

	// Recover from panics in content stream processing and convert to errors
	defer func() {
		if r := recover(); r != nil {
			content = Content{}
			err = wrapError("process content stream", fmt.Errorf("%v", r))
		}
		// CRITICAL FIX: Clear scope references to break potential circular references
		// and allow GC to reclaim font objects. This prevents accumulation of Font
		// objects across multiple page extractions.
		if scope != nil {
			scope.fonts = nil
			scope.parent = nil
		}
	}()

	// Handle in case the content page is empty
	if p.V.IsNull() || p.V.Key("Contents").Kind() == Null {
		return Content{}, nil
	}

	// Use pooled slices to reduce allocations in appendText
	textSlice, rectSlice := GetContentExtractorSlices()
	extractor := contentExtractor{page: p, text: textSlice, rect: rectSlice}
	scope = p.buildFontScope(p.Resources(), fonts, nil)
	initial := gstate{
		Th:  1,
		CTM: ident,
	}
	extractor.process(p.V.Key("Contents"), p.Resources(), scope, initial)
	content = Content{extractor.text, extractor.rect}
	// Note: we don't return slices to pool here because they're now owned by Content
	// The caller should call PutContentExtractorSlices when done if needed
	return content, err
}

type contentExtractor struct {
	page     Page
	text     []Text
	rect     []Rect
	textCap  int // Track capacity to avoid frequent reallocations
	growHint int // Hint for next growth size
}

func (ce *contentExtractor) process(strm Value, resources Value, scope *fontScope, initial gstate) {
	if strm.Kind() == Null {
		return
	}
	g := initial
	var enc TextEncoding = &nopEncoder{}
	var gstack []gstate
	Interpret(strm, func(stk *Stack, op string) {
		n := stk.Len()
		args := make([]Value, n)
		for i := n - 1; i >= 0; i-- {
			args[i] = stk.Pop()
		}
		switch op {
		default:
			return

		case "cm":
			if len(args) != 6 {
				panic("bad cm operator")
			}
			var m matrix
			for i := 0; i < 6; i++ {
				m[i/2][i%2] = args[i].Float64()
			}
			m[2][2] = 1
			g.CTM = m.mul(g.CTM)

		case "re":
			if len(args) != 4 {
				panic("bad re")
			}
			x, y, w, h := args[0].Float64(), args[1].Float64(), args[2].Float64(), args[3].Float64()
			ce.rect = append(ce.rect, Rect{Point{x, y}, Point{x + w, y + h}})

		case "q":
			gstack = append(gstack, g)

		case "Q":
			if len(gstack) == 0 {
				return
			}
			g = gstack[len(gstack)-1]
			gstack = gstack[:len(gstack)-1]

		case "BT":
			g.Tm = ident
			g.Tlm = g.Tm

		case "ET":
		case "T*":
			x := matrix{{1, 0, 0}, {0, 1, 0}, {0, -g.Tl, 1}}
			g.Tlm = x.mul(g.Tlm)
			g.Tm = g.Tlm

		case "Tc":
			if len(args) != 1 {
				panic("bad Tc")
			}
			g.Tc = args[0].Float64()

		case "TD":
			if len(args) != 2 {
				panic("bad TD")
			}
			g.Tl = -args[1].Float64()
			fallthrough
		case "Td":
			if len(args) != 2 {
				panic("bad Td")
			}
			tx := args[0].Float64()
			ty := args[1].Float64()
			x := matrix{{1, 0, 0}, {0, 1, 0}, {tx, ty, 1}}
			g.Tlm = x.mul(g.Tlm)
			g.Tm = g.Tlm

		case "Tf":
			if len(args) != 2 {
				panic("bad Tf")
			}
			name := args[0].Name()
			if font := scope.Get(name); font != nil {
				g.Tf = *font
				enc = g.Tf.Encoder()
				if enc == nil {
					enc = &nopEncoder{}
				}
			} else {
				g.Tf = Font{}
				enc = &nopEncoder{}
			}
			g.Tfs = args[1].Float64()

		case "\"":
			if len(args) != 3 {
				panic("bad \\\" operator")
			}
			g.Tw = args[0].Float64()
			g.Tc = args[1].Float64()
			args = args[2:]
			fallthrough
		case "'":
			if len(args) != 1 {
				panic("bad ' operator")
			}
			x := matrix{{1, 0, 0}, {0, 1, 0}, {0, -g.Tl, 1}}
			g.Tlm = x.mul(g.Tlm)
			g.Tm = g.Tlm
			fallthrough
		case "Tj":
			if len(args) != 1 {
				panic("bad Tj operator")
			}
			ce.appendText(&g, enc, args[0].RawString())

		case "TJ":
			v := args[0]
			for i := 0; i < v.Len(); i++ {
				x := v.Index(i)
				if x.Kind() == String {
					ce.appendText(&g, enc, x.RawString())
				} else {
					tx := -x.Float64() / 1000 * g.Tfs * g.Th
					g.Tm = matrix{{1, 0, 0}, {0, 1, 0}, {tx, 0, 1}}.mul(g.Tm)
				}
			}
			ce.appendText(&g, enc, "\n")

		case "TL":
			if len(args) != 1 {
				panic("bad TL")
			}
			g.Tl = args[0].Float64()

		case "Tm":
			if len(args) != 6 {
				panic("bad Tm")
			}
			var m matrix
			for i := 0; i < 6; i++ {
				m[i/2][i%2] = args[i].Float64()
			}
			m[2][2] = 1
			g.Tm = m
			g.Tlm = m

		case "Tr":
			if len(args) != 1 {
				panic("bad Tr")
			}
			g.Tmode = int(args[0].Int64())

		case "Ts":
			if len(args) != 1 {
				panic("bad Ts")
			}
			g.Trise = args[0].Float64()

		case "Tw":
			if len(args) != 1 {
				panic("bad Tw")
			}
			g.Tw = args[0].Float64()

		case "Tz":
			if len(args) != 1 {
				panic("bad Tz")
			}
			g.Th = args[0].Float64() / 100

		case "Do":
			if len(args) != 1 {
				panic("bad Do")
			}
			ce.handleDo(args[0], resources, scope, g)
		}
	})
}

func (ce *contentExtractor) appendText(g *gstate, enc TextEncoding, s string) {
	if enc == nil {
		enc = &nopEncoder{}
	}

	decoded := enc.Decode(s)
	decodedLen := len(decoded)
	if decodedLen == 0 {
		return
	}

	vertical := g.Tf.writingMode() == 1

	// Aggressive pre-allocation strategy to minimize reallocations
	oldLen := len(ce.text)
	newLen := oldLen + decodedLen

	// Only reallocate if necessary
	if cap(ce.text) < newLen {
		// Use adaptive growth strategy based on usage patterns
		// For first allocation or small slices: allocate generously
		// For large slices: grow by 50% + needed space
		var newCap int
		if oldLen < 100 {
			// Small slice: allocate at least 512 to avoid early reallocations
			newCap = 512
			if newLen > newCap {
				newCap = newLen * 2
			}
		} else if oldLen < 10000 {
			// Medium slice: 50% growth
			newCap = oldLen + oldLen/2 + decodedLen
		} else {
			// Large slice: 25% growth to save memory
			newCap = oldLen + oldLen/4 + decodedLen
		}

		// Use hint from previous growth if available
		if ce.growHint > 0 && newCap < ce.growHint {
			newCap = ce.growHint
		}

		// Allocate new slice - copy is unavoidable but minimize frequency
		newText := make([]Text, oldLen, newCap)
		copy(newText, ce.text)
		ce.text = newText
		ce.textCap = newCap

		// Update growth hint for next time
		ce.growHint = newCap + decodedLen*2
	}

	// Extend slice to final size - avoids repeated append overhead
	ce.text = ce.text[:newLen]

	// Pre-compute common values outside loop
	f := g.Tf.BaseFont()
	if i := strings.Index(f, "+"); i >= 0 {
		f = f[i+1:]
	}
	bold, italic, underline := parseFontStyles(f)

	// Pre-compute base transformation matrix components
	// Trm = matrix{{g.Tfs * g.Th, 0, 0}, {0, g.Tfs, 0}, {0, g.Trise, 1}}.mul(g.Tm).mul(g.CTM)
	// Pre-compute the constant part: textMatrix = {{g.Tfs * g.Th, 0, 0}, {0, g.Tfs, 0}, {0, g.Trise, 1}}
	tfsth := g.Tfs * g.Th
	trise := g.Trise

	// Cache CTM values for faster access
	ctm := g.CTM

	// Batch processing: fill slice directly instead of append
	n := 0
	for i, ch := range decoded {
		var w0 float64
		if n < len(s) {
			w0 = g.Tf.Width(int(s[n]))
		}
		n++

		// Inline matrix multiplication to avoid function call overhead
		// Trm = textMatrix.mul(g.Tm).mul(g.CTM)
		tm := g.Tm
		// First: textMatrix.mul(tm)
		// textMatrix is {{tfsth, 0, 0}, {0, tfs, 0}, {0, trise, 1}}
		// Row 0: tfsth * tm[0]
		temp00 := tfsth * tm[0][0]
		temp01 := tfsth * tm[0][1]
		temp02 := tfsth * tm[0][2]
		// Row 2: trise * tm[1] + tm[2]
		temp20 := trise*tm[1][0] + tm[2][0]
		temp21 := trise*tm[1][1] + tm[2][1]
		temp22 := trise*tm[1][2] + tm[2][2]

		// Second: result.mul(ctm)
		trm00 := temp00*ctm[0][0] + temp01*ctm[1][0] + temp02*ctm[2][0]
		trm20 := temp20*ctm[0][0] + temp21*ctm[1][0] + temp22*ctm[2][0]
		trm21 := temp20*ctm[0][1] + temp21*ctm[1][1] + temp22*ctm[2][1]

		// Direct assignment instead of append - no reallocation
		ce.text[oldLen+i] = Text{
			f,
			trm00,
			trm20,
			trm21,
			w0 / 1000 * trm00,
			InternRune(ch),
			vertical,
			bold,
			italic,
			underline,
		}

		tx := w0/1000*g.Tfs + g.Tc
		tx *= g.Th
		// Update g.Tm inline: g.Tm = matrix{{1, 0, 0}, {0, 1, 0}, {tx, 0, 1}}.mul(g.Tm)
		g.Tm[2][0] += tx * g.Tm[0][0]
		g.Tm[2][1] += tx * g.Tm[0][1]
		g.Tm[2][2] += tx * g.Tm[0][2]
	}
}

func (ce *contentExtractor) handleDo(arg Value, resources Value, scope *fontScope, g gstate) {
	name := arg.Name()
	if name == "" {
		return
	}
	xobjects := resources.Key("XObject")
	if xobjects.Kind() != Dict {
		return
	}
	xobj := xobjects.Key(name)
	if xobj.Kind() != Stream || xobj.Key("Subtype").Name() != "Form" {
		return
	}
	formRes := xobj.Key("Resources")
	if formRes.Kind() == Null {
		formRes = resources
	}
	childScope := ce.page.buildFontScope(formRes, nil, scope)
	childState := g
	if m, ok := matrixFromValue(xobj.Key("Matrix")); ok {
		childState.CTM = m.mul(childState.CTM)
	}
	ce.process(xobj, formRes, childScope, childState)
}

// TextVertical implements sort.Interface for sorting
// a slice of Text values in vertical order, top to bottom,
// and then left to right within a line.
type TextVertical []Text

func (x TextVertical) Len() int      { return len(x) }
func (x TextVertical) Swap(i, j int) { x[i], x[j] = x[j], x[i] }
func (x TextVertical) Less(i, j int) bool {
	if x[i].Y != x[j].Y {
		return x[i].Y > x[j].Y
	}
	return x[i].X < x[j].X
}

// TextHorizontal implements sort.Interface for sorting
// a slice of Text values in horizontal order, left to right,
// and then top to bottom within a column.
type TextHorizontal []Text

func (x TextHorizontal) Len() int      { return len(x) }
func (x TextHorizontal) Swap(i, j int) { x[i], x[j] = x[j], x[i] }
func (x TextHorizontal) Less(i, j int) bool {
	if x[i].X != x[j].X {
		return x[i].X < x[j].X
	}
	return x[i].Y > x[j].Y
}

// An Outline is a tree describing the outline (also known as the table of contents)
// of a document.
type Outline struct {
	Title string    // title for this element
	Child []Outline // child elements
}

// Outline returns the document outline.
// The Outline returned is the root of the outline tree and typically has no Title itself.
// That is, the children of the returned root are the top-level entries in the outline.
func (r *Reader) Outline() Outline {
	return buildOutline(r.Trailer().Key("Root").Key("Outlines"))
}

func buildOutline(entry Value) Outline {
	var x Outline
	x.Title = entry.Key("Title").Text()
	for child := entry.Key("First"); child.Kind() == Dict; child = child.Key("Next") {
		x.Child = append(x.Child, buildOutline(child))
	}
	return x
}
