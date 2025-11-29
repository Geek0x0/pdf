package pdf

import (
	"testing"
	"time"
)

// BenchmarkCFFRealFontParsing benchmarks parsing of real CFF font data
func BenchmarkCFFRealFontParsing(b *testing.B) {
	// Create a realistic CFF font data (simplified but representative)
	fontData := createTestCFFData()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		font, err := NewCFFFont(fontData)
		if err != nil {
			b.Fatal(err)
		}
		_ = font
	}
}

// BenchmarkCFFCharStringBatchDecoding benchmarks batch decoding of multiple charstrings
func BenchmarkCFFCharStringBatchDecoding(b *testing.B) {
	fontData := createTestCFFData()
	font, err := NewCFFFont(fontData)
	if err != nil {
		b.Fatal(err)
	}

	// Get all charstrings for batch processing
	charstrings := make([][]byte, 0, len(font.CharStrings.Data))
	for _, cs := range font.CharStrings.Data {
		charstrings = append(charstrings, cs)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for _, cs := range charstrings {
			decoder := NewCFFCharStringDecoder(cs)
			_, err := decoder.Decode()
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkCFFDictLookup benchmarks dictionary key lookups
func BenchmarkCFFDictLookup(b *testing.B) {
	fontData := createTestCFFData()
	font, err := NewCFFFont(fontData)
	if err != nil {
		b.Fatal(err)
	}

	keys := []int{0, 1, 18, 17} // FontName, FontMatrix, Private, CharStrings

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for _, key := range keys {
			_ = font.TopDict.Data[key]
		}
	}
}

// BenchmarkCFFMemoryFootprint benchmarks memory usage patterns
func BenchmarkCFFMemoryFootprint(b *testing.B) {
	fontData := createTestCFFData()

	b.ResetTimer()
	b.ReportAllocs()

	start := time.Now()
	for i := 0; i < b.N; i++ {
		font, err := NewCFFFont(fontData)
		if err != nil {
			b.Fatal(err)
		}
		_ = font
	}
	duration := time.Since(start)

	b.ReportMetric(float64(duration.Nanoseconds())/float64(b.N), "ns/op")
}

// createTestCFFData creates a minimal but valid CFF font data for benchmarking
func createTestCFFData() []byte {
	// Based on the working test data from font_cff_test.go
	// This creates a minimal valid CFF font structure
	return []byte{
		// CFF header: major=1, minor=0, hdrSize=4, offSize=4
		0x01, 0x00, 0x04, 0x04,

		// Name INDEX: count=1, offSize=4, offsets=[1, 9], data="TestFont"
		0x00, 0x00, 0x00, 0x01, // count=1
		0x04,                   // offSize=4
		0x00, 0x00, 0x00, 0x01, // offset[0]=1
		0x00, 0x00, 0x00, 0x09, // offset[1]=9
		'T', 'e', 's', 't', 'F', 'o', 'n', 't', // "TestFont"

		// Top DICT INDEX: count=1, offSize=4, offsets=[1, 25], dict data
		0x00, 0x00, 0x00, 0x01, // count=1
		0x04,                   // offSize=4
		0x00, 0x00, 0x00, 0x01, // offset[0]=1
		0x00, 0x00, 0x00, 0x19, // offset[1]=25 (dict length +1)
		// Dict data: version=1, CharStrings offset=1
		0x8B,       // integer 0 (version operand)
		0x00,       // version operator (0)
		0x8D,       // integer 2 (CharStrings operand)
		0x0C, 0x04, // CharStrings operator (12 4)

		// String INDEX: count=0 (empty)
		0x00, 0x00, 0x00, 0x00,

		// Global Subr INDEX: count=0 (empty)
		0x00, 0x00, 0x00, 0x00,

		// CharStrings INDEX: count=2, offSize=4, offsets=[1, 5, 10]
		0x00, 0x00, 0x00, 0x02, // count=2
		0x04,                   // offSize=4
		0x00, 0x00, 0x00, 0x01, // offset[0]=1
		0x00, 0x00, 0x00, 0x05, // offset[1]=5
		0x00, 0x00, 0x00, 0x0A, // offset[2]=10
		// CharString 1: simple moveto endchar
		0x8B, 0x14, 0x21, 0x0B, // moveto 100 50, endchar
		// CharString 2: moveto lineto endchar
		0x8B, 0x14, 0x8B, 0x28, 0x05, 0x0B, // moveto 100 50, lineto 100 100, endchar
	}
}
