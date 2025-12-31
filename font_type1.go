// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"bytes"
	"io"
	"strconv"
	"strings"
)

// Type1FontInfo contains parsed Type1 font header information
type Type1FontInfo struct {
	FontName       string
	FullName       string
	FamilyName     string
	Weight         string
	ItalicAngle    float64
	IsFixedPitch   bool
	UnderlinePos   float64
	UnderlineThick float64
	FontBBox       [4]float64
	UniqueID       int
	XUID           []int

	// Font metrics
	Encoding    string
	PaintType   int
	FontType    int
	FontMatrix  [6]float64
	StrokeWidth float64

	// Private dict values
	BlueValues       []int
	OtherBlues       []int
	FamilyBlues      []int
	FamilyOtherBlues []int
	BlueScale        float64
	BlueShift        int
	BlueFuzz         int
	StdHW            float64
	StdVW            float64
	StemSnapH        []float64
	StemSnapV        []float64
	ForceBold        bool
	LanguageGroup    int
	RndStemUp        bool
	ExpansionFactor  float64
}

// Type1Font represents a Type1 font
type Type1Font struct {
	info        *Type1FontInfo
	charStrings map[string][]byte
	subrs       [][]byte
	encoding    map[byte]string
	widths      map[string]float64
}

// NewType1Font creates a new Type1 font from raw font data with caching
func NewType1Font(data []byte) (*Type1Font, error) {
	// Try to get from cache first
	cache := GetGlobalType1Cache()
	if cachedFont, found := cache.GetFont(data); found {
		return cachedFont, nil
	}

	// Parse the font
	font := &Type1Font{
		info:        &Type1FontInfo{},
		charStrings: make(map[string][]byte),
		encoding:    make(map[byte]string),
		widths:      make(map[string]float64),
	}

	if err := font.parse(data); err != nil {
		return nil, err
	}

	// Cache the parsed font
	cache.PutFont(data, font)

	return font, nil
}

// Info returns the font info
func (f *Type1Font) Info() *Type1FontInfo {
	return f.info
}

// GlyphWidth returns the width of a glyph by name
func (f *Type1Font) GlyphWidth(name string) float64 {
	if w, ok := f.widths[name]; ok {
		return w
	}
	return 0
}

// GlyphName returns the glyph name for a character code
func (f *Type1Font) GlyphName(code byte) string {
	if name, ok := f.encoding[code]; ok {
		return name
	}
	return ".notdef"
}

func (f *Type1Font) parse(data []byte) error {
	// Type1 fonts have three segments:
	// 1. ASCII header (clear text)
	// 2. Encrypted data (binary or eexec encoded)
	// 3. ASCII trailer (fixed zeros)

	// Find the eexec marker
	eexecIdx := bytes.Index(data, []byte("eexec"))
	if eexecIdx == -1 {
		// No encryption, parse as plain text
		return f.parseASCII(data)
	}

	// Parse the ASCII header
	if err := f.parseASCII(data[:eexecIdx]); err != nil {
		return err
	}

	// Find the start of encrypted data (after eexec and whitespace)
	encStart := eexecIdx + 5
	for encStart < len(data) && (data[encStart] == ' ' || data[encStart] == '\n' || data[encStart] == '\r' || data[encStart] == '\t') {
		encStart++
	}

	// Find the end of encrypted data (cleartomark or 512 zeros)
	encEnd := bytes.Index(data[encStart:], []byte("cleartomark"))
	if encEnd == -1 {
		encEnd = len(data) - encStart
	}

	// Decrypt the eexec data
	encData := data[encStart : encStart+encEnd]

	// Check if it's hex-encoded or binary
	if isHexEncoded(encData) {
		decData, err := decodeHex(encData)
		if err != nil {
			return err
		}
		encData = decData
	}

	// Decrypt with eexec cipher
	decrypted := eexecDecrypt(encData)

	// Parse the decrypted data
	return f.parsePrivate(decrypted)
}

