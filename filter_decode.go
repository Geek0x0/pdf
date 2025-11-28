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

// CCITTFaxDecoder decodes CCITT Group 3 and Group 4 fax encoded data
// as specified in PDF 32000-1:2008, Section 7.4.6
type CCITTFaxDecoder struct {
	r           io.Reader
	params      CCITTFaxParams
	width       int
	height      int
	currentRow  int
	buf         *bytes.Buffer
	refLine     []byte // Reference line for 2D encoding
	currentLine []byte // Current decoding line
	bitReader   *bitReader
	eofReached  bool
}

// CCITTFaxParams contains parameters for CCITT fax decoding
type CCITTFaxParams struct {
	K                      int  // <0: pure 2D (Group 4), 0: pure 1D (Group 3), >0: mixed
	EndOfLine              bool // If true, require EOL alignment bits
	EncodedByteAlign       bool // If true, encoded data is byte-aligned after each row
	Columns                int  // Width of image in pixels (default: 1728)
	Rows                   int  // Height of image (0 = unknown)
	EndOfBlock             bool // If true, expect EOFB sequence
	BlackIs1               bool // If true, 1 bits represent black pixels
	DamagedRowsBeforeError int  // Max consecutive damaged rows before error
}

// DefaultCCITTFaxParams returns default CCITT fax parameters
func DefaultCCITTFaxParams() CCITTFaxParams {
	return CCITTFaxParams{
		K:                      0, // Group 3 1D
		EndOfLine:              false,
		EncodedByteAlign:       false,
		Columns:                1728,
		Rows:                   0,
		EndOfBlock:             true,
		BlackIs1:               false,
		DamagedRowsBeforeError: 0,
	}
}

// NewCCITTFaxDecoder creates a new CCITT fax decoder
func NewCCITTFaxDecoder(r io.Reader, params CCITTFaxParams) *CCITTFaxDecoder {
	if params.Columns <= 0 {
		params.Columns = 1728
	}

	return &CCITTFaxDecoder{
		r:           r,
		params:      params,
		width:       params.Columns,
		height:      params.Rows,
		buf:         new(bytes.Buffer),
		refLine:     make([]byte, params.Columns),
		currentLine: make([]byte, params.Columns),
		bitReader:   newBitReader(r),
	}
}

// Read implements io.Reader
func (d *CCITTFaxDecoder) Read(p []byte) (n int, err error) {
	if d.eofReached {
		return 0, io.EOF
	}

	// Fill buffer with decoded data
	for d.buf.Len() < len(p) && !d.eofReached {
		if err := d.decodeRow(); err != nil {
			if err == io.EOF {
				d.eofReached = true
				break
			}
			return 0, err
		}
	}

	return d.buf.Read(p)
}

func (d *CCITTFaxDecoder) decodeRow() error {
	if d.params.K < 0 {
		// Group 4 (pure 2D)
		return d.decode2D()
	} else if d.params.K == 0 {
		// Group 3 (pure 1D)
		return d.decode1D()
	} else {
		// Mixed mode (Group 3 with 2D)
		return d.decodeMixed()
	}
}

func (d *CCITTFaxDecoder) decode1D() error {
	// Modified Huffman (MH) coding
	col := 0
	white := true // Start with white run

	for col < d.width {
		var runLen int
		var err error

		if white {
			runLen, err = d.readWhiteCode()
		} else {
			runLen, err = d.readBlackCode()
		}

		if err != nil {
			return err
		}

		// Fill pixels
		val := byte(0)
		if !white {
			val = 1
		}
		if d.params.BlackIs1 {
			val = 1 - val
		}

		for i := 0; i < runLen && col < d.width; i++ {
			d.currentLine[col] = val
			col++
		}

		// Check for make-up code (run >= 64)
		if runLen < 64 {
			white = !white
		}
	}

	// Output row
	d.outputRow()
	d.currentRow++

	// Check for EOL or height
	if d.height > 0 && d.currentRow >= d.height {
		return io.EOF
	}

	return nil
}

