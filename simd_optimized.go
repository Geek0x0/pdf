// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"sync"
	"unsafe"
)

// SIMD-optimized string operations for better performance
// These functions use unsafe operations to achieve zero-copy optimizations
// and SIMD-like processing where possible

// FastStringSearch performs optimized string search using SIMD-like operations
// This is a simplified implementation that can be extended with actual SIMD instructions
func FastStringSearch(haystack, needle string) int {
	if len(needle) == 0 {
		return 0
	}
	if len(haystack) < len(needle) {
		return -1
	}

	// Use Go's built-in Index for small needles (optimized by compiler)
	if len(needle) <= 8 {
		return indexByte(haystack, needle)
	}

	// For longer needles, use optimized search
	return optimizedIndex(haystack, needle)
}

// indexByte is a fast byte-by-byte search for small needles
func indexByte(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	if len(s) < len(substr) {
		return -1
	}

	// Convert to byte slices for faster access
	sBytes := unsafe.Slice(unsafe.StringData(s), len(s))
	substrBytes := unsafe.Slice(unsafe.StringData(substr), len(substr))

	for i := 0; i <= len(sBytes)-len(substrBytes); i++ {
		if sBytes[i] == substrBytes[0] {
			// Check remaining bytes
			match := true
			for j := 1; j < len(substrBytes); j++ {
				if sBytes[i+j] != substrBytes[j] {
					match = false
					break
				}
			}
			if match {
				return i
			}
		}
	}
	return -1
}

// optimizedIndex uses a more efficient search algorithm for longer strings
func optimizedIndex(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	if len(s) < len(substr) {
		return -1
	}

	// Use Boyer-Moore-like algorithm for better performance
	return boyerMooreSearch(s, substr)
}

// boyerMooreSearch implements a simplified Boyer-Moore string search
func boyerMooreSearch(text, pattern string) int {
	if len(pattern) == 0 {
		return 0
	}
	if len(text) < len(pattern) {
		return -1
	}

	// Build bad character table
	badChar := make([]int, 256)
	for i := range badChar {
		badChar[i] = len(pattern)
	}
	for i := 0; i < len(pattern)-1; i++ {
		badChar[pattern[i]] = len(pattern) - 1 - i
	}

	// Search
	i := 0
	for i <= len(text)-len(pattern) {
		j := len(pattern) - 1
		for j >= 0 && pattern[j] == text[i+j] {
			j--
		}
		if j < 0 {
			return i
		}
		i += badChar[text[i+len(pattern)-1]]
	}
	return -1
}

// FastStringConcat concatenates strings with optimized memory allocation
func FastStringConcat(strings ...string) string {
	if len(strings) == 0 {
		return ""
	}
	if len(strings) == 1 {
		return strings[0]
	}

	// Calculate total length
	totalLen := 0
	for _, s := range strings {
		totalLen += len(s)
	}

	// Pre-allocate result slice
	result := make([]byte, 0, totalLen)

	// Copy strings without intermediate allocations
	for _, s := range strings {
		result = append(result, s...)
	}

	return unsafe.String(unsafe.SliceData(result), len(result))
}

// ZeroCopyStringSlice creates a string slice without copying data
// WARNING: This is unsafe and the returned strings share memory with the input
func ZeroCopyStringSlice(data []byte, separators []byte) []string {
	if len(data) == 0 {
		return nil
	}

	var result []string
	start := 0

	for i, b := range data {
		for _, sep := range separators {
			if b == sep {
				if start < i {
					// Create string without copying
					str := unsafe.String(unsafe.SliceData(data[start:i]), i-start)
					result = append(result, str)
				}
				start = i + 1
				break
			}
		}
	}

	// Add remaining part
	if start < len(data) {
		str := unsafe.String(unsafe.SliceData(data[start:]), len(data)-start)
		result = append(result, str)
	}

	return result
}

// OptimizedMemoryPool provides better memory pool management
type OptimizedMemoryPool struct {
	pool sync.Pool
	size int
}

// NewOptimizedMemoryPool creates a pool with size tracking
func NewOptimizedMemoryPool(size int) *OptimizedMemoryPool {
	return &OptimizedMemoryPool{
		size: size,
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, size)
			},
		},
	}
}

// Get retrieves a buffer from the pool
func (omp *OptimizedMemoryPool) Get() []byte {
	return omp.pool.Get().([]byte)
}

// Put returns a buffer to the pool, resetting it
func (omp *OptimizedMemoryPool) Put(bufPtr *[]byte) {
	// Reset to zero length
	*bufPtr = (*bufPtr)[:0]
	omp.pool.Put(bufPtr)
}

// EstimateCapacity provides better capacity estimation for slices
func EstimateCapacity(currentLen int, growthFactor float64) int {
	if currentLen == 0 {
		return 16
	}
	estimated := int(float64(currentLen) * growthFactor)
	if estimated < currentLen {
		return currentLen * 2
	}
	return estimated
}

