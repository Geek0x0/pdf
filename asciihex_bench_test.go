package pdf

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// BenchmarkASCIIHexDecoder benchmarks the optimized ASCIIHexDecode implementation
func BenchmarkASCIIHexDecoder(b *testing.B) {
	sizes := []struct {
		name string
		size int
	}{
		{"Small_100B", 100},
		{"Medium_1KB", 1024},
		{"Large_10KB", 10 * 1024},
		{"XLarge_100KB", 100 * 1024},
		{"Huge_1MB", 1024 * 1024},
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			// Create hex string with the specified size (2 hex chars per byte)
			var hexBuf bytes.Buffer
			for i := 0; i < size.size; i++ {
				hexBuf.WriteString("48") // 'H'
			}
			hexBuf.WriteByte('>')
			hexData := hexBuf.Bytes()

			b.ResetTimer()
			b.SetBytes(int64(size.size))

			for i := 0; i < b.N; i++ {
				decoder := newASCIIHexDecoder(bytes.NewReader(hexData))
				_, err := io.Copy(io.Discard, decoder)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkASCIIHexDecoderWithSpaces benchmarks with whitespace
func BenchmarkASCIIHexDecoderWithSpaces(b *testing.B) {
	sizes := []struct {
		name string
		size int
	}{
		{"Small_100B", 100},
		{"Medium_1KB", 1024},
		{"Large_10KB", 10 * 1024},
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			// Create hex string with spaces between each byte
			var hexBuf bytes.Buffer
			for i := 0; i < size.size; i++ {
				hexBuf.WriteString("48 ") // 'H' with space
			}
			hexBuf.WriteByte('>')
			hexData := hexBuf.Bytes()

			b.ResetTimer()
			b.SetBytes(int64(size.size))

			for i := 0; i < b.N; i++ {
				decoder := newASCIIHexDecoder(bytes.NewReader(hexData))
				_, err := io.Copy(io.Discard, decoder)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkASCIIHexDecoderBuffered benchmarks buffered reading
func BenchmarkASCIIHexDecoderBuffered(b *testing.B) {
	// Create 10KB hex data
	var hexBuf bytes.Buffer
	for i := 0; i < 10*1024; i++ {
		hexBuf.WriteString("48")
	}
	hexBuf.WriteByte('>')
	hexData := hexBuf.Bytes()

	bufferSizes := []int{32, 64, 128, 256, 512, 1024, 4096}

	for _, bufSize := range bufferSizes {
		b.Run(string(rune(bufSize))+"B", func(b *testing.B) {
			b.SetBytes(10 * 1024)

			for i := 0; i < b.N; i++ {
				decoder := newASCIIHexDecoder(bytes.NewReader(hexData))
				buf := make([]byte, bufSize)
				total := 0
				for {
					n, err := decoder.Read(buf)
					total += n
					if err == io.EOF {
						break
					}
					if err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// BenchmarkASCIIHexDecoderVsHexString compares with hex string parsing
func BenchmarkASCIIHexDecoderVsHexString(b *testing.B) {
	// Create 1KB hex data
	hexContent := strings.Repeat("48", 1024)
	hexWithMarker := hexContent + ">"

	b.Run("ASCIIHexDecoder", func(b *testing.B) {
		b.SetBytes(1024)
		for i := 0; i < b.N; i++ {
			decoder := newASCIIHexDecoder(strings.NewReader(hexWithMarker))
			_, err := io.Copy(io.Discard, decoder)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ReadHexString", func(b *testing.B) {
		b.SetBytes(1024)
		for i := 0; i < b.N; i++ {
			// Simulate buffer hex string reading (for comparison)
			hexData := "<" + hexContent + ">"
			buf := newBuffer(strings.NewReader(hexData), 0)
			buf.readByte() // consume '<'
			_ = buf.readHexStringSIMDAdvanced()
		}
	})
}

// BenchmarkASCIIHexDecoderMixedCase benchmarks mixed case performance
func BenchmarkASCIIHexDecoderMixedCase(b *testing.B) {
	// Create mixed case hex data
	var hexBuf bytes.Buffer
	for i := 0; i < 1024; i++ {
		if i%2 == 0 {
			hexBuf.WriteString("4A") // uppercase
		} else {
			hexBuf.WriteString("6b") // lowercase
		}
	}
	hexBuf.WriteByte('>')
	hexData := hexBuf.Bytes()

	b.SetBytes(1024)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		decoder := newASCIIHexDecoder(bytes.NewReader(hexData))
		_, err := io.Copy(io.Discard, decoder)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkASCIIHexDecoderOddLength benchmarks odd-length hex strings
func BenchmarkASCIIHexDecoderOddLength(b *testing.B) {
	// Create odd-length hex data (2049 hex chars = 1024.5 bytes)
	var hexBuf bytes.Buffer
	for i := 0; i < 1024; i++ {
		hexBuf.WriteString("48")
	}
	hexBuf.WriteString("4") // One more hex digit
	hexBuf.WriteByte('>')
	hexData := hexBuf.Bytes()

	b.SetBytes(1025)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		decoder := newASCIIHexDecoder(bytes.NewReader(hexData))
		result, err := io.ReadAll(decoder)
		if err != nil {
			b.Fatal(err)
		}
		// 2049 hex chars = 1024 full bytes + 1 half byte (padded) = 1025 bytes output
		if len(result) != 1025 {
			b.Fatalf("expected 1025 bytes, got %d", len(result))
		}
	}
}
