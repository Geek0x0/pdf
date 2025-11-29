package pdf

import (
	"bytes"
	"io"
	"testing"
)

// BenchmarkCCITTFaxDecoderOptimized benchmarks the optimized CCITT fax decoder
func BenchmarkCCITTFaxDecoderOptimized(b *testing.B) {
	params := DefaultCCITTFaxParams()
	params.Columns = 8
	params.Rows = 1

	// Simple test data - just a single byte (same as original)
	data := []byte{0x00}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoder := NewCCITTFaxDecoder(bytes.NewReader(data), params)
		buf := make([]byte, 8)
		decoder.Read(buf)
	}
}

// BenchmarkCCITTFaxDecoderLargeOptimized benchmarks with larger data
func BenchmarkCCITTFaxDecoderLargeOptimized(b *testing.B) {
	params := DefaultCCITTFaxParams()
	params.Columns = 64
	params.Rows = 8

	// Larger test data (same as original)
	data := make([]byte, 64)
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoder := NewCCITTFaxDecoder(bytes.NewReader(data), params)
		buf := make([]byte, 64*8)
		io.ReadFull(decoder, buf)
	}
}

// BenchmarkCCITTFaxDecoderGroup4Optimized benchmarks Group 4 (pure 2D) mode
func BenchmarkCCITTFaxDecoderGroup4Optimized(b *testing.B) {
	params := DefaultCCITTFaxParams()
	params.K = -1 // Group 4 (2D)
	params.Columns = 8
	params.Rows = 2

	data := []byte{0x00, 0x00} // Same as original

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoder := NewCCITTFaxDecoder(bytes.NewReader(data), params)
		buf := make([]byte, 16)
		decoder.Read(buf)
	}
}

// BenchmarkCCITTFaxDecoderMixedOptimized benchmarks mixed 1D/2D mode
func BenchmarkCCITTFaxDecoderMixedOptimized(b *testing.B) {
	params := DefaultCCITTFaxParams()
	params.K = 1 // Mixed 1D/2D
	params.Columns = 8
	params.Rows = 2

	data := []byte{0x00, 0x00} // Same as original

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoder := NewCCITTFaxDecoder(bytes.NewReader(data), params)
		buf := make([]byte, 16)
		decoder.Read(buf)
	}
}
