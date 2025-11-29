// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// CFFHeader represents the CFF font header
type CFFHeader struct {
	Major   uint8
	Minor   uint8
	HdrSize uint8
	OffSize uint8
}

// CFFIndex represents a CFF INDEX structure
type CFFIndex struct {
	Count   uint16
	OffSize uint8
	Offsets []uint32
	Data    [][]byte
}

// CFFDict represents a CFF DICT data structure
type CFFDict struct {
	Data map[int]interface{}
}

// CFFFont represents a parsed CFF font
type CFFFont struct {
	Header      *CFFHeader
	NameIndex   *CFFIndex
	TopDict     *CFFDict
	StringIndex *CFFIndex
	GlobalSubrs *CFFIndex
	CharStrings *CFFIndex
	PrivateDict *CFFDict
	LocalSubrs  *CFFIndex
	FDArray     []*CFFDict // For CID-keyed fonts
	FDSelect    []byte     // For CID-keyed fonts
	isCID       bool
}

// NewCFFFont parses CFF font data with caching
func NewCFFFont(data []byte) (*CFFFont, error) {
	// Try to get from cache first
	cache := GetGlobalCFFCache()
	if cachedFont, found := cache.GetFont(data); found {
		return cachedFont, nil
	}

	// Parse the font
	font := &CFFFont{}
	if err := font.parse(data); err != nil {
		return nil, err
	}

	// Cache the parsed font
	cache.PutFont(data, font)

	return font, nil
}

func (f *CFFFont) parse(data []byte) error {
	r := bytes.NewReader(data)

	// Parse header
	if err := f.parseHeader(r); err != nil {
		return fmt.Errorf("failed to parse CFF header: %w", err)
	}

	// Skip to after header
	if _, err := r.Seek(int64(f.Header.HdrSize), io.SeekStart); err != nil {
		return err
	}

	// Parse Name INDEX
	nameIndex, err := f.parseIndex(r)
	if err != nil {
		return fmt.Errorf("failed to parse Name INDEX: %w", err)
	}
	f.NameIndex = nameIndex

	// Parse Top DICT INDEX
	topDictIndex, err := f.parseIndex(r)
	if err != nil {
		return fmt.Errorf("failed to parse Top DICT INDEX: %w", err)
	}

	// Parse first Top DICT
	if len(topDictIndex.Data) > 0 {
		f.TopDict, err = f.parseDict(topDictIndex.Data[0])
		if err != nil {
			return fmt.Errorf("failed to parse Top DICT: %w", err)
		}
	}

	// Parse String INDEX
	stringIndex, err := f.parseIndex(r)
	if err != nil {
		return fmt.Errorf("failed to parse String INDEX: %w", err)
	}
	f.StringIndex = stringIndex

	// Parse Global Subr INDEX
	globalSubrs, err := f.parseIndex(r)
	if err != nil {
		return fmt.Errorf("failed to parse Global Subr INDEX: %w", err)
	}
	f.GlobalSubrs = globalSubrs

	// Check if this is a CID-keyed font
	if ros, ok := f.TopDict.Data[12]; ok { // ROS operator
		if arr, ok := ros.([]interface{}); ok && len(arr) >= 3 {
			f.isCID = true
		}
	}

	if f.isCID {
		return f.parseCIDFont(r)
	}

	return f.parseSimpleFont(r)
}

func (f *CFFFont) parseHeader(r io.Reader) error {
	var hdr CFFHeader
	if err := binary.Read(r, binary.BigEndian, &hdr); err != nil {
		return err
	}
	f.Header = &hdr
	return nil
}

func (f *CFFFont) parseIndex(r io.Reader) (*CFFIndex, error) {
	var count uint16
	if err := binary.Read(r, binary.BigEndian, &count); err != nil {
		return nil, err
	}

	if count == 0 {
		return &CFFIndex{Count: 0, Data: [][]byte{}}, nil
	}

	var offSize uint8
	if err := binary.Read(r, binary.BigEndian, &offSize); err != nil {
		return nil, err
	}

	// Read offsets
	offsets := make([]uint32, count+1)
	for i := 0; i < int(count+1); i++ {
		offset, err := f.readOffset(r, offSize)
		if err != nil {
			return nil, err
		}
		offsets[i] = offset
	}

	// Read data
	dataSize := offsets[count] - offsets[0]
	data := make([]byte, dataSize)
	if _, err := r.Read(data); err != nil {
		return nil, err
	}

	// Split data into objects
	objects := make([][]byte, count)
	for i := 0; i < int(count); i++ {
		start := offsets[i] - offsets[0]
		end := offsets[i+1] - offsets[0]
		objects[i] = data[start:end]
	}

	return &CFFIndex{
		Count:   count,
		OffSize: offSize,
		Offsets: offsets,
		Data:    objects,
	}, nil
}

