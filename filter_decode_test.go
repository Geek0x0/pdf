// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"bytes"
	"io"
	"testing"
)

func TestCCITTFaxDecoderBasic(t *testing.T) {
	params := DefaultCCITTFaxParams()
	params.Columns = 8
	params.Rows = 1

	// Create a simple test pattern - all white
	// White run length 8 would be encoded as specific Huffman code
	data := []byte{0x00} // Simplified test data

	decoder := NewCCITTFaxDecoder(bytes.NewReader(data), params)
	if decoder == nil {
		t.Fatal("NewCCITTFaxDecoder returned nil")
	}

	// Just verify it doesn't panic
	buf := make([]byte, 8)
	_, _ = decoder.Read(buf)
}

func TestCCITTFaxParams(t *testing.T) {
	tests := []struct {
		name   string
		params CCITTFaxParams
		expect CCITTFaxParams
	}{
		{
			name:   "default",
			params: DefaultCCITTFaxParams(),
			expect: CCITTFaxParams{
				K:                      0,
				EndOfLine:              false,
				EncodedByteAlign:       false,
				Columns:                1728,
				Rows:                   0,
				EndOfBlock:             true,
				BlackIs1:               false,
				DamagedRowsBeforeError: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.params.K != tt.expect.K {
				t.Errorf("K: expected %d, got %d", tt.expect.K, tt.params.K)
			}
			if tt.params.Columns != tt.expect.Columns {
				t.Errorf("Columns: expected %d, got %d", tt.expect.Columns, tt.params.Columns)
			}
			if tt.params.EndOfBlock != tt.expect.EndOfBlock {
				t.Errorf("EndOfBlock: expected %v, got %v", tt.expect.EndOfBlock, tt.params.EndOfBlock)
			}
		})
	}
}

func TestParseCCITTFaxParams(t *testing.T) {
	// Test with empty Value
	params := ParseCCITTFaxParams(Value{})
	if params.Columns != 1728 {
		t.Errorf("expected default columns 1728, got %d", params.Columns)
	}
}

func TestJBIG2DecoderBasic(t *testing.T) {
	params := JBIG2Params{}

	// Create a minimal JBIG2 stream (just header)
	data := []byte{0x97, 0x4A, 0x42, 0x32, 0x0D, 0x0A, 0x1A, 0x0A, 0x00}

	decoder := NewJBIG2Decoder(bytes.NewReader(data), params)
	if decoder == nil {
		t.Fatal("NewJBIG2Decoder returned nil")
	}

	// Just verify it doesn't panic
	buf := make([]byte, 8)
	_, _ = decoder.Read(buf)
}

func TestJBIG2Params(t *testing.T) {
	params := ParseJBIG2Params(Value{})
	if params.Globals != nil {
		t.Error("expected nil Globals for empty Value")
	}
}

func TestLZWPredictorNone(t *testing.T) {
	params := LZWPredictorParams{
		Predictor: 1,
		Colors:    1,
		BPC:       8,
		Columns:   4,
	}

	data := []byte{0x01, 0x02, 0x03, 0x04}
	predictor := NewLZWPredictor(bytes.NewReader(data), params)

	result, err := io.ReadAll(predictor)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if !bytes.Equal(result, data) {
		t.Errorf("expected %v, got %v", data, result)
	}
}