func (d *CCITTFaxDecoder) decode2D() error {
	// Two-dimensional coding (Group 4)
	col := 0
	a0 := -1

	for col < d.width {
		code, err := d.read2DCode()
		if err != nil {
			return err
		}

		switch code {
		case ccittPass:
			// Pass mode
			b1 := d.findB1(a0, col)
			b2 := d.findB2(b1)
			col = b2

		case ccittHorizontal:
			// Horizontal mode
			var runLen1, runLen2 int
			var err error

			isWhite := (a0 < 0 || d.refLine[a0] == 0)
			if isWhite {
				runLen1, err = d.readWhiteCode()
				if err != nil {
					return err
				}
				runLen2, err = d.readBlackCode()
			} else {
				runLen1, err = d.readBlackCode()
				if err != nil {
					return err
				}
				runLen2, err = d.readWhiteCode()
			}
			if err != nil {
				return err
			}

			// Fill first run
			val1 := byte(0)
			if !isWhite {
				val1 = 1
			}
			for i := 0; i < runLen1 && col < d.width; i++ {
				d.currentLine[col] = val1
				col++
			}

			// Fill second run
			val2 := 1 - val1
			for i := 0; i < runLen2 && col < d.width; i++ {
				d.currentLine[col] = val2
				col++
			}

			a0 = col - 1

		case ccittVertical0:
			b1 := d.findB1(a0, col)
			col = b1
			d.fillToColumn(a0+1, col)
			a0 = col - 1

		case ccittVerticalR1, ccittVerticalR2, ccittVerticalR3:
			b1 := d.findB1(a0, col)
			offset := code - ccittVertical0
			col = b1 + offset
			d.fillToColumn(a0+1, col)
			a0 = col - 1

		case ccittVerticalL1, ccittVerticalL2, ccittVerticalL3:
			b1 := d.findB1(a0, col)
			offset := ccittVertical0 - code
			col = b1 - offset
			if col < 0 {
				col = 0
			}
			d.fillToColumn(a0+1, col)
			a0 = col - 1

		case ccittEOFB:
			return io.EOF
		}
	}

	// Output row and swap lines
	d.outputRow()
	copy(d.refLine, d.currentLine)
	d.currentRow++

	if d.height > 0 && d.currentRow >= d.height {
		return io.EOF
	}

	return nil
}

func (d *CCITTFaxDecoder) decodeMixed() error {
	// Mixed 1D/2D mode - check tag bit
	bit, err := d.bitReader.ReadBit()
	if err != nil {
		return err
	}

	if bit == 1 {
		return d.decode1D()
	}
	return d.decode2D()
}

func (d *CCITTFaxDecoder) findB1(a0, col int) int {
	// Find first changing element in reference line after a0
	start := a0 + 1
	if start < 0 {
		start = 0
	}

	currentColor := byte(0)
	if a0 >= 0 && a0 < d.width {
		currentColor = d.currentLine[a0]
	}

	for i := start; i < d.width; i++ {
		if d.refLine[i] != currentColor {
			return i
		}
	}
	return d.width
}

func (d *CCITTFaxDecoder) findB2(b1 int) int {
	// Find next changing element after b1
	if b1 >= d.width {
		return d.width
	}

	color := d.refLine[b1]
	for i := b1 + 1; i < d.width; i++ {
		if d.refLine[i] != color {
			return i
		}
	}
	return d.width
}

func (d *CCITTFaxDecoder) fillToColumn(from, to int) {
	if from < 0 {
		from = 0
	}

	var val byte
	if from > 0 && from <= d.width {
		// Continue with opposite color
		val = 1 - d.currentLine[from-1]
	}

	for i := from; i < to && i < d.width; i++ {
		d.currentLine[i] = val
	}
}

func (d *CCITTFaxDecoder) outputRow() {
	// Pack bits into bytes
	for i := 0; i < d.width; i += 8 {
		var b byte
		for j := 0; j < 8 && i+j < d.width; j++ {
			if d.currentLine[i+j] != 0 {
				b |= 0x80 >> j
			}
		}
		d.buf.WriteByte(b)
	}
}

// 2D code types
const (
	ccittPass = iota
	ccittHorizontal
	ccittVertical0
	ccittVerticalR1
	ccittVerticalR2
	ccittVerticalR3
	ccittVerticalL1
	ccittVerticalL2
	ccittVerticalL3
	ccittEOFB
)