func (f *CFFFont) readOffset(r io.Reader, offSize uint8) (uint32, error) {
	var offset uint32
	switch offSize {
	case 1:
		var b uint8
		if err := binary.Read(r, binary.BigEndian, &b); err != nil {
			return 0, err
		}
		offset = uint32(b)
	case 2:
		var b uint16
		if err := binary.Read(r, binary.BigEndian, &b); err != nil {
			return 0, err
		}
		offset = uint32(b)
	case 3:
		var b [3]byte
		if _, err := r.Read(b[:]); err != nil {
			return 0, err
		}
		offset = uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
	case 4:
		if err := binary.Read(r, binary.BigEndian, &offset); err != nil {
			return 0, err
		}
	}
	return offset, nil
}

func (f *CFFFont) parseDict(data []byte) (*CFFDict, error) {
	dict := &CFFDict{Data: make(map[int]interface{})}
	r := bytes.NewReader(data)

	var operands []interface{}

	for r.Len() > 0 {
		b, err := r.ReadByte()
		if err != nil {
			break
		}

		if b <= 27 { // Operator
			if b == 12 { // Two-byte operator
				b2, err := r.ReadByte()
				if err != nil {
					return nil, err
				}
				op := int(b)<<8 | int(b2)
				f.executeOperator(dict, op, operands)
			} else {
				f.executeOperator(dict, int(b), operands)
			}
			operands = nil
		} else if b >= 28 && b <= 31 { // Operands
			operand, err := f.readOperand(r, b)
			if err != nil {
				return nil, err
			}
			operands = append(operands, operand)
		} else if b >= 32 && b <= 246 { // Integer
			operands = append(operands, int(b)-139)
		} else if b >= 247 && b <= 250 { // Integer
			b2, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			operands = append(operands, (int(b)-247)*256+int(b2)+108)
		} else if b >= 251 && b <= 254 { // Integer
			b2, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			operands = append(operands, -(int(b)-251)*256-int(b2)-108)
		} else if b == 255 { // Real
			realBytes := make([]byte, 4)
			if _, err := r.Read(realBytes); err != nil {
				return nil, err
			}
			// Convert to float (simplified)
			operands = append(operands, float64(binary.BigEndian.Uint32(realBytes)))
		}
	}

	return dict, nil
}

func (f *CFFFont) readOperand(r *bytes.Reader, b byte) (interface{}, error) {
	switch b {
	case 28: // 16-bit integer
		var val int16
		if err := binary.Read(r, binary.BigEndian, &val); err != nil {
			return nil, err
		}
		return int(val), nil
	case 29: // 32-bit integer
		var val int32
		if err := binary.Read(r, binary.BigEndian, &val); err != nil {
			return nil, err
		}
		return int(val), nil
	case 30: // Real number
		return f.readReal(r), nil
	}
	return nil, fmt.Errorf("unknown operand type: %d", b)
}

func (f *CFFFont) readReal(r *bytes.Reader) float64 {
	var buf []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			break
		}
		if b&0x0F == 0x0F {
			break
		}
		buf = append(buf, b)
	}
	// Simplified real parsing - in practice, this needs full implementation
	return 0.0
}

