package pdf

import (
	"bytes"
	"io"
	"testing"
)

// BenchmarkCCITTFaxDecoder benchmarks basic CCITT fax decoding
func BenchmarkCCITTFaxDecoder(b *testing.B) {
	params := DefaultCCITTFaxParams()
	params.Columns = 8
	params.Rows = 1

	// Simple test data - just a single byte
	data := []byte{0x00}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoder := NewCCITTFaxDecoder(bytes.NewReader(data), params)
		buf := make([]byte, 8)
		decoder.Read(buf)
	}
}

// BenchmarkCCITTFaxDecoderLarge benchmarks CCITT fax decoding with larger data
func BenchmarkCCITTFaxDecoderLarge(b *testing.B) {
	params := DefaultCCITTFaxParams()
	params.Columns = 64
	params.Rows = 8

	// Larger test data
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

// BenchmarkCCITTFaxDecoderGroup4 benchmarks Group 4 (2D) CCITT fax decoding
func BenchmarkCCITTFaxDecoderGroup4(b *testing.B) {
	params := DefaultCCITTFaxParams()
	params.K = -1 // Group 4 (2D)
	params.Columns = 8
	params.Rows = 2

	data := []byte{0x00, 0x00}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoder := NewCCITTFaxDecoder(bytes.NewReader(data), params)
		buf := make([]byte, 16)
		decoder.Read(buf)
	}
}

// BenchmarkCCITTFaxDecoderMixed benchmarks mixed 1D/2D CCITT fax decoding
func BenchmarkCCITTFaxDecoderMixed(b *testing.B) {
	params := DefaultCCITTFaxParams()
	params.K = 1 // Mixed 1D/2D
	params.Columns = 8
	params.Rows = 2

	data := []byte{0x00, 0x00}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoder := NewCCITTFaxDecoder(bytes.NewReader(data), params)
		buf := make([]byte, 16)
		decoder.Read(buf)
	}
}