func (f *Type1Font) parseASCII(data []byte) error {
	lines := bytes.Split(data, []byte{'\n'})

	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] == '%' {
			continue
		}

		// Parse key-value pairs
		if bytes.HasPrefix(line, []byte("/FontName")) {
			f.info.FontName = extractName(line)
		} else if bytes.HasPrefix(line, []byte("/FullName")) {
			f.info.FullName = extractString(line)
		} else if bytes.HasPrefix(line, []byte("/FamilyName")) {
			f.info.FamilyName = extractString(line)
		} else if bytes.HasPrefix(line, []byte("/Weight")) {
			f.info.Weight = extractString(line)
		} else if bytes.HasPrefix(line, []byte("/ItalicAngle")) {
			f.info.ItalicAngle = extractFloat(line)
		} else if bytes.HasPrefix(line, []byte("/isFixedPitch")) {
			f.info.IsFixedPitch = extractBool(line)
		} else if bytes.HasPrefix(line, []byte("/UnderlinePosition")) {
			f.info.UnderlinePos = extractFloat(line)
		} else if bytes.HasPrefix(line, []byte("/UnderlineThickness")) {
			f.info.UnderlineThick = extractFloat(line)
		} else if bytes.HasPrefix(line, []byte("/FontBBox")) {
			f.info.FontBBox = extractBBox(line)
		} else if bytes.HasPrefix(line, []byte("/UniqueID")) {
			f.info.UniqueID = extractInt(line)
		} else if bytes.HasPrefix(line, []byte("/PaintType")) {
			f.info.PaintType = extractInt(line)
		} else if bytes.HasPrefix(line, []byte("/FontType")) {
			f.info.FontType = extractInt(line)
		} else if bytes.HasPrefix(line, []byte("/FontMatrix")) {
			f.info.FontMatrix = extractMatrix(line)
		} else if bytes.HasPrefix(line, []byte("/StrokeWidth")) {
			f.info.StrokeWidth = extractFloat(line)
		} else if bytes.HasPrefix(line, []byte("/Encoding")) {
			// Handle encoding array
			f.parseEncoding(data)
		}
	}

	return nil
}

func (f *Type1Font) parsePrivate(data []byte) error {
	// Skip the random bytes at the start (4 bytes for eexec)
	if len(data) > 4 {
		data = data[4:]
	}

	// Parse Private dictionary values
	lines := bytes.Split(data, []byte{'\n'})

	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		if bytes.HasPrefix(line, []byte("/BlueValues")) {
			f.info.BlueValues = extractIntArray(line)
		} else if bytes.HasPrefix(line, []byte("/OtherBlues")) {
			f.info.OtherBlues = extractIntArray(line)
		} else if bytes.HasPrefix(line, []byte("/FamilyBlues")) {
			f.info.FamilyBlues = extractIntArray(line)
		} else if bytes.HasPrefix(line, []byte("/FamilyOtherBlues")) {
			f.info.FamilyOtherBlues = extractIntArray(line)
		} else if bytes.HasPrefix(line, []byte("/BlueScale")) {
			f.info.BlueScale = extractFloat(line)
		} else if bytes.HasPrefix(line, []byte("/BlueShift")) {
			f.info.BlueShift = extractInt(line)
		} else if bytes.HasPrefix(line, []byte("/BlueFuzz")) {
			f.info.BlueFuzz = extractInt(line)
		} else if bytes.HasPrefix(line, []byte("/StdHW")) {
			f.info.StdHW = extractFloat(line)
		} else if bytes.HasPrefix(line, []byte("/StdVW")) {
			f.info.StdVW = extractFloat(line)
		} else if bytes.HasPrefix(line, []byte("/ForceBold")) {
			f.info.ForceBold = extractBool(line)
		} else if bytes.HasPrefix(line, []byte("/LanguageGroup")) {
			f.info.LanguageGroup = extractInt(line)
		}
	}

	// Parse CharStrings
	f.parseCharStrings(data)

	// Parse Subrs
	f.parseSubrs(data)

	return nil
}

