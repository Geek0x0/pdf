// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"math/bits"
	"sync"
)

// SizedBytePool implements a multi-level size-bucketed object pool
// for byte slices. It reduces memory allocation overhead by reusing
// buffers of appropriate sizes.
//
// Size buckets: 16B, 32B, 64B, 128B, 256B, 512B, 1KB, 4KB
type SizedBytePool struct {
	pools [8]*sync.Pool
	sizes [8]int
}

// Global sized byte pool instance
var globalSizedBytePool = NewSizedBytePool()

// NewSizedBytePool creates a new sized byte pool with 8 size buckets
func NewSizedBytePool() *SizedBytePool {
	sp := &SizedBytePool{
		sizes: [8]int{16, 32, 64, 128, 256, 512, 1024, 4096},
	}

	// Initialize each pool with a factory function
	for i := 0; i < 8; i++ {
		size := sp.sizes[i]
		sp.pools[i] = &sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 0, size)
				return &buf
			},
		}
	}

	return sp
}

// Get retrieves a byte slice from the appropriate size bucket
// Returns a buffer with at least the requested capacity
func (sp *SizedBytePool) Get(size int) []byte {
	if size <= 0 {
		size = 16 // minimum size
	}

	idx := sp.getBucketIndex(size)
	if idx >= len(sp.pools) {
		// Size exceeds largest bucket, allocate directly
		return make([]byte, 0, size)
	}

	bufPtr := sp.pools[idx].Get().(*[]byte)
	buf := *bufPtr

	// Ensure the buffer has the requested capacity
	if cap(buf) < size {
		// This shouldn't happen, but handle it gracefully
		return make([]byte, 0, size)
	}

	return buf[:0] // Reset length to 0, keep capacity
}

// Put returns a byte slice to the appropriate pool
// The slice is cleared before being returned to the pool
func (sp *SizedBytePool) Put(buf []byte) {
	if buf == nil {
		return
	}

	capacity := cap(buf)
	idx := sp.getBucketIndex(capacity)

	// Only pool if it fits in one of our buckets
	if idx < len(sp.pools) && capacity == sp.sizes[idx] {
		buf = buf[:0] // Clear the slice
		sp.pools[idx].Put(&buf)
	}
	// Otherwise, let GC handle it
}

// getBucketIndex returns the appropriate bucket index for a given size
// Uses bit manipulation for fast log2 calculation
func (sp *SizedBytePool) getBucketIndex(size int) int {
	if size <= 16 {
		return 0
	}
	if size > 4096 {
		return 8 // exceeds max bucket
	}

	// Calculate log2 and adjust for our bucket sizes
	// sizes[i] = 16 * 2^i, so log2(size/16) gives us the index
	// For exact power of 2 sizes, we need to use the same bucket
	idx := bits.Len(uint(size-1)) - 4
	if idx >= 8 {
		return 7 // clamp to max bucket index
	}
	return idx
}

// GetStats returns statistics about pool usage (for debugging/monitoring)
type PoolStats struct {
	BucketSize int
	InUse      int // approximation, not perfectly accurate
}

// GetSizedBuffer retrieves a byte buffer from the global sized pool
// This is a convenience function for common use cases
func GetSizedBuffer(size int) []byte {
	return globalSizedBytePool.Get(size)
}

// PutSizedBuffer returns a byte buffer to the global sized pool
// This is a convenience function for common use cases
func PutSizedBuffer(buf []byte) {
	globalSizedBytePool.Put(buf)
}

// SizedTextSlicePool implements a size-bucketed pool for Text slices
// Similar to SizedBytePool but for []Text instead of []byte
type SizedTextSlicePool struct {
	pools [6]*sync.Pool
	sizes [6]int
}

// Global sized text slice pool instance
var globalSizedTextSlicePool = NewSizedTextSlicePool()

// NewSizedTextSlicePool creates a new sized text slice pool
// Buckets: 8, 16, 32, 64, 128, 256 texts
func NewSizedTextSlicePool() *SizedTextSlicePool {
	sp := &SizedTextSlicePool{
		sizes: [6]int{8, 16, 32, 64, 128, 256},
	}

	for i := 0; i < 6; i++ {
		size := sp.sizes[i]
		sp.pools[i] = &sync.Pool{
			New: func() interface{} {
				slice := make([]Text, 0, size)
				return &slice
			},
		}
	}

	return sp
}

