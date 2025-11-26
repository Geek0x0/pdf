// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"strings"
	"testing"
)

// BenchmarkReadHexString benchmarks hex string parsing performance
func BenchmarkReadHexString(b *testing.B) {
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
			hexContent := strings.Repeat("48", size.size) // 'H' repeated
			hexString := "<" + hexContent + ">"

			b.ResetTimer()
			b.SetBytes(int64(size.size))

			for i := 0; i < b.N; i++ {
				buf := newBuffer(strings.NewReader(hexString), 0)
				buf.readByte() // consume '<'
				tok := buf.readHexStringSIMDAdvanced()
				if tok == nil {
					b.Fatal("unexpected nil token")
				}
				result := tok.(string)
				if len(result) != size.size {
					b.Fatalf("expected %d bytes, got %d", size.size, len(result))
				}
			}
		})
	}
}

// BenchmarkReadHexStringWithSpaces benchmarks hex string with whitespace
func BenchmarkReadHexStringWithSpaces(b *testing.B) {
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
			var builder strings.Builder
			builder.WriteByte('<')
			for i := 0; i < size.size; i++ {
				builder.WriteString("48 ") // 'H' with space
			}
			builder.WriteByte('>')
			hexString := builder.String()

			b.ResetTimer()
			b.SetBytes(int64(size.size))

			for i := 0; i < b.N; i++ {
				buf := newBuffer(strings.NewReader(hexString), 0)
				buf.readByte() // consume '<'
				tok := buf.readHexStringSIMDAdvanced()
				if tok == nil {
					b.Fatal("unexpected nil token")
				}
				result := tok.(string)
				if len(result) != size.size {
					b.Fatalf("expected %d bytes, got %d", size.size, len(result))
				}
			}
		})
	}
}

// BenchmarkUnhex benchmarks the hex digit lookup
func BenchmarkUnhex(b *testing.B) {
	testBytes := []byte("0123456789abcdefABCDEF")

	b.Run("LookupTable", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, c := range testBytes {
				_ = unhex(c)
			}
		}
	})
}

// BenchmarkHexTableLookup benchmarks direct table lookup
func BenchmarkHexTableLookup(b *testing.B) {
	testBytes := []byte("0123456789abcdefABCDEF")

	b.Run("DirectLookup", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, c := range testBytes {
				_ = hexTable[c]
			}
		}
	})
}