func (f *Type1Font) parseEncoding(data []byte) {
	// Find Encoding array
	start := bytes.Index(data, []byte("/Encoding"))
	if start == -1 {
		return
	}

	// Check if it's StandardEncoding
	if bytes.Contains(data[start:], []byte("StandardEncoding")) {
		f.info.Encoding = "StandardEncoding"
		// Use standard encoding
		for code, name := range standardEncoding {
			f.encoding[byte(code)] = name
		}
		return
	}

	// Parse custom encoding array
	arrayStart := bytes.Index(data[start:], []byte("dup"))
	if arrayStart == -1 {
		return
	}

	// Parse dup entries: dup <code> /<name> put
	rest := data[start+arrayStart:]
	for {
		dupIdx := bytes.Index(rest, []byte("dup "))
		if dupIdx == -1 {
			break
		}

		// Skip "dup "
		rest = rest[dupIdx+4:]

		// Parse code
		var code int
		_, err := parseNextInt(rest, &code)
		if err != nil {
			break
		}

		// Find glyph name
		nameStart := bytes.Index(rest, []byte("/"))
		if nameStart == -1 {
			break
		}
		nameEnd := bytes.IndexAny(rest[nameStart+1:], " \t\n\r")
		if nameEnd == -1 {
			break
		}

		name := string(rest[nameStart+1 : nameStart+1+nameEnd])
		f.encoding[byte(code)] = name

		rest = rest[nameStart+1+nameEnd:]
	}
}

func (f *Type1Font) parseCharStrings(data []byte) {
	// Find CharStrings dict
	start := bytes.Index(data, []byte("/CharStrings"))
	if start == -1 {
		return
	}

	// Find dict start
	dictStart := bytes.Index(data[start:], []byte("begin"))
	if dictStart == -1 {
		return
	}

	rest := data[start+dictStart+5:]

	// Parse each CharString: /<name> <len> RD <data> ND
	for {
		// Find next glyph name
		nameStart := bytes.Index(rest, []byte("/"))
		if nameStart == -1 {
			break
		}

		nameEnd := bytes.IndexAny(rest[nameStart+1:], " \t\n\r")
		if nameEnd == -1 {
			break
		}

		name := string(rest[nameStart+1 : nameStart+1+nameEnd])
		rest = rest[nameStart+1+nameEnd:]

		// Skip whitespace and get length
		rest = bytes.TrimLeft(rest, " \t\n\r")
		var length int
		n, err := parseNextInt(rest, &length)
		if err != nil {
			break
		}
		rest = rest[n:]

		// Skip to RD or -| and the space after
		rdIdx := bytes.Index(rest, []byte("RD "))
		if rdIdx == -1 {
			rdIdx = bytes.Index(rest, []byte("-| "))
		}
		if rdIdx == -1 {
			break
		}
		rest = rest[rdIdx+3:]

		// Extract CharString data
		if length > len(rest) {
			break
		}

		// Decrypt CharString data
		charString := charStringDecrypt(rest[:length])
		f.charStrings[name] = charString

		rest = rest[length:]

		// Check for end
		if bytes.HasPrefix(bytes.TrimLeft(rest, " \t\n\r"), []byte("end")) {
			break
		}
	}
}

func (f *Type1Font) parseSubrs(data []byte) {
	// Find Subrs array
	start := bytes.Index(data, []byte("/Subrs"))
	if start == -1 {
		return
	}

	// Get array size
	rest := data[start+6:]
	rest = bytes.TrimLeft(rest, " \t\n\r")

	var count int
	n, err := parseNextInt(rest, &count)
	if err != nil || count <= 0 {
		return
	}
	rest = rest[n:]

	f.subrs = make([][]byte, count)

	// Find array start
	arrayStart := bytes.Index(rest, []byte("dup"))
	if arrayStart == -1 {
		return
	}
	rest = rest[arrayStart:]

	// Parse each Subr: dup <index> <len> RD <data> NP
	for {
		dupIdx := bytes.Index(rest, []byte("dup "))
		if dupIdx == -1 {
			break
		}
		rest = rest[dupIdx+4:]

		// Parse index
		var index int
		n, err := parseNextInt(rest, &index)
		if err != nil {
			break
		}
		rest = rest[n:]

		// Parse length
		rest = bytes.TrimLeft(rest, " \t\n\r")
		var length int
		n, err = parseNextInt(rest, &length)
		if err != nil {
			break
		}
		rest = rest[n:]

		// Skip to RD or -|
		rdIdx := bytes.Index(rest, []byte("RD "))
		if rdIdx == -1 {
			rdIdx = bytes.Index(rest, []byte("-| "))
		}
		if rdIdx == -1 {
			break
		}
		rest = rest[rdIdx+3:]

		// Extract and decrypt Subr data
		if length > len(rest) {
			break
		}

		if index >= 0 && index < count {
			subr := charStringDecrypt(rest[:length])
			f.subrs[index] = subr
		}

		rest = rest[length:]
	}
}