func (f *CFFFont) executeOperator(dict *CFFDict, op int, operands []interface{}) {
	if len(operands) == 0 {
		return
	}

	// Handle two-byte operators (12 xx)
	if op&0xFF00 == 0x0C00 {
		secondByte := op & 0xFF
		switch secondByte {
		case 0: // Copyright
			dict.Data[0x0C00] = operands[0]
		case 1: // isFixedPitch
			dict.Data[0x0C01] = operands[0]
		case 2: // ItalicAngle
			dict.Data[0x0C02] = operands[0]
		case 3: // UnderlinePosition
			dict.Data[0x0C03] = operands[0]
		case 4: // UnderlineThickness
			dict.Data[0x0C04] = operands[0]
		case 5: // PaintType
			dict.Data[0x0C05] = operands[0]
		case 6: // CharstringType
			dict.Data[0x0C06] = operands[0]
		case 7: // FontMatrix
			dict.Data[0x0C07] = operands
		case 8: // StrokeWidth
			dict.Data[0x0C08] = operands[0]
		case 20: // SyntheticBase
			dict.Data[0x0C14] = operands[0]
		case 21: // PostScript
			dict.Data[0x0C15] = operands[0]
		case 22: // BaseFontName
			dict.Data[0x0C16] = operands[0]
		case 23: // BaseFontBlend
			dict.Data[0x0C17] = operands
		case 30: // ROS (Registry, Ordering, Supplement)
			dict.Data[0x0C1E] = operands
		case 31: // CIDFontVersion
			dict.Data[0x0C1F] = operands[0]
		case 32: // CIDFontRevision
			dict.Data[0x0C20] = operands[0]
		case 33: // CIDFontType
			dict.Data[0x0C21] = operands[0]
		case 34: // CIDCount
			dict.Data[0x0C22] = operands[0]
		case 35: // UIDBase
			dict.Data[0x0C23] = operands[0]
		case 36: // FDArray
			dict.Data[0x0C24] = operands[0]
		case 37: // FDSelect
			dict.Data[0x0C25] = operands[0]
		case 38: // FontName
			dict.Data[0x0C26] = operands[0]
		}
		return
	}

	// Handle single-byte operators
	switch op {
	case 0: // version
		dict.Data[0] = operands[0]
	case 1: // Notice
		dict.Data[1] = operands[0]
	case 2: // FullName
		dict.Data[2] = operands[0]
	case 3: // FamilyName
		dict.Data[3] = operands[0]
	case 4: // Weight
		dict.Data[4] = operands[0]
	case 5: // FontBBox
		dict.Data[5] = operands
	case 13: // UniqueID
		dict.Data[13] = operands[0]
	case 14: // XUID
		dict.Data[14] = operands
	case 15: // charset
		dict.Data[15] = operands[0]
	case 16: // Encoding
		dict.Data[16] = operands[0]
	case 17: // CharStrings
		dict.Data[17] = operands[0]
	case 18: // Private
		dict.Data[18] = operands
	case 19: // Subrs (Local Subrs)
		dict.Data[19] = operands[0]
	}
}

func (f *CFFFont) parseSimpleFont(r *bytes.Reader) error {
	// Parse CharStrings INDEX
	charStrings, err := f.parseIndex(r)
	if err != nil {
		return fmt.Errorf("failed to parse CharStrings INDEX: %w", err)
	}
	f.CharStrings = charStrings

	// Parse Private DICT if present
	if private, ok := f.TopDict.Data[18]; ok {
		if arr, ok := private.([]interface{}); ok && len(arr) >= 2 {
			size := int(arr[0].(int))
			offset := int(arr[1].(int))
			// Seek to private dict
			currentPos, _ := r.Seek(0, io.SeekCurrent)
			r.Seek(currentPos-int64(offset), io.SeekStart)

			privateData := make([]byte, size)
			if _, err := r.Read(privateData); err != nil {
				return err
			}

			f.PrivateDict, err = f.parseDict(privateData)
			if err != nil {
				return fmt.Errorf("failed to parse Private DICT: %w", err)
			}

			// Parse Local Subrs if present
			if _, ok := f.PrivateDict.Data[19]; ok {
				r.Seek(currentPos-int64(offset), io.SeekStart)
				localSubrs, err := f.parseIndex(r)
				if err != nil {
					return fmt.Errorf("failed to parse Local Subrs: %w", err)
				}
				f.LocalSubrs = localSubrs
			}
		}
	}

	return nil
}

func (f *CFFFont) parseCIDFont(r *bytes.Reader) error {
	// Parse CharStrings INDEX
	charStrings, err := f.parseIndex(r)
	if err != nil {
		return fmt.Errorf("failed to parse CharStrings INDEX: %w", err)
	}
	f.CharStrings = charStrings

	// Parse FDArray
	if fdArrayOffset, ok := f.TopDict.Data[12|36]; ok {
		offset := int(fdArrayOffset.(int))
		r.Seek(int64(offset), io.SeekStart)

		fdArrayIndex, err := f.parseIndex(r)
		if err != nil {
			return fmt.Errorf("failed to parse FDArray: %w", err)
		}

		f.FDArray = make([]*CFFDict, len(fdArrayIndex.Data))
		for i, data := range fdArrayIndex.Data {
			f.FDArray[i], err = f.parseDict(data)
			if err != nil {
				return fmt.Errorf("failed to parse FDArray[%d]: %w", i, err)
			}
		}
	}

	// Parse FDSelect
	if fdSelectOffset, ok := f.TopDict.Data[12|37]; ok {
		offset := int(fdSelectOffset.(int))
		r.Seek(int64(offset), io.SeekStart)

		// Read FDSelect format and data
		var format uint8
		if err := binary.Read(r, binary.BigEndian, &format); err != nil {
			return err
		}

		// For simplicity, read the entire FDSelect data
		// In practice, this needs proper parsing based on format
		remaining := f.CharStrings.Count
		f.FDSelect = make([]byte, remaining)
		if _, err := r.Read(f.FDSelect); err != nil {
			return err
		}
	}

	return nil
}