func (d *CCITTFaxDecoder) read2DCode() (int, error) {
	// Read 2D mode codes
	// These are variable-length codes from the CCITT tables

	bits, err := d.bitReader.PeekBits(7)
	if err != nil {
		return 0, err
	}

	// Check for common codes first
	switch {
	case bits>>6 == 1: // 1
		d.bitReader.SkipBits(1)
		return ccittVertical0, nil
	case bits>>5 == 6: // 011
		d.bitReader.SkipBits(3)
		return ccittHorizontal, nil
	case bits>>4 == 2: // 0010
		d.bitReader.SkipBits(4)
		return ccittPass, nil
	case bits>>4 == 3: // 0011
		d.bitReader.SkipBits(4)
		return ccittVerticalR1, nil
	case bits>>4 == 1: // 0001
		d.bitReader.SkipBits(4)
		return ccittVerticalL1, nil
	case bits>>2 == 3: // 000011
		d.bitReader.SkipBits(6)
		return ccittVerticalR2, nil
	case bits>>2 == 2: // 000010
		d.bitReader.SkipBits(6)
		return ccittVerticalL2, nil
	case bits == 3: // 0000011
		d.bitReader.SkipBits(7)
		return ccittVerticalR3, nil
	case bits == 2: // 0000010
		d.bitReader.SkipBits(7)
		return ccittVerticalL3, nil
	}

	// Check for EOFB (12 zeros followed by 1 or just end of data)
	if bits == 0 {
		moreBits, _ := d.bitReader.PeekBits(12)
		if moreBits == 0 {
			return ccittEOFB, nil
		}
	}

	return 0, fmt.Errorf("invalid 2D CCITT code")
}

func (d *CCITTFaxDecoder) readWhiteCode() (int, error) {
	return d.readHuffmanCode(whiteTable)
}

func (d *CCITTFaxDecoder) readBlackCode() (int, error) {
	return d.readHuffmanCode(blackTable)
}

func (d *CCITTFaxDecoder) readHuffmanCode(table []huffmanEntry) (int, error) {
	total := 0

	for {
		runLen, err := d.lookupHuffman(table)
		if err != nil {
			return 0, err
		}

		total += runLen

		// Check if this is a terminating code (< 64)
		if runLen < 64 {
			return total, nil
		}
	}
}

func (d *CCITTFaxDecoder) lookupHuffman(table []huffmanEntry) (int, error) {
	bits, err := d.bitReader.PeekBits(13) // Max code length
	if err != nil && err != io.EOF {
		return 0, err
	}

	for _, entry := range table {
		mask := uint32(0xFFFF) << (16 - entry.bits)
		if (uint32(bits)<<3)&mask == uint32(entry.code)<<(16-entry.bits) {
			d.bitReader.SkipBits(int(entry.bits))
			return int(entry.runLen), nil
		}
	}

	return 0, fmt.Errorf("invalid Huffman code in CCITT data")
}

// huffmanEntry represents a Huffman code entry
type huffmanEntry struct {
	code   uint16
	bits   uint8
	runLen uint16
}