// eexecDecrypt decrypts eexec-encrypted data
func eexecDecrypt(data []byte) []byte {
	const c1 = 52845
	const c2 = 22719

	r := uint16(55665)
	result := make([]byte, len(data))

	for i, b := range data {
		plain := b ^ byte(r>>8)
		result[i] = plain
		r = (uint16(b)+r)*c1 + c2
	}

	return result
}

// charStringDecrypt decrypts CharString data
func charStringDecrypt(data []byte) []byte {
	const c1 = 52845
	const c2 = 22719

	r := uint16(4330)
	result := make([]byte, 0, len(data))

	// Skip first 4 random bytes (lenIV)
	skip := 4
	for i, b := range data {
		plain := b ^ byte(r>>8)
		if i >= skip {
			result = append(result, plain)
		}
		r = (uint16(b)+r)*c1 + c2
	}

	return result
}

// isHexEncoded checks if data is hex-encoded
func isHexEncoded(data []byte) bool {
	if len(data) < 10 {
		return false
	}
	for i := 0; i < 10; i++ {
		c := data[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') ||
			c == ' ' || c == '\n' || c == '\r' || c == '\t') {
			return false
		}
	}
	return true
}

// decodeHex decodes hex-encoded data
func decodeHex(data []byte) ([]byte, error) {
	result := make([]byte, 0, len(data)/2)
	var hex bytes.Buffer

	for _, b := range data {
		if (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F') {
			hex.WriteByte(b)
			if hex.Len() == 2 {
				val, err := strconv.ParseUint(hex.String(), 16, 8)
				if err != nil {
					return nil, err
				}
				result = append(result, byte(val))
				hex.Reset()
			}
		}
	}

	return result, nil
}

// Helper functions for parsing
func extractName(line []byte) string {
	parts := bytes.Fields(line)
	for i, p := range parts {
		if p[0] == '/' && i < len(parts)-1 {
			next := parts[i+1]
			if next[0] == '/' {
				return string(next[1:])
			}
		}
	}
	return ""
}

func extractString(line []byte) string {
	start := bytes.Index(line, []byte("("))
	end := bytes.LastIndex(line, []byte(")"))
	if start != -1 && end != -1 && end > start {
		return string(line[start+1 : end])
	}
	return ""
}

func extractFloat(line []byte) float64 {
	parts := bytes.Fields(line)
	if len(parts) >= 2 {
		val, _ := strconv.ParseFloat(string(parts[1]), 64)
		return val
	}
	return 0
}

func extractInt(line []byte) int {
	parts := bytes.Fields(line)
	if len(parts) >= 2 {
		val, _ := strconv.Atoi(string(parts[1]))
		return val
	}
	return 0
}

func extractBool(line []byte) bool {
	return bytes.Contains(line, []byte("true"))
}

func extractBBox(line []byte) [4]float64 {
	var bbox [4]float64
	start := bytes.Index(line, []byte("{"))
	if start == -1 {
		start = bytes.Index(line, []byte("["))
	}
	end := bytes.LastIndex(line, []byte("}"))
	if end == -1 {
		end = bytes.LastIndex(line, []byte("]"))
	}
	if start != -1 && end != -1 && end > start {
		parts := bytes.Fields(line[start+1 : end])
		for i := 0; i < 4 && i < len(parts); i++ {
			bbox[i], _ = strconv.ParseFloat(string(parts[i]), 64)
		}
	}
	return bbox
}

func extractMatrix(line []byte) [6]float64 {
	var matrix [6]float64
	start := bytes.Index(line, []byte("["))
	end := bytes.LastIndex(line, []byte("]"))
	if start != -1 && end != -1 && end > start {
		parts := bytes.Fields(line[start+1 : end])
		for i := 0; i < 6 && i < len(parts); i++ {
			matrix[i], _ = strconv.ParseFloat(string(parts[i]), 64)
		}
	}
	return matrix
}

func extractIntArray(line []byte) []int {
	var arr []int
	start := bytes.Index(line, []byte("["))
	end := bytes.LastIndex(line, []byte("]"))
	if start != -1 && end != -1 && end > start {
		parts := bytes.Fields(line[start+1 : end])
		for _, p := range parts {
			if val, err := strconv.Atoi(string(p)); err == nil {
				arr = append(arr, val)
			}
		}
	}
	return arr
}