// Get retrieves a Text slice from the appropriate size bucket
func (sp *SizedTextSlicePool) Get(size int) []Text {
	if size <= 0 {
		size = 8
	}

	idx := sp.getBucketIndex(size)
	if idx >= len(sp.pools) {
		return make([]Text, 0, size)
	}

	slicePtr := sp.pools[idx].Get().(*[]Text)
	slice := *slicePtr
	return slice[:0]
}

// Put returns a Text slice to the appropriate pool
func (sp *SizedTextSlicePool) Put(slice []Text) {
	if slice == nil {
		return
	}

	capacity := cap(slice)
	idx := sp.getBucketIndex(capacity)

	if idx < len(sp.pools) && capacity == sp.sizes[idx] {
		// Clear the slice elements to avoid memory leaks
		for i := range slice {
			slice[i] = Text{}
		}
		slice = slice[:0]
		sp.pools[idx].Put(&slice)
	}
}

// getBucketIndex returns the bucket index for Text slices
func (sp *SizedTextSlicePool) getBucketIndex(size int) int {
	if size <= 8 {
		return 0
	}
	if size > 256 {
		return 6
	}

	// Find the smallest bucket that fits
	for i, bucketSize := range sp.sizes {
		if size <= bucketSize {
			return i
		}
	}
	return len(sp.sizes)
}

// GetSizedTextSlice retrieves a Text slice from the global pool
func GetSizedTextSlice(size int) []Text {
	return globalSizedTextSlicePool.Get(size)
}

// PutSizedTextSlice returns a Text slice to the global pool
func PutSizedTextSlice(slice []Text) {
	globalSizedTextSlicePool.Put(slice)
}

// StringBuilderPool provides size-aware string builder pooling
type StringBuilderPool struct {
	small  sync.Pool // < 1KB
	medium sync.Pool // 1KB - 16KB
	large  sync.Pool // > 16KB
}

// Global string builder pool
var globalStringBuilderPool = &StringBuilderPool{
	small: sync.Pool{
		New: func() interface{} {
			sb := &FastStringBuilder{buf: make([]byte, 0, 512)}
			return sb
		},
	},
	medium: sync.Pool{
		New: func() interface{} {
			sb := &FastStringBuilder{buf: make([]byte, 0, 8192)}
			return sb
		},
	},
	large: sync.Pool{
		New: func() interface{} {
			sb := &FastStringBuilder{buf: make([]byte, 0, 65536)}
			return sb
		},
	},
}

// GetSizedStringBuilder retrieves a string builder from the appropriate pool
func GetSizedStringBuilder(estimatedSize int) *FastStringBuilder {
	var sb *FastStringBuilder

	switch {
	case estimatedSize < 1024:
		if obj := globalStringBuilderPool.small.Get(); obj != nil {
			sb, _ = obj.(*FastStringBuilder)
		}
	case estimatedSize < 16384:
		if obj := globalStringBuilderPool.medium.Get(); obj != nil {
			sb, _ = obj.(*FastStringBuilder)
		}
	default:
		if obj := globalStringBuilderPool.large.Get(); obj != nil {
			sb, _ = obj.(*FastStringBuilder)
		}
	}

	// Defensive check: ensure builder is valid
	if sb == nil || sb.buf == nil {
		// Pool returned nil or invalid builder, create new one
		sb = NewFastStringBuilder(estimatedSize)
	} else {
		// Safe reset: ensure buf is valid before resetting
		if sb.buf != nil {
			sb.buf = sb.buf[:0]
		} else {
			sb.buf = make([]byte, 0, 512)
		}
	}
	return sb
}

// PutSizedStringBuilder returns a string builder to the appropriate pool
func PutSizedStringBuilder(sb *FastStringBuilder, estimatedSize int) {
	if sb == nil {
		return
	}

	sb.Reset()

	switch {
	case estimatedSize < 1024:
		globalStringBuilderPool.small.Put(sb)
	case estimatedSize < 16384:
		globalStringBuilderPool.medium.Put(sb)
	default:
		globalStringBuilderPool.large.Put(sb)
	}
}