// White terminating codes (0-63)
var whiteTable = []huffmanEntry{
	{0x35, 8, 0},  // 00110101
	{0x7, 6, 1},   // 000111
	{0x7, 4, 2},   // 0111
	{0x8, 4, 3},   // 1000
	{0xB, 4, 4},   // 1011
	{0xC, 4, 5},   // 1100
	{0xE, 4, 6},   // 1110
	{0xF, 4, 7},   // 1111
	{0x13, 5, 8},  // 10011
	{0x14, 5, 9},  // 10100
	{0x7, 5, 10},  // 00111
	{0x8, 5, 11},  // 01000
	{0x8, 6, 12},  // 001000
	{0x3, 6, 13},  // 000011
	{0x34, 6, 14}, // 110100
	{0x35, 6, 15}, // 110101
	{0x2A, 6, 16}, // 101010
	{0x2B, 6, 17}, // 101011
	{0x27, 7, 18}, // 0100111
	{0xC, 7, 19},  // 0001100
	{0x8, 7, 20},  // 0001000
	{0x17, 7, 21}, // 0010111
	{0x3, 7, 22},  // 0000011
	{0x4, 7, 23},  // 0000100
	{0x28, 7, 24}, // 0101000
	{0x2B, 7, 25}, // 0101011
	{0x13, 7, 26}, // 0010011
	{0x24, 7, 27}, // 0100100
	{0x18, 7, 28}, // 0011000
	{0x2, 8, 29},  // 00000010
	{0x3, 8, 30},  // 00000011
	{0x1A, 8, 31}, // 00011010
	{0x1B, 8, 32}, // 00011011
	{0x12, 8, 33}, // 00010010
	{0x13, 8, 34}, // 00010011
	{0x14, 8, 35}, // 00010100
	{0x15, 8, 36}, // 00010101
	{0x16, 8, 37}, // 00010110
	{0x17, 8, 38}, // 00010111
	{0x28, 8, 39}, // 00101000
	{0x29, 8, 40}, // 00101001
	{0x2A, 8, 41}, // 00101010
	{0x2B, 8, 42}, // 00101011
	{0x2C, 8, 43}, // 00101100
	{0x2D, 8, 44}, // 00101101
	{0x4, 8, 45},  // 00000100
	{0x5, 8, 46},  // 00000101
	{0xA, 8, 47},  // 00001010
	{0xB, 8, 48},  // 00001011
	{0x52, 8, 49}, // 01010010
	{0x53, 8, 50}, // 01010011
	{0x54, 8, 51}, // 01010100
	{0x55, 8, 52}, // 01010101
	{0x24, 8, 53}, // 00100100
	{0x25, 8, 54}, // 00100101
	{0x58, 8, 55}, // 01011000
	{0x59, 8, 56}, // 01011001
	{0x5A, 8, 57}, // 01011010
	{0x5B, 8, 58}, // 01011011
	{0x4A, 8, 59}, // 01001010
	{0x4B, 8, 60}, // 01001011
	{0x32, 8, 61}, // 00110010
	{0x33, 8, 62}, // 00110011
	{0x34, 8, 63}, // 00110100
	// Make-up codes (64, 128, ...)
	{0x1B, 5, 64},
	{0x12, 5, 128},
	{0x17, 6, 192},
	{0x37, 7, 256},
	{0x36, 8, 320},
	{0x37, 8, 384},
	{0x64, 8, 448},
	{0x65, 8, 512},
	{0x68, 8, 576},
	{0x67, 8, 640},
	{0xCC, 9, 704},
	{0xCD, 9, 768},
	{0xD2, 9, 832},
	{0xD3, 9, 896},
	{0xD4, 9, 960},
	{0xD5, 9, 1024},
	{0xD6, 9, 1088},
	{0xD7, 9, 1152},
	{0xD8, 9, 1216},
	{0xD9, 9, 1280},
	{0xDA, 9, 1344},
	{0xDB, 9, 1408},
	{0x98, 9, 1472},
	{0x99, 9, 1536},
	{0x9A, 9, 1600},
	{0x18, 6, 1664},
	{0x9B, 9, 1728},
}

