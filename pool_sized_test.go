// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"testing"
)

func TestSizedBytePool(t *testing.T) {
	pool := NewSizedBytePool()

	tests := []struct {
		name     string
		size     int
		expected int // expected bucket size
	}{
		{"tiny", 8, 16},
		{"small", 16, 16},
		{"small2", 20, 32},
		{"medium", 100, 128},
		{"large", 500, 512},
		{"xlarge", 2000, 4096},
		{"max", 4096, 4096},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := pool.Get(tt.size)
			if cap(buf) < tt.size {
				t.Errorf("Get(%d) returned buffer with capacity %d, want at least %d",
					tt.size, cap(buf), tt.size)
			}
			if cap(buf) != tt.expected {
				t.Errorf("Get(%d) returned buffer with capacity %d, expected %d",
					tt.size, cap(buf), tt.expected)
			}
			if len(buf) != 0 {
				t.Errorf("Get(%d) returned buffer with length %d, want 0", tt.size, len(buf))
			}

			// Use the buffer
			for i := 0; i < tt.size && i < cap(buf); i++ {
				buf = append(buf, byte(i%256))
			}

			// Return to pool
			pool.Put(buf)

			// Get again and verify it's cleared
			buf2 := pool.Get(tt.size)
			if len(buf2) != 0 {
				t.Errorf("After Put/Get, buffer length = %d, want 0", len(buf2))
			}
		})
	}
}

func TestSizedBytePoolOversized(t *testing.T) {
	pool := NewSizedBytePool()

	// Request size larger than max bucket
	size := 10000
	buf := pool.Get(size)

	if cap(buf) < size {
		t.Errorf("Get(%d) returned buffer with capacity %d, want at least %d",
			size, cap(buf), size)
	}

	// Put should not panic for oversized buffers
	pool.Put(buf)
}

func TestSizedTextSlicePool(t *testing.T) {
	pool := NewSizedTextSlicePool()

	tests := []struct {
		name     string
		size     int
		expected int
	}{
		{"tiny", 4, 8},
		{"small", 10, 16},
		{"medium", 50, 64},
		{"large", 200, 256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slice := pool.Get(tt.size)
			if cap(slice) < tt.size {
				t.Errorf("Get(%d) returned slice with capacity %d, want at least %d",
					tt.size, cap(slice), tt.size)
			}
			if len(slice) != 0 {
				t.Errorf("Get(%d) returned slice with length %d, want 0", tt.size, len(slice))
			}

			// Use the slice
			for i := 0; i < tt.size && i < cap(slice); i++ {
				slice = append(slice, Text{S: "test"})
			}

			// Return to pool
			pool.Put(slice)

			// Get again and verify it's cleared
			slice2 := pool.Get(tt.size)
			if len(slice2) != 0 {
				t.Errorf("After Put/Get, slice length = %d, want 0", len(slice2))
			}
		})
	}
}

func TestGetBucketIndex(t *testing.T) {
	pool := NewSizedBytePool()

	tests := []struct {
		size     int
		expected int
	}{
		{1, 0},
		{16, 0},
		{17, 1},
		{32, 1},
		{33, 2},
		{64, 2},
		{65, 3},
		{128, 3},
		{129, 4},
		{256, 4},
		{257, 5},
		{512, 5},
		{513, 6},
		{1024, 6},
		{1025, 7},
		{4096, 7},
		{4097, 8},
		{10000, 8},
	}

	for _, tt := range tests {
		got := pool.getBucketIndex(tt.size)
		if got != tt.expected {
			t.Errorf("getBucketIndex(%d) = %d, want %d", tt.size, got, tt.expected)
		}
	}
}

func TestGlobalSizedBuffer(t *testing.T) {
	// Test global convenience functions
	buf := GetSizedBuffer(100)
	if cap(buf) < 100 {
		t.Errorf("GetSizedBuffer(100) capacity = %d, want at least 100", cap(buf))
	}

	buf = append(buf, []byte("test data")...)
	PutSizedBuffer(buf)

	// Should be able to get a buffer again
	buf2 := GetSizedBuffer(100)
	if cap(buf2) < 100 {
		t.Errorf("GetSizedBuffer(100) second call capacity = %d, want at least 100", cap(buf2))
	}
}