// GetFontName returns the font name
func (f *CFFFont) GetFontName() string {
	if f.NameIndex != nil && len(f.NameIndex.Data) > 0 {
		return string(f.NameIndex.Data[0])
	}
	return ""
}

// GetCharString returns the CharString for a given glyph index
func (f *CFFFont) GetCharString(gid int) []byte {
	if f.CharStrings != nil && gid < len(f.CharStrings.Data) {
		return f.CharStrings.Data[gid]
	}
	return nil
}

// GetFDIndex returns the Font DICT index for a CID (CID-keyed fonts)
func (f *CFFFont) GetFDIndex(cid int) int {
	if !f.isCID || len(f.FDSelect) == 0 {
		return 0
	}

	if cid < len(f.FDSelect) {
		return int(f.FDSelect[cid])
	}
	return 0
}

// IsCID returns true if this is a CID-keyed font
func (f *CFFFont) IsCID() bool {
	return f.isCID
}

// CFFCharStringDecoder decodes CFF CharString data
type CFFCharStringDecoder struct {
	data     []byte
	pos      int
	stack    []float64
	width    float64
	hasWidth bool
}

// NewCFFCharStringDecoder creates a new CharString decoder with pooled objects
func NewCFFCharStringDecoder(data []byte) *CFFCharStringDecoder {
	pool := GetGlobalCFFPool()
	return &CFFCharStringDecoder{
		data:  data,
		stack: pool.GetStack(),
	}
}

// Decode decodes the CharString and returns the path commands with caching and pooling
func (d *CFFCharStringDecoder) Decode() ([]interface{}, error) {
	// Try to get from cache first
	cache := GetGlobalCFFCache()
	if cachedCommands, found := cache.GetDecoding(d.data); found {
		return cachedCommands, nil
	}

	// Get pooled command slice
	pool := GetGlobalCFFPool()
	commands := pool.GetCommandSlice()

	// Decode the commands
	for d.pos < len(d.data) {
		cmd, err := d.decodeCommand()
		if err != nil {
			pool.PutCommandSlice(commands)
			return nil, err
		}

		if cmd != nil {
			commands = append(commands, cmd)
		}
	}

	// Make a copy for caching (since we're returning the pooled slice)
	commandsCopy := make([]interface{}, len(commands))
	copy(commandsCopy, commands)

	// Cache the decoded commands
	cache.PutDecoding(d.data, commandsCopy)

	// Return the pooled slice (caller should return it to pool when done)
	return commands, nil
}

func (d *CFFCharStringDecoder) decodeCommand() (interface{}, error) {
	b := d.data[d.pos]
	d.pos++

	if b == 255 { // Reserved
		return nil, nil
	}

	if b >= 32 && b <= 246 { // Integer
		d.stack = append(d.stack, float64(b-139))
		return nil, nil
	}

	if b >= 247 && b <= 250 { // Integer
		b1 := d.data[d.pos]
		d.pos++
		d.stack = append(d.stack, float64((int(b)-247)*256+int(b1)+108))
		return nil, nil
	}

	if b >= 251 && b <= 254 { // Integer
		b1 := d.data[d.pos]
		d.pos++
		d.stack = append(d.stack, float64(-(int(b)-251)*256-int(b1)-108))
		return nil, nil
	}

	if b == 28 { // 16-bit integer
		if d.pos+1 >= len(d.data) {
			return nil, fmt.Errorf("unexpected end of data")
		}
		val := int16(d.data[d.pos])<<8 | int16(d.data[d.pos+1])
		d.pos += 2
		d.stack = append(d.stack, float64(val))
		return nil, nil
	}

	if b == 255 { // 32-bit fixed point (16.16)
		if d.pos+3 >= len(d.data) {
			return nil, fmt.Errorf("unexpected end of data")
		}
		val := binary.BigEndian.Uint32(d.data[d.pos : d.pos+4])
		d.pos += 4
		d.stack = append(d.stack, float64(val)/65536.0)
		return nil, nil
	}

	// Commands
	switch b {
	case 1: // hstem
		return d.handleStem(true), nil
	case 3: // vstem
		return d.handleStem(false), nil
	case 4: // vmoveto
		return d.handleMoveto(false), nil
	case 5: // rlineto
		return d.handleLineto(true), nil
	case 6: // hlineto
		return d.handleLineto(false), nil
	case 7: // vlineto
		return d.handleLineto(true), nil
	case 8: // rrcurveto
		return d.handleCurveto(true), nil
	case 9: // closepath
		return "closepath", nil
	case 10: // callsubr
		return d.handleCallsubr(false), nil
	case 11: // return
		return "return", nil
	case 12: // escape
		return d.handleEscape(), nil
	case 13: // hsbw (horizontal side bearing and width)
		return d.handleHsbw(), nil
	case 14: // endchar
		return "endchar", nil
	case 21: // rmoveto
		return d.handleMoveto(true), nil
	case 22: // hmoveto
		return d.handleMoveto(false), nil
	case 30: // vhcurveto
		return d.handleCurveto(false), nil
	case 31: // hvcurveto
		return d.handleCurveto(false), nil
	}

	return nil, fmt.Errorf("unknown command: %d", b)
}