// Black terminating codes (0-63)
var blackTable = []huffmanEntry{
	{0x37, 10, 0},  // 0000110111
	{0x2, 3, 1},    // 010
	{0x3, 2, 2},    // 11
	{0x2, 2, 3},    // 10
	{0x3, 3, 4},    // 011
	{0x3, 4, 5},    // 0011
	{0x2, 4, 6},    // 0010
	{0x3, 5, 7},    // 00011
	{0x5, 6, 8},    // 000101
	{0x4, 6, 9},    // 000100
	{0x4, 7, 10},   // 0000100
	{0x5, 7, 11},   // 0000101
	{0x7, 7, 12},   // 0000111
	{0x4, 8, 13},   // 00000100
	{0x7, 8, 14},   // 00000111
	{0x18, 9, 15},  // 000011000
	{0x17, 10, 16}, // 0000010111
	{0x18, 10, 17}, // 0000011000
	{0x8, 10, 18},  // 0000001000
	{0x67, 11, 19}, // 00001100111
	{0x68, 11, 20}, // 00001101000
	{0x6C, 11, 21}, // 00001101100
	{0x37, 11, 22}, // 00000110111
	{0x28, 11, 23}, // 00000101000
	{0x17, 11, 24}, // 00000010111
	{0x18, 11, 25}, // 00000011000
	{0xCA, 12, 26}, // 000011001010
	{0xCB, 12, 27}, // 000011001011
	{0xCC, 12, 28}, // 000011001100
	{0xCD, 12, 29}, // 000011001101
	{0x68, 12, 30}, // 000001101000
	{0x69, 12, 31}, // 000001101001
	{0x6A, 12, 32}, // 000001101010
	{0x6B, 12, 33}, // 000001101011
	{0xD2, 12, 34}, // 000011010010
	{0xD3, 12, 35}, // 000011010011
	{0xD4, 12, 36}, // 000011010100
	{0xD5, 12, 37}, // 000011010101
	{0xD6, 12, 38}, // 000011010110
	{0xD7, 12, 39}, // 000011010111
	{0x6C, 12, 40}, // 000001101100
	{0x6D, 12, 41}, // 000001101101
	{0xDA, 12, 42}, // 000011011010
	{0xDB, 12, 43}, // 000011011011
	{0x54, 12, 44}, // 000001010100
	{0x55, 12, 45}, // 000001010101
	{0x56, 12, 46}, // 000001010110
	{0x57, 12, 47}, // 000001010111
	{0x64, 12, 48}, // 000001100100
	{0x65, 12, 49}, // 000001100101
	{0x52, 12, 50}, // 000001010010
	{0x53, 12, 51}, // 000001010011
	{0x24, 12, 52}, // 000000100100
	{0x37, 12, 53}, // 000000110111
	{0x38, 12, 54}, // 000000111000
	{0x27, 12, 55}, // 000000100111
	{0x28, 12, 56}, // 000000101000
	{0x58, 12, 57}, // 000001011000
	{0x59, 12, 58}, // 000001011001
	{0x2B, 12, 59}, // 000000101011
	{0x2C, 12, 60}, // 000000101100
	{0x5A, 12, 61}, // 000001011010
	{0x66, 12, 62}, // 000001100110
	{0x67, 12, 63}, // 000001100111
	// Make-up codes
	{0xF, 10, 64},
	{0xC8, 12, 128},
	{0xC9, 12, 192},
	{0x5B, 12, 256},
	{0x33, 12, 320},
	{0x34, 12, 384},
	{0x35, 12, 448},
	{0x6C, 13, 512},
	{0x6D, 13, 576},
	{0x4A, 13, 640},
	{0x4B, 13, 704},
	{0x4C, 13, 768},
	{0x4D, 13, 832},
	{0x72, 13, 896},
	{0x73, 13, 960},
	{0x74, 13, 1024},
	{0x75, 13, 1088},
	{0x76, 13, 1152},
	{0x77, 13, 1216},
	{0x52, 13, 1280},
	{0x53, 13, 1344},
	{0x54, 13, 1408},
	{0x55, 13, 1472},
	{0x5A, 13, 1536},
	{0x5B, 13, 1600},
	{0x64, 13, 1664},
	{0x65, 13, 1728},
}

// bitReader provides bit-level reading from an io.Reader
type bitReader struct {
	r    io.Reader
	buf  uint32
	bits int
	err  error
}

func newBitReader(r io.Reader) *bitReader {
	return &bitReader{r: r}
}

func (br *bitReader) ReadBit() (int, error) {
	if br.bits == 0 {
		if err := br.fill(); err != nil {
			return 0, err
		}
	}

	br.bits--
	bit := int((br.buf >> br.bits) & 1)
	return bit, nil
}

func (br *bitReader) PeekBits(n int) (uint32, error) {
	for br.bits < n {
		if err := br.fill(); err != nil {
			if err == io.EOF && br.bits > 0 {
				// Return what we have
				return br.buf << (n - br.bits), nil
			}
			return 0, err
		}
	}

	return (br.buf >> (br.bits - n)) & ((1 << n) - 1), nil
}