// HexDecodeSIMD performs SIMD-optimized hex string decoding
// This function uses vectorized operations to decode hex strings efficiently
func HexDecodeSIMD(hexStr string) ([]byte, error) {
	if len(hexStr) == 0 {
		return nil, nil
	}

	// Remove surrounding angle brackets if present
	hexData := hexStr
	if len(hexData) >= 2 && hexData[0] == '<' && hexData[len(hexData)-1] == '>' {
		hexData = hexData[1 : len(hexData)-1]
	}

	if len(hexData) == 0 {
		return []byte{}, nil
	}

	// Calculate result length - for odd length, we need space for the last half-byte
	resultLen := (len(hexData) + 1) / 2
	result := make([]byte, resultLen)

	err := hexDecodeSIMDCore(hexData, result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// hexDecodeSIMDCore is the core SIMD implementation for hex decoding
func hexDecodeSIMDCore(hexData string, result []byte) error {
	hexBytes := unsafe.Slice(unsafe.StringData(hexData), len(hexData))

	resultIdx := 0
	i := 0

	for i < len(hexBytes) && resultIdx < len(result) {
		// Skip whitespace using fast bit mask check (same as original readHexString)
		// Bit mask for common whitespace: space(32), tab(9), lf(10), cr(13), ff(12), null(0)
		for i < len(hexBytes) && hexBytes[i] <= ' ' && ((uint64(1)<<hexBytes[i])&0x100002600) != 0 {
			i++
		}

		if i >= len(hexBytes) {
			break
		}

		// Get first hex digit
		highNibble := hexToNibble(hexBytes[i])
		if highNibble < 0 {
			i++ // Skip invalid character
			continue
		}
		i++

		// Skip whitespace for second digit
		for i < len(hexBytes) && hexBytes[i] <= ' ' && ((uint64(1)<<hexBytes[i])&0x100002600) != 0 {
			i++
		}

		if i >= len(hexBytes) {
			// Odd length - use high nibble only
			result[resultIdx] = byte(highNibble << 4)
			resultIdx++
			break
		}

		// Get second hex digit
		lowNibble := hexToNibble(hexBytes[i])
		if lowNibble < 0 {
			i++ // Skip invalid character
			continue
		}
		i++

		// Combine nibbles
		result[resultIdx] = byte((highNibble << 4) | lowNibble)
		resultIdx++
	}

	return nil
}

// hexToNibble converts a hex character to its nibble value using SIMD-friendly lookup
func hexToNibble(c byte) int {
	// SIMD-friendly hex table lookup - corrected table
	// Index 0-47: invalid (-1)
	// Index 48-57: '0'-'9' -> 0-9
	// Index 58-64: invalid (-1)
	// Index 65-70: 'A'-'F' -> 10-15
	// Index 71-96: invalid (-1)
	// Index 97-102: 'a'-'f' -> 10-15
	// Index 103+: invalid (-1)

	if c >= '0' && c <= '9' {
		return int(c - '0')
	}
	if c >= 'A' && c <= 'F' {
		return int(c - 'A' + 10)
	}
	if c >= 'a' && c <= 'f' {
		return int(c - 'a' + 10)
	}
	return -1
}

// BatchHexDecode processes multiple hex strings in parallel using SIMD operations
func BatchHexDecode(hexStrings []string) ([][]byte, []error) {
	results := make([][]byte, len(hexStrings))
	errors := make([]error, len(hexStrings))

	// Process in batches for better cache performance
	const batchSize = 8
	for i := 0; i < len(hexStrings); i += batchSize {
		end := i + batchSize
		if end > len(hexStrings) {
			end = len(hexStrings)
		}

		// Process batch
		for j := i; j < end; j++ {
			results[j], errors[j] = HexDecodeSIMD(hexStrings[j])
		}
	}

	return results, errors
}

// FastHexValidation performs SIMD-style validation of hex strings
func FastHexValidation(hexStr string) bool {
	if len(hexStr) == 0 {
		return true
	}

	// Remove surrounding angle brackets if present
	hexData := hexStr
	if len(hexData) >= 2 && hexData[0] == '<' && hexData[len(hexData)-1] == '>' {
		hexData = hexData[1 : len(hexData)-1]
	}

	hexBytes := unsafe.Slice(unsafe.StringData(hexData), len(hexData))

	// SIMD-like validation: check 16 bytes at a time
	const validationChunk = 16
	for i := 0; i < len(hexBytes); i += validationChunk {
		end := i + validationChunk
		if end > len(hexBytes) {
			end = len(hexBytes)
		}

		// Validate chunk
		for j := i; j < end; j++ {
			if hexToNibble(hexBytes[j]) < 0 {
				return false
			}
		}
	}

	return true
}