func TestGlobalSizedTextSlice(t *testing.T) {
	slice := GetSizedTextSlice(50)
	if cap(slice) < 50 {
		t.Errorf("GetSizedTextSlice(50) capacity = %d, want at least 50", cap(slice))
	}

	slice = append(slice, Text{S: "test"})
	PutSizedTextSlice(slice)

	slice2 := GetSizedTextSlice(50)
	if cap(slice2) < 50 {
		t.Errorf("GetSizedTextSlice(50) second call capacity = %d, want at least 50", cap(slice2))
	}
	if len(slice2) != 0 {
		t.Errorf("GetSizedTextSlice after Put/Get has length %d, want 0", len(slice2))
	}
}

func TestStringBuilderPool(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		{"small", 100},
		{"medium", 5000},
		{"large", 50000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := GetSizedStringBuilder(tt.size)
			if sb == nil {
				t.Fatal("GetSizedStringBuilder returned nil")
			}

			sb.WriteString("test data")
			if sb.Len() != 9 {
				t.Errorf("After WriteString, Len() = %d, want 9", sb.Len())
			}

			PutSizedStringBuilder(sb, tt.size)

			// Get again and verify reset
			sb2 := GetSizedStringBuilder(tt.size)
			if sb2.Len() != 0 {
				t.Errorf("After Put/Get, Len() = %d, want 0", sb2.Len())
			}
		})
	}
}

// Benchmark tests

func BenchmarkSizedBytePool(b *testing.B) {
	sizes := []int{16, 32, 64, 128, 256, 512, 1024, 4096}

	for _, size := range sizes {
		b.Run(string(rune(size)), func(b *testing.B) {
			pool := NewSizedBytePool()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				buf := pool.Get(size)
				buf = append(buf, make([]byte, size)...)
				pool.Put(buf)
			}
		})
	}
}

func BenchmarkDirectAllocation(b *testing.B) {
	sizes := []int{16, 32, 64, 128, 256, 512, 1024, 4096}

	for _, size := range sizes {
		b.Run(string(rune(size)), func(b *testing.B) {
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				buf := make([]byte, 0, size)
				buf = append(buf, make([]byte, size)...)
				_ = buf // prevent optimization
			}
		})
	}
}

func BenchmarkSizedBytePoolVsDirect(b *testing.B) {
	size := 128

	b.Run("Pool", func(b *testing.B) {
		pool := NewSizedBytePool()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			buf := pool.Get(size)
			for j := 0; j < size; j++ {
				buf = append(buf, byte(j))
			}
			pool.Put(buf)
		}
	})

	b.Run("Direct", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := make([]byte, 0, size)
			for j := 0; j < size; j++ {
				buf = append(buf, byte(j))
			}
		}
	})
}

func BenchmarkSizedTextSlicePool(b *testing.B) {
	sizes := []int{8, 16, 32, 64, 128, 256}

	for _, size := range sizes {
		b.Run(string(rune(size)), func(b *testing.B) {
			pool := NewSizedTextSlicePool()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				slice := pool.Get(size)
				for j := 0; j < size/2; j++ {
					slice = append(slice, Text{S: "test"})
				}
				pool.Put(slice)
			}
		})
	}
}

func BenchmarkStringBuilderPoolSizes(b *testing.B) {
	sizes := []int{100, 1000, 10000, 100000}

	for _, size := range sizes {
		b.Run(string(rune(size)), func(b *testing.B) {
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				sb := GetSizedStringBuilder(size)
				for j := 0; j < size/10; j++ {
					sb.WriteString("test data ")
				}
				_ = sb.String()
				PutSizedStringBuilder(sb, size)
			}
		})
	}
}

func BenchmarkPoolVsNonPool_RealWorldScenario(b *testing.B) {
	// Simulate real-world text extraction scenario

	b.Run("WithPool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Get buffers from pool
			buf1 := GetSizedBuffer(256)
			buf2 := GetSizedBuffer(512)
			textSlice := GetSizedTextSlice(32)

			// Simulate work
			for j := 0; j < 32; j++ {
				buf1 = append(buf1, byte(j))
				buf2 = append(buf2, byte(j*2))
				textSlice = append(textSlice, Text{S: "sample"})
			}

			// Return to pool
			PutSizedBuffer(buf1)
			PutSizedBuffer(buf2)
			PutSizedTextSlice(textSlice)
		}
	})

	b.Run("WithoutPool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Direct allocation
			buf1 := make([]byte, 0, 256)
			buf2 := make([]byte, 0, 512)
			textSlice := make([]Text, 0, 32)

			// Simulate work
			for j := 0; j < 32; j++ {
				buf1 = append(buf1, byte(j))
				buf2 = append(buf2, byte(j*2))
				textSlice = append(textSlice, Text{S: "sample"})
			}

			// No cleanup needed
			_ = buf1
			_ = buf2
			_ = textSlice
		}
	})
}