func (br *bitReader) SkipBits(n int) {
	if n <= br.bits {
		br.bits -= n
		return
	}

	n -= br.bits
	br.bits = 0

	for n >= 8 {
		br.fill()
		n -= 8
	}

	if n > 0 {
		br.fill()
		br.bits -= n
	}
}

func (br *bitReader) fill() error {
	var b [1]byte
	_, err := br.r.Read(b[:])
	if err != nil {
		br.err = err
		return err
	}

	br.buf = (br.buf << 8) | uint32(b[0])
	br.bits += 8
	return nil
}

// ParseCCITTFaxParams parses CCITT fax parameters from a Value
func ParseCCITTFaxParams(param Value) CCITTFaxParams {
	params := DefaultCCITTFaxParams()

	if param.Kind() == Null {
		return params
	}

	if k := param.Key("K"); k.Kind() == Integer {
		params.K = int(k.Int64())
	}
	if eol := param.Key("EndOfLine"); eol.Kind() == Bool {
		params.EndOfLine = eol.Bool()
	}
	if eba := param.Key("EncodedByteAlign"); eba.Kind() == Bool {
		params.EncodedByteAlign = eba.Bool()
	}
	if cols := param.Key("Columns"); cols.Kind() == Integer {
		params.Columns = int(cols.Int64())
	}
	if rows := param.Key("Rows"); rows.Kind() == Integer {
		params.Rows = int(rows.Int64())
	}
	if eob := param.Key("EndOfBlock"); eob.Kind() == Bool {
		params.EndOfBlock = eob.Bool()
	}
	if bi1 := param.Key("BlackIs1"); bi1.Kind() == Bool {
		params.BlackIs1 = bi1.Bool()
	}
	if drbe := param.Key("DamagedRowsBeforeError"); drbe.Kind() == Integer {
		params.DamagedRowsBeforeError = int(drbe.Int64())
	}

	return params
}

// JBIG2Decoder decodes JBIG2 encoded data
// JBIG2 is a complex format primarily used for scanned documents
// This implementation provides basic support for embedded JBIG2 streams
type JBIG2Decoder struct {
	r       io.Reader
	params  JBIG2Params
	globals []byte // Global segments from JBIG2Globals
	buf     *bytes.Buffer
	decoded bool
}

// JBIG2Params contains parameters for JBIG2 decoding
type JBIG2Params struct {
	Globals []byte // Data from JBIG2Globals stream
}

// NewJBIG2Decoder creates a new JBIG2 decoder
func NewJBIG2Decoder(r io.Reader, params JBIG2Params) *JBIG2Decoder {
	return &JBIG2Decoder{
		r:       r,
		params:  params,
		globals: params.Globals,
		buf:     new(bytes.Buffer),
	}
}

// Read implements io.Reader
func (d *JBIG2Decoder) Read(p []byte) (n int, err error) {
	if !d.decoded {
		if err := d.decode(); err != nil && err != io.EOF {
			return 0, err
		}
		d.decoded = true
	}

	return d.buf.Read(p)
}

func (d *JBIG2Decoder) decode() error {
	// Read all data first
	data, err := io.ReadAll(d.r)
	if err != nil {
		return err
	}

	// Prepend globals if present
	if len(d.globals) > 0 {
		data = append(d.globals, data...)
	}

	// Parse JBIG2 file header
	if len(data) < 4 {
		return fmt.Errorf("JBIG2 data too short")
	}

	// Check for file header signature (0x97 0x4A 0x42 0x32)
	hasFileHeader := len(data) >= 8 &&
		data[0] == 0x97 && data[1] == 0x4A &&
		data[2] == 0x42 && data[3] == 0x32

	offset := 0
	if hasFileHeader {
		// Skip file header (9 bytes minimum)
		offset = 9
		if len(data) > 4 && data[4]&0x01 != 0 {
			// Unknown page count - need to scan
			offset = 13
		}
	}

	// Parse segments
	for offset < len(data) {
		segOffset, segData, err := d.parseSegment(data[offset:])
		if err != nil {
			break // End of segments or error
		}
		offset += segOffset

		// Process segment data (simplified)
		d.buf.Write(segData)
	}

	return nil
}