func parseNextInt(data []byte, result *int) (int, error) {
	data = bytes.TrimLeft(data, " \t\n\r")
	end := 0
	for end < len(data) && data[end] >= '0' && data[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, io.EOF
	}
	val, err := strconv.Atoi(string(data[:end]))
	if err != nil {
		return 0, err
	}
	*result = val
	return end, nil
}

// Standard encoding mapping
var standardEncoding = map[int]string{
	32: "space", 33: "exclam", 34: "quotedbl", 35: "numbersign",
	36: "dollar", 37: "percent", 38: "ampersand", 39: "quoteright",
	40: "parenleft", 41: "parenright", 42: "asterisk", 43: "plus",
	44: "comma", 45: "hyphen", 46: "period", 47: "slash",
	48: "zero", 49: "one", 50: "two", 51: "three",
	52: "four", 53: "five", 54: "six", 55: "seven",
	56: "eight", 57: "nine", 58: "colon", 59: "semicolon",
	60: "less", 61: "equal", 62: "greater", 63: "question",
	64: "at", 65: "A", 66: "B", 67: "C",
	68: "D", 69: "E", 70: "F", 71: "G",
	72: "H", 73: "I", 74: "J", 75: "K",
	76: "L", 77: "M", 78: "N", 79: "O",
	80: "P", 81: "Q", 82: "R", 83: "S",
	84: "T", 85: "U", 86: "V", 87: "W",
	88: "X", 89: "Y", 90: "Z", 91: "bracketleft",
	92: "backslash", 93: "bracketright", 94: "asciicircum", 95: "underscore",
	96: "quoteleft", 97: "a", 98: "b", 99: "c",
	100: "d", 101: "e", 102: "f", 103: "g",
	104: "h", 105: "i", 106: "j", 107: "k",
	108: "l", 109: "m", 110: "n", 111: "o",
	112: "p", 113: "q", 114: "r", 115: "s",
	116: "t", 117: "u", 118: "v", 119: "w",
	120: "x", 121: "y", 122: "z", 123: "braceleft",
	124: "bar", 125: "braceright", 126: "asciitilde",
}

// ParseType1FromStream parses Type1 font from a PDF stream
func ParseType1FromStream(v Value) (*Type1Font, error) {
	if v.Kind() != Stream {
		return nil, nil
	}

	reader := v.Reader()
	if reader == nil {
		return nil, nil
	}
	defer reader.Close() // Important: close reader to prevent resource leak

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	return NewType1Font(data)
}

// IsType1Font checks if a font value is a Type1 font
func IsType1Font(v Value) bool {
	subtype := v.Key("Subtype").Name()
	return subtype == "Type1" || subtype == "MMType1"
}

// GetType1FontInfo extracts Type1 font info from embedded font program
func GetType1FontInfo(v Value) *Type1FontInfo {
	// Check FontDescriptor for embedded font
	desc := v.Key("FontDescriptor")
	if desc.Kind() != Dict {
		return nil
	}

	// Try FontFile (Type1) or FontFile3 (CFF)
	fontFile := desc.Key("FontFile")
	if fontFile.Kind() == Stream {
		font, err := ParseType1FromStream(fontFile)
		if err == nil && font != nil {
			return font.Info()
		}
	}

	return nil
}