func (d *CFFCharStringDecoder) handleStem(horizontal bool) interface{} {
	// Simplified stem handling
	d.stack = d.stack[:0] // Clear stack
	return map[string]interface{}{
		"type":       "stem",
		"horizontal": horizontal,
	}
}

func (d *CFFCharStringDecoder) handleMoveto(relative bool) interface{} {
	if len(d.stack) < 2 {
		return nil
	}

	dy := d.stack[len(d.stack)-1]
	dx := d.stack[len(d.stack)-2]
	d.stack = d.stack[:len(d.stack)-2]

	return map[string]interface{}{
		"type":     "moveto",
		"relative": relative,
		"dx":       dx,
		"dy":       dy,
	}
}

func (d *CFFCharStringDecoder) handleLineto(relative bool) interface{} {
	if len(d.stack) < 2 {
		return nil
	}

	// For simplicity, handle pairs
	var lines []map[string]interface{}
	for len(d.stack) >= 2 {
		dy := d.stack[len(d.stack)-1]
		dx := d.stack[len(d.stack)-2]
		d.stack = d.stack[:len(d.stack)-2]

		lines = append(lines, map[string]interface{}{
			"type": "lineto",
			"dx":   dx,
			"dy":   dy,
		})
	}

	return map[string]interface{}{
		"type":  "lineto",
		"lines": lines,
	}
}

func (d *CFFCharStringDecoder) handleCurveto(relative bool) interface{} {
	if len(d.stack) < 6 {
		return nil
	}

	dy3 := d.stack[len(d.stack)-1]
	dx3 := d.stack[len(d.stack)-2]
	dy2 := d.stack[len(d.stack)-3]
	dx2 := d.stack[len(d.stack)-4]
	dy1 := d.stack[len(d.stack)-5]
	dx1 := d.stack[len(d.stack)-6]
	d.stack = d.stack[:len(d.stack)-6]

	return map[string]interface{}{
		"type": "curveto",
		"dx1":  dx1,
		"dy1":  dy1,
		"dx2":  dx2,
		"dy2":  dy2,
		"dx3":  dx3,
		"dy3":  dy3,
	}
}

func (d *CFFCharStringDecoder) handleCallsubr(global bool) interface{} {
	if len(d.stack) < 1 {
		return nil
	}

	subr := int(d.stack[len(d.stack)-1])
	d.stack = d.stack[:len(d.stack)-1]

	return map[string]interface{}{
		"type":   "callsubr",
		"global": global,
		"index":  subr,
	}
}

func (d *CFFCharStringDecoder) handleEscape() interface{} {
	if d.pos >= len(d.data) {
		return nil
	}

	esc := d.data[d.pos]
	d.pos++

	switch esc {
	case 9: // abs cur
		return d.handleCurveto(true)
	case 10: // flex
		return d.handleFlex()
	case 11: // hflex
		return d.handleFlex()
	case 12: // hflex1
		return d.handleFlex()
	case 13: // flex1
		return d.handleFlex()
	}

	return nil
}

func (d *CFFCharStringDecoder) handleFlex() interface{} {
	// Simplified flex handling
	d.stack = d.stack[:0]
	return map[string]interface{}{
		"type": "flex",
	}
}

func (d *CFFCharStringDecoder) handleHsbw() interface{} {
	if len(d.stack) < 2 {
		return nil
	}

	width := d.stack[len(d.stack)-1]
	sbx := d.stack[len(d.stack)-2]
	d.stack = d.stack[:len(d.stack)-2]

	d.width = width
	d.hasWidth = true

	return map[string]interface{}{
		"type":  "hsbw",
		"sbx":   sbx,
		"width": width,
	}
}

// GetWidth returns the glyph width if available
func (d *CFFCharStringDecoder) GetWidth() (float64, bool) {
	return d.width, d.hasWidth
}