func (d *JBIG2Decoder) parseSegment(data []byte) (int, []byte, error) {
	if len(data) < 7 {
		return 0, nil, io.EOF
	}

	// Segment number (4 bytes)
	// segNum := binary.BigEndian.Uint32(data[0:4])

	// Segment header flags (1 byte)
	flags := data[4]
	segType := flags & 0x3F

	// Check for end of file/page markers
	if segType == 50 || segType == 51 {
		return 7, nil, io.EOF
	}

	// Calculate header size (simplified)
	headerSize := 7
	if flags&0x40 != 0 {
		// Page association size is 4 bytes
		headerSize += 3
	}

	// Referred-to segment count
	refCount := int((data[5] >> 5) & 0x07)
	if refCount == 7 {
		// Extended count
		if len(data) < headerSize+4 {
			return 0, nil, io.EOF
		}
		refCount = int(binary.BigEndian.Uint32(data[headerSize:headerSize+4]) & 0x1FFFFFFF)
		headerSize += 4
	}

	// Skip referred segment numbers and page association
	headerSize += refCount * 4
	if flags&0x40 != 0 {
		headerSize += 4
	} else {
		headerSize += 1
	}

	// Data length (4 bytes)
	if len(data) < headerSize+4 {
		return 0, nil, io.EOF
	}
	dataLen := int(binary.BigEndian.Uint32(data[headerSize : headerSize+4]))
	headerSize += 4

	if dataLen == 0xFFFFFFFF {
		// Unknown length - need to scan for end
		return headerSize, nil, nil
	}

	if len(data) < headerSize+dataLen {
		return 0, nil, io.EOF
	}

	return headerSize + dataLen, data[headerSize : headerSize+dataLen], nil
}

// ParseJBIG2Params parses JBIG2 parameters from a Value
func ParseJBIG2Params(param Value) JBIG2Params {
	params := JBIG2Params{}

	if param.Kind() == Null {
		return params
	}

	// Get JBIG2Globals stream if present
	globals := param.Key("JBIG2Globals")
	if globals.Kind() == Stream {
		rc := globals.Reader()
		defer rc.Close()
		params.Globals, _ = io.ReadAll(rc)
	}

	return params
}

// LZWPredictor implements PNG prediction filters for LZW decoded data
type LZWPredictor struct {
	r             io.Reader
	predictor     int
	colors        int
	bpc           int // Bits per component
	columns       int
	rowBytes      int
	prevRow       []byte
	curRow        []byte
	buf           *bytes.Buffer
	bytesPerPixel int
}

// LZWPredictorParams contains parameters for LZW prediction
type LZWPredictorParams struct {
	Predictor int // 1=none, 2=TIFF, 10-15=PNG
	Colors    int // Number of color components (default: 1)
	BPC       int // Bits per component (default: 8)
	Columns   int // Pixels per row (default: 1)
}

// DefaultLZWPredictorParams returns default predictor parameters
func DefaultLZWPredictorParams() LZWPredictorParams {
	return LZWPredictorParams{
		Predictor: 1,
		Colors:    1,
		BPC:       8,
		Columns:   1,
	}
}

// NewLZWPredictor creates a new LZW predictor filter
func NewLZWPredictor(r io.Reader, params LZWPredictorParams) *LZWPredictor {
	if params.Colors < 1 {
		params.Colors = 1
	}
	if params.BPC < 1 {
		params.BPC = 8
	}
	if params.Columns < 1 {
		params.Columns = 1
	}

	bytesPerPixel := (params.Colors*params.BPC + 7) / 8
	rowBytes := (params.Columns*params.Colors*params.BPC + 7) / 8

	return &LZWPredictor{
		r:             r,
		predictor:     params.Predictor,
		colors:        params.Colors,
		bpc:           params.BPC,
		columns:       params.Columns,
		rowBytes:      rowBytes,
		prevRow:       make([]byte, rowBytes),
		curRow:        make([]byte, rowBytes),
		buf:           new(bytes.Buffer),
		bytesPerPixel: bytesPerPixel,
	}
}

// Read implements io.Reader
func (p *LZWPredictor) Read(b []byte) (n int, err error) {
	// Fill buffer if needed
	for p.buf.Len() < len(b) {
		if err := p.decodeRow(); err != nil {
			if err == io.EOF {
				break
			}
			return 0, err
		}
	}

	return p.buf.Read(b)
}

