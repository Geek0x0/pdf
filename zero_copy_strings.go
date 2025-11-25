// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"unsafe"
)

// ZeroCopyString provides zero-copy string operations
// Note: These operations need to be used carefully because they bypass Go's type safety

// BytesToString zero-copy conversion from []byte to string
// Warning: The returned string directly references the underlying byte array, do not modify the original []byte
func BytesToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return *(*string)(unsafe.Pointer(&b))
}

// StringToBytes zero-copy conversion from string to []byte
// Warning: The returned []byte is read-only, do not modify
func StringToBytes(s string) []byte {
	if s == "" {
		return nil
	}

	// Use unsafe for direct conversion
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// SubstringZeroCopy zero-copy substring extraction
// Actually all string slicing in Go is already zero-copy
func SubstringZeroCopy(s string, start, end int) string {
	if start < 0 || end > len(s) || start > end {
		return ""
	}
	return s[start:end]
}

// StringBuffer string building buffer, optimizes multiple concatenations
type StringBuffer struct {
	buf []byte
}

// NewStringBuffer create new string buffer
func NewStringBuffer(capacity int) *StringBuffer {
	return &StringBuffer{
		buf: make([]byte, 0, capacity),
	}
}

// WriteString write string
func (sb *StringBuffer) WriteString(s string) {
	sb.buf = append(sb.buf, s...)
}

// WriteByte write single byte
func (sb *StringBuffer) WriteByte(b byte) error {
	sb.buf = append(sb.buf, b)
	return nil
}

// WriteBytes write byte slice
func (sb *StringBuffer) WriteBytes(b []byte) {
	sb.buf = append(sb.buf, b...)
}

// String zero-copy return string
// Warning: Do not use StringBuffer after return
func (sb *StringBuffer) String() string {
	return BytesToString(sb.buf)
}

// StringCopy safely return string copy
func (sb *StringBuffer) StringCopy() string {
	return string(sb.buf)
}

// Len return current length
func (sb *StringBuffer) Len() int {
	return len(sb.buf)
}

// Cap return capacity
func (sb *StringBuffer) Cap() int {
	return cap(sb.buf)
}

// Reset reset buffer
func (sb *StringBuffer) Reset() {
	sb.buf = sb.buf[:0]
}

// Bytes return underlying byte slice
func (sb *StringBuffer) Bytes() []byte {
	return sb.buf
}

// StringPool string pool, reuse common strings
type StringPool struct {
	pool map[string]string
}

// NewStringPool create new string pool
func NewStringPool() *StringPool {
	return &StringPool{
		pool: make(map[string]string),
	}
}

// Intern add string to pool and return pooled version
// Strings with same content will share memory
func (sp *StringPool) Intern(s string) string {
	if cached, ok := sp.pool[s]; ok {
		return cached
	}
	// Create new copy and store
	sp.pool[s] = s
	return s
}

// Clear clear pool
func (sp *StringPool) Clear() {
	sp.pool = make(map[string]string)
}

// Size return number of strings in pool
func (sp *StringPool) Size() int {
	return len(sp.pool)
}

// InplaceStringBuilder in-place string builder
// Avoid intermediate allocations
type InplaceStringBuilder struct {
	parts  []string
	length int
}

// NewInplaceStringBuilder create new in-place string builder
func NewInplaceStringBuilder(capacity int) *InplaceStringBuilder {
	return &InplaceStringBuilder{
		parts: make([]string, 0, capacity),
	}
}

// Append append string
func (isb *InplaceStringBuilder) Append(s string) {
	isb.parts = append(isb.parts, s)
	isb.length += len(s)
}

// Build build final string (single allocation)
func (isb *InplaceStringBuilder) Build() string {
	if len(isb.parts) == 0 {
		return ""
	}
	if len(isb.parts) == 1 {
		return isb.parts[0]
	}

	// Allocate sufficient space at once
	buf := make([]byte, 0, isb.length)
	for _, part := range isb.parts {
		buf = append(buf, part...)
	}
	return BytesToString(buf)
}

// Reset reset builder
func (isb *InplaceStringBuilder) Reset() {
	isb.parts = isb.parts[:0]
	isb.length = 0
}

// Len return total length
func (isb *InplaceStringBuilder) Len() int {
	return isb.length
}

// StringInterning global string interning
var globalStringPool = NewStringPool()

// InternString adds string to global pool
func InternString(s string) string {
	return globalStringPool.Intern(s)
}

// ClearGlobalStringPool clears the global string pool
func ClearGlobalStringPool() {
	globalStringPool.Clear()
}

// FastStringConcatZC fast concatenation of multiple strings (zero-copy version)
func FastStringConcatZC(parts ...string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}

	// Calculate total length
	totalLen := 0
	for _, part := range parts {
		totalLen += len(part)
	}

	// Allocate once
	buf := make([]byte, 0, totalLen)
	for _, part := range parts {
		buf = append(buf, part...)
	}

	return BytesToString(buf)
}

// StringSliceToByteSlice zero-copy conversion of each string in []string
// Each element in the returned [][]byte is read-only
func StringSliceToByteSlice(strings []string) [][]byte {
	result := make([][]byte, len(strings))
	for i, s := range strings {
		result[i] = StringToBytes(s)
	}
	return result
}

// CompareStringsZeroCopy zero-copy string comparison
// Returns -1 (s1 < s2), 0 (s1 == s2), 1 (s1 > s2)
func CompareStringsZeroCopy(s1, s2 string) int {
	// Go's string comparison is already efficient, use directly
	if s1 < s2 {
		return -1
	}
	if s1 > s2 {
		return 1
	}
	return 0
}

// HasPrefixZeroCopy zero-copy prefix check
func HasPrefixZeroCopy(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	// String slicing is zero-copy
	return s[:len(prefix)] == prefix
}

// HasSuffixZeroCopy zero-copy suffix check
func HasSuffixZeroCopy(s, suffix string) bool {
	if len(suffix) > len(s) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}

// TrimSpaceZeroCopy zero-copy trim leading and trailing spaces
func TrimSpaceZeroCopy(s string) string {
	start := 0
	end := len(s)

	// Find the first non-space character
	for start < end {
		c := s[start]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		start++
	}

	// Find the last non-space character
	for end > start {
		c := s[end-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		end--
	}

	return s[start:end]
}

// SplitZeroCopy zero-copy string splitting
// Strings in the returned slice are all slices of the original string
func SplitZeroCopy(s string, sep byte) []string {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			n++
		}
	}

	result := make([]string, 0, n+1)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])

	return result
}

// JoinZeroCopy zero-copy string joining (single allocation)
func JoinZeroCopy(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}

	// Calculate total length
	totalLen := 0
	for _, part := range parts {
		totalLen += len(part)
	}
	totalLen += (len(parts) - 1) * len(sep)

	// Allocate once
	buf := make([]byte, 0, totalLen)
	buf = append(buf, parts[0]...)
	for i := 1; i < len(parts); i++ {
		buf = append(buf, sep...)
		buf = append(buf, parts[i]...)
	}

	return BytesToString(buf)
}