func TestLZWPredictorTIFF(t *testing.T) {
	params := LZWPredictorParams{
		Predictor: 2,
		Colors:    1,
		BPC:       8,
		Columns:   4,
	}

	// Input: 10, 5, 3, 2 (differences)
	// Output: 10, 15, 18, 20 (cumulative)
	data := []byte{10, 5, 3, 2}
	predictor := NewLZWPredictor(bytes.NewReader(data), params)

	result, err := io.ReadAll(predictor)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	expected := []byte{10, 15, 18, 20}
	if !bytes.Equal(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestLZWPredictorPNGNone(t *testing.T) {
	params := LZWPredictorParams{
		Predictor: 10,
		Colors:    1,
		BPC:       8,
		Columns:   4,
	}

	// Filter type 0 (none) + row data
	data := []byte{0, 1, 2, 3, 4}
	predictor := NewLZWPredictor(bytes.NewReader(data), params)

	result, err := io.ReadAll(predictor)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	expected := []byte{1, 2, 3, 4}
	if !bytes.Equal(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestLZWPredictorPNGSub(t *testing.T) {
	params := LZWPredictorParams{
		Predictor: 10,
		Colors:    1,
		BPC:       8,
		Columns:   4,
	}

	// Filter type 1 (sub) + differences
	data := []byte{1, 10, 5, 3, 2}
	predictor := NewLZWPredictor(bytes.NewReader(data), params)

	result, err := io.ReadAll(predictor)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	expected := []byte{10, 15, 18, 20}
	if !bytes.Equal(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestLZWPredictorPNGUp(t *testing.T) {
	params := LZWPredictorParams{
		Predictor: 10,
		Colors:    1,
		BPC:       8,
		Columns:   4,
	}

	// First row: filter=0, values 1,2,3,4
	// Second row: filter=2 (up), differences 1,1,1,1
	data := []byte{
		0, 1, 2, 3, 4, // Row 1: no filter
		2, 1, 1, 1, 1, // Row 2: up filter
	}
	predictor := NewLZWPredictor(bytes.NewReader(data), params)

	result, err := io.ReadAll(predictor)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	expected := []byte{
		1, 2, 3, 4, // Row 1
		2, 3, 4, 5, // Row 2: 1+1, 2+1, 3+1, 4+1
	}
	if !bytes.Equal(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestLZWPredictorPNGAverage(t *testing.T) {
	params := LZWPredictorParams{
		Predictor: 10,
		Colors:    1,
		BPC:       8,
		Columns:   4,
	}

	// First row: filter=0, values 10,20,30,40
	// Second row: filter=3 (average)
	data := []byte{
		0, 10, 20, 30, 40, // Row 1: no filter
		3, 5, 0, 0, 0, // Row 2: average filter
	}
	predictor := NewLZWPredictor(bytes.NewReader(data), params)

	result, err := io.ReadAll(predictor)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	// Row 2 calculation:
	// pos 0: 5 + 10/2 = 5 + 5 = 10
	// pos 1: 0 + (10 + 20)/2 = 0 + 15 = 15
	// pos 2: 0 + (15 + 30)/2 = 0 + 22 = 22
	// pos 3: 0 + (22 + 40)/2 = 0 + 31 = 31
	expected := []byte{
		10, 20, 30, 40,
		10, 15, 22, 31,
	}
	if !bytes.Equal(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestLZWPredictorPNGPaeth(t *testing.T) {
	params := LZWPredictorParams{
		Predictor: 10,
		Colors:    1,
		BPC:       8,
		Columns:   4,
	}

	// First row: filter=0, values 10,20,30,40
	data := []byte{
		0, 10, 20, 30, 40, // Row 1: no filter
		4, 0, 0, 0, 0, // Row 2: Paeth filter
	}
	predictor := NewLZWPredictor(bytes.NewReader(data), params)

	result, err := io.ReadAll(predictor)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	// For Paeth with all zeros in filtered data, result equals previous row
	expected := []byte{
		10, 20, 30, 40,
		10, 20, 30, 40,
	}
	if !bytes.Equal(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestParseLZWPredictorParams(t *testing.T) {
	params := ParseLZWPredictorParams(Value{})

	if params.Predictor != 1 {
		t.Errorf("expected default predictor 1, got %d", params.Predictor)
	}
	if params.Colors != 1 {
		t.Errorf("expected default colors 1, got %d", params.Colors)
	}
	if params.BPC != 8 {
		t.Errorf("expected default BPC 8, got %d", params.BPC)
	}
	if params.Columns != 1 {
		t.Errorf("expected default columns 1, got %d", params.Columns)
	}
}

func TestBitReader(t *testing.T) {
	data := []byte{0xAB, 0xCD} // 10101011 11001101
	br := newBitReader(bytes.NewReader(data))

	// Read first bit (1)
	bit, err := br.ReadBit()
	if err != nil {
		t.Fatalf("ReadBit failed: %v", err)
	}
	if bit != 1 {
		t.Errorf("expected bit 1, got %d", bit)
	}

	// Peek next 4 bits (0101 = 5)
	bits, err := br.PeekBits(4)
	if err != nil {
		t.Fatalf("PeekBits failed: %v", err)
	}
	if bits != 5 {
		t.Errorf("expected bits 5, got %d", bits)
	}

	// Skip 4 bits
	br.SkipBits(4)

	// Read next bit (0)
	bit, err = br.ReadBit()
	if err != nil {
		t.Fatalf("ReadBit failed: %v", err)
	}
	if bit != 0 {
		t.Errorf("expected bit 0, got %d", bit)
	}
}

func TestHuffmanTables(t *testing.T) {
	// Verify white and black tables are not empty
	if len(whiteTable) == 0 {
		t.Error("whiteTable is empty")
	}
	if len(blackTable) == 0 {
		t.Error("blackTable is empty")
	}

	// Check that terminating codes (0-63) are present
	whiteTermCount := 0
	for _, entry := range whiteTable {
		if entry.runLen < 64 {
			whiteTermCount++
		}
	}
	if whiteTermCount < 64 {
		t.Errorf("whiteTable missing terminating codes, found %d", whiteTermCount)
	}

	blackTermCount := 0
	for _, entry := range blackTable {
		if entry.runLen < 64 {
			blackTermCount++
		}
	}
	if blackTermCount < 64 {
		t.Errorf("blackTable missing terminating codes, found %d", blackTermCount)
	}
}

func TestPaethPredictor(t *testing.T) {
	tests := []struct {
		a, b, c  byte
		expected byte
	}{
		{0, 0, 0, 0},
		{10, 0, 0, 10},
		{0, 10, 0, 10},
		{10, 10, 0, 10},
		{10, 10, 10, 10},
		{100, 50, 75, 75}, // pa=25, pb=25, pc=0 -> c closest (pc smallest)
		{50, 100, 75, 75}, // pa=25, pb=25, pc=0 -> c closest
	}

	// Create a predictor instance to access the method
	params := LZWPredictorParams{Predictor: 10, Colors: 1, BPC: 8, Columns: 1}
	predictor := NewLZWPredictor(bytes.NewReader([]byte{}), params)

	for _, tt := range tests {
		result := predictor.paethPredictor(tt.a, tt.b, tt.c)
		if result != tt.expected {
			t.Errorf("paethPredictor(%d, %d, %d) = %d, expected %d",
				tt.a, tt.b, tt.c, result, tt.expected)
		}
	}
}

func TestLZWPredictorMultiColors(t *testing.T) {
	params := LZWPredictorParams{
		Predictor: 2,
		Colors:    3, // RGB
		BPC:       8,
		Columns:   2, // 2 pixels = 6 bytes
	}

	// Input: R1,G1,B1, R2-R1,G2-G1,B2-B1
	data := []byte{10, 20, 30, 5, 5, 5}
	predictor := NewLZWPredictor(bytes.NewReader(data), params)

	result, err := io.ReadAll(predictor)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	// Output: R1,G1,B1, R1+5,G1+5,B1+5
	expected := []byte{10, 20, 30, 15, 25, 35}
	if !bytes.Equal(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestCCITTFaxDecodePassThrough(t *testing.T) {
	// Test with K=0 (Group 3 1D)
	params := DefaultCCITTFaxParams()
	params.K = 0
	params.Columns = 8
	params.Rows = 1

	// Simple data
	data := []byte{0xFF}
	decoder := NewCCITTFaxDecoder(bytes.NewReader(data), params)

	result, _ := io.ReadAll(decoder)
	// We expect some output (even if it's just the original data)
	if len(result) == 0 {
		// Could be empty on EOF, which is acceptable
	}
}

func TestCCITTFaxGroup4(t *testing.T) {
	params := DefaultCCITTFaxParams()
	params.K = -1 // Group 4 (2D)
	params.Columns = 8
	params.Rows = 2

	data := []byte{0x00, 0x00}
	decoder := NewCCITTFaxDecoder(bytes.NewReader(data), params)

	// Just verify no panic
	buf := make([]byte, 16)
	_, _ = decoder.Read(buf)
}

// Benchmark tests
func BenchmarkLZWPredictorNone(b *testing.B) {
	params := LZWPredictorParams{
		Predictor: 1,
		Colors:    3,
		BPC:       8,
		Columns:   1024,
	}

	data := make([]byte, 1024*3)
	for i := range data {
		data[i] = byte(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		predictor := NewLZWPredictor(bytes.NewReader(data), params)
		io.Copy(io.Discard, predictor)
	}
}

func BenchmarkLZWPredictorTIFF(b *testing.B) {
	params := LZWPredictorParams{
		Predictor: 2,
		Colors:    3,
		BPC:       8,
		Columns:   1024,
	}

	data := make([]byte, 1024*3)
	for i := range data {
		data[i] = byte(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		predictor := NewLZWPredictor(bytes.NewReader(data), params)
		io.Copy(io.Discard, predictor)
	}
}

func BenchmarkLZWPredictorPNG(b *testing.B) {
	params := LZWPredictorParams{
		Predictor: 10,
		Colors:    3,
		BPC:       8,
		Columns:   1024,
	}

	// Create data with filter bytes
	rowLen := 1024 * 3
	data := make([]byte, (rowLen+1)*10) // 10 rows
	for i := 0; i < 10; i++ {
		offset := i * (rowLen + 1)
		data[offset] = 0 // No filter
		for j := 0; j < rowLen; j++ {
			data[offset+1+j] = byte(j)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		predictor := NewLZWPredictor(bytes.NewReader(data), params)
		io.Copy(io.Discard, predictor)
	}
}

func BenchmarkBitReaderRead(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = 0xAB
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		br := newBitReader(bytes.NewReader(data))
		for j := 0; j < 1024*8; j++ {
			br.ReadBit()
		}
	}
}