func (p *LZWPredictor) decodeRow() error {
	switch {
	case p.predictor == 1:
		// No prediction
		return p.readNoPredictor()

	case p.predictor == 2:
		// TIFF Predictor 2
		return p.readTIFFPredictor()

	case p.predictor >= 10 && p.predictor <= 15:
		// PNG prediction
		return p.readPNGPredictor()

	default:
		return fmt.Errorf("unsupported predictor: %d", p.predictor)
	}
}

func (p *LZWPredictor) readNoPredictor() error {
	n, err := io.ReadFull(p.r, p.curRow)
	if err != nil {
		return err
	}
	if n > 0 {
		p.buf.Write(p.curRow[:n])
	}
	return nil
}

func (p *LZWPredictor) readTIFFPredictor() error {
	_, err := io.ReadFull(p.r, p.curRow)
	if err != nil {
		return err
	}

	// TIFF Predictor 2: horizontal differencing
	// Each pixel is predicted based on the previous pixel
	for i := p.bytesPerPixel; i < len(p.curRow); i++ {
		p.curRow[i] += p.curRow[i-p.bytesPerPixel]
	}

	p.buf.Write(p.curRow)
	return nil
}

func (p *LZWPredictor) readPNGPredictor() error {
	// Read filter type byte
	var filterType [1]byte
	if _, err := io.ReadFull(p.r, filterType[:]); err != nil {
		return err
	}

	// Read row data
	if _, err := io.ReadFull(p.r, p.curRow); err != nil {
		return err
	}

	// Apply filter based on type
	switch filterType[0] {
	case 0: // None
		// Do nothing

	case 1: // Sub
		for i := p.bytesPerPixel; i < len(p.curRow); i++ {
			p.curRow[i] += p.curRow[i-p.bytesPerPixel]
		}

	case 2: // Up
		for i := 0; i < len(p.curRow); i++ {
			p.curRow[i] += p.prevRow[i]
		}

	case 3: // Average
		for i := 0; i < p.bytesPerPixel; i++ {
			p.curRow[i] += p.prevRow[i] / 2
		}
		for i := p.bytesPerPixel; i < len(p.curRow); i++ {
			p.curRow[i] += byte((int(p.curRow[i-p.bytesPerPixel]) + int(p.prevRow[i])) / 2)
		}

	case 4: // Paeth
		for i := 0; i < p.bytesPerPixel; i++ {
			p.curRow[i] += p.paethPredictor(0, p.prevRow[i], 0)
		}
		for i := p.bytesPerPixel; i < len(p.curRow); i++ {
			a := p.curRow[i-p.bytesPerPixel]
			b := p.prevRow[i]
			c := p.prevRow[i-p.bytesPerPixel]
			p.curRow[i] += p.paethPredictor(a, b, c)
		}
	}

	// Output filtered row
	p.buf.Write(p.curRow)

	// Save current row as previous for next iteration
	copy(p.prevRow, p.curRow)

	return nil
}

func (p *LZWPredictor) paethPredictor(a, b, c byte) byte {
	pa := absInt(int(b) - int(c))
	pb := absInt(int(a) - int(c))
	pc := absInt(int(a) + int(b) - 2*int(c))

	if pa <= pb && pa <= pc {
		return a
	} else if pb <= pc {
		return b
	}
	return c
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ParseLZWPredictorParams parses predictor parameters from a Value
func ParseLZWPredictorParams(param Value) LZWPredictorParams {
	params := DefaultLZWPredictorParams()

	if param.Kind() == Null {
		return params
	}

	if pred := param.Key("Predictor"); pred.Kind() == Integer {
		params.Predictor = int(pred.Int64())
	}
	if colors := param.Key("Colors"); colors.Kind() == Integer {
		params.Colors = int(colors.Int64())
	}
	if bpc := param.Key("BitsPerComponent"); bpc.Kind() == Integer {
		params.BPC = int(bpc.Int64())
	}
	if cols := param.Key("Columns"); cols.Kind() == Integer {
		params.Columns = int(cols.Int64())
	}

	return params
}