// Type1GlyphNames provides glyph name to Unicode mapping
var Type1GlyphNames = map[string]rune{
	"space": ' ', "exclam": '!', "quotedbl": '"', "numbersign": '#',
	"dollar": '$', "percent": '%', "ampersand": '&', "quoteright": '\'',
	"parenleft": '(', "parenright": ')', "asterisk": '*', "plus": '+',
	"comma": ',', "hyphen": '-', "period": '.', "slash": '/',
	"zero": '0', "one": '1', "two": '2', "three": '3',
	"four": '4', "five": '5', "six": '6', "seven": '7',
	"eight": '8', "nine": '9', "colon": ':', "semicolon": ';',
	"less": '<', "equal": '=', "greater": '>', "question": '?',
	"at": '@',
	"A":  'A', "B": 'B', "C": 'C', "D": 'D', "E": 'E', "F": 'F', "G": 'G',
	"H": 'H', "I": 'I', "J": 'J', "K": 'K', "L": 'L', "M": 'M', "N": 'N',
	"O": 'O', "P": 'P', "Q": 'Q', "R": 'R', "S": 'S', "T": 'T', "U": 'U',
	"V": 'V', "W": 'W', "X": 'X', "Y": 'Y', "Z": 'Z',
	"bracketleft": '[', "backslash": '\\', "bracketright": ']',
	"asciicircum": '^', "underscore": '_', "quoteleft": '`',
	"a": 'a', "b": 'b', "c": 'c', "d": 'd', "e": 'e', "f": 'f', "g": 'g',
	"h": 'h', "i": 'i', "j": 'j', "k": 'k', "l": 'l', "m": 'm', "n": 'n',
	"o": 'o', "p": 'p', "q": 'q', "r": 'r', "s": 's', "t": 't', "u": 'u',
	"v": 'v', "w": 'w', "x": 'x', "y": 'y', "z": 'z',
	"braceleft": '{', "bar": '|', "braceright": '}', "asciitilde": '~',
	// Extended characters
	"Agrave": 'À', "Aacute": 'Á', "Acircumflex": 'Â', "Atilde": 'Ã',
	"Adieresis": 'Ä', "Aring": 'Å', "AE": 'Æ', "Ccedilla": 'Ç',
	"Egrave": 'È', "Eacute": 'É', "Ecircumflex": 'Ê', "Edieresis": 'Ë',
	"Igrave": 'Ì', "Iacute": 'Í', "Icircumflex": 'Î', "Idieresis": 'Ï',
	"Eth": 'Ð', "Ntilde": 'Ñ', "Ograve": 'Ò', "Oacute": 'Ó',
	"Ocircumflex": 'Ô', "Otilde": 'Õ', "Odieresis": 'Ö', "multiply": '×',
	"Oslash": 'Ø', "Ugrave": 'Ù', "Uacute": 'Ú', "Ucircumflex": 'Û',
	"Udieresis": 'Ü', "Yacute": 'Ý', "Thorn": 'Þ', "germandbls": 'ß',
	"agrave": 'à', "aacute": 'á', "acircumflex": 'â', "atilde": 'ã',
	"adieresis": 'ä', "aring": 'å', "ae": 'æ', "ccedilla": 'ç',
	"egrave": 'è', "eacute": 'é', "ecircumflex": 'ê', "edieresis": 'ë',
	"igrave": 'ì', "iacute": 'í', "icircumflex": 'î', "idieresis": 'ï',
	"eth": 'ð', "ntilde": 'ñ', "ograve": 'ò', "oacute": 'ó',
	"ocircumflex": 'ô', "otilde": 'õ', "odieresis": 'ö', "divide": '÷',
	"oslash": 'ø', "ugrave": 'ù', "uacute": 'ú', "ucircumflex": 'û',
	"udieresis": 'ü', "yacute": 'ý', "thorn": 'þ', "ydieresis": 'ÿ',
	// Common ligatures and symbols
	"fi": 'ﬁ', "fl": 'ﬂ', "ff": '\ufb00', "ffi": '\ufb03', "ffl": '\ufb04',
	"bullet": '•', "endash": '–', "emdash": '—',
	"quotesingle": '\'', "quotedblleft": '"', "quotedblright": '"',
	"guillemotleft": '«', "guillemotright": '»',
	"ellipsis": '…', "trademark": '™', "copyright": '©', "registered": '®',
	"degree": '°', "plusminus": '±', "mu": 'µ', "paragraph": '¶',
	"section": '§', "dagger": '†', "daggerdbl": '‡',
}

// GlyphNameToRune converts a glyph name to Unicode rune
func GlyphNameToRune(name string) rune {
	if r, ok := Type1GlyphNames[name]; ok {
		return r
	}
	// Try to parse uniXXXX format
	if strings.HasPrefix(name, "uni") && len(name) == 7 {
		if val, err := strconv.ParseInt(name[3:], 16, 32); err == nil {
			return rune(val)
		}
	}
	// Try to parse uXXXXX format
	if strings.HasPrefix(name, "u") && len(name) >= 5 {
		if val, err := strconv.ParseInt(name[1:], 16, 32); err == nil {
			return rune(val)
		}
	}
	return 0
}
