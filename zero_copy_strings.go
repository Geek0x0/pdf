// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"unsafe"
)

// ZeroCopyString 提供零拷贝字符串操作
// 注意：这些操作需要谨慎使用，因为它们绕过了 Go 的类型安全

// BytesToString 零拷贝转换 []byte 到 string
// 警告：返回的字符串直接引用底层字节数组，不要修改原始 []byte
func BytesToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return *(*string)(unsafe.Pointer(&b))
}

// StringToBytes 零拷贝转换 string 到 []byte
// 警告：返回的 []byte 是只读的，不要修改
func StringToBytes(s string) []byte {
	if s == "" {
		return nil
	}
	
	// 使用 unsafe 直接转换
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// SubstringZeroCopy 零拷贝获取子字符串
// 实际上所有字符串切片在 Go 中已经是零拷贝的
func SubstringZeroCopy(s string, start, end int) string {
	if start < 0 || end > len(s) || start > end {
		return ""
	}
	return s[start:end]
}

// StringBuffer 字符串构建缓冲区，优化多次拼接
type StringBuffer struct {
	buf []byte
}

// NewStringBuffer 创建新的字符串缓冲区
func NewStringBuffer(capacity int) *StringBuffer {
	return &StringBuffer{
		buf: make([]byte, 0, capacity),
	}
}

// WriteString 写入字符串
func (sb *StringBuffer) WriteString(s string) {
	sb.buf = append(sb.buf, s...)
}

// WriteByte 写入单个字节
func (sb *StringBuffer) WriteByte(b byte) error {
	sb.buf = append(sb.buf, b)
	return nil
}

// WriteBytes 写入字节切片
func (sb *StringBuffer) WriteBytes(b []byte) {
	sb.buf = append(sb.buf, b...)
}

// String 零拷贝返回字符串
// 警告：返回后不要再使用 StringBuffer
func (sb *StringBuffer) String() string {
	return BytesToString(sb.buf)
}

// StringCopy 安全返回字符串副本
func (sb *StringBuffer) StringCopy() string {
	return string(sb.buf)
}

// Len 返回当前长度
func (sb *StringBuffer) Len() int {
	return len(sb.buf)
}

// Cap 返回容量
func (sb *StringBuffer) Cap() int {
	return cap(sb.buf)
}

// Reset 重置缓冲区
func (sb *StringBuffer) Reset() {
	sb.buf = sb.buf[:0]
}

// Bytes 返回底层字节切片
func (sb *StringBuffer) Bytes() []byte {
	return sb.buf
}

// StringPool 字符串池，复用常见字符串
type StringPool struct {
	pool map[string]string
}

// NewStringPool 创建新的字符串池
func NewStringPool() *StringPool {
	return &StringPool{
		pool: make(map[string]string),
	}
}

// Intern 将字符串加入池中并返回池化版本
// 相同内容的字符串将共享内存
func (sp *StringPool) Intern(s string) string {
	if cached, ok := sp.pool[s]; ok {
		return cached
	}
	// 创建新副本并存储
	sp.pool[s] = s
	return s
}

// Clear 清空池
func (sp *StringPool) Clear() {
	sp.pool = make(map[string]string)
}

// Size 返回池中字符串数量
func (sp *StringPool) Size() int {
	return len(sp.pool)
}

// InplaceStringBuilder 原地字符串构建器
// 避免中间分配
type InplaceStringBuilder struct {
	parts  []string
	length int
}

// NewInplaceStringBuilder 创建新的原地字符串构建器
func NewInplaceStringBuilder(capacity int) *InplaceStringBuilder {
	return &InplaceStringBuilder{
		parts: make([]string, 0, capacity),
	}
}

// Append 追加字符串
func (isb *InplaceStringBuilder) Append(s string) {
	isb.parts = append(isb.parts, s)
	isb.length += len(s)
}

// Build 构建最终字符串（一次性分配）
func (isb *InplaceStringBuilder) Build() string {
	if len(isb.parts) == 0 {
		return ""
	}
	if len(isb.parts) == 1 {
		return isb.parts[0]
	}

	// 一次性分配足够的空间
	buf := make([]byte, 0, isb.length)
	for _, part := range isb.parts {
		buf = append(buf, part...)
	}
	return BytesToString(buf)
}

// Reset 重置构建器
func (isb *InplaceStringBuilder) Reset() {
	isb.parts = isb.parts[:0]
	isb.length = 0
}

// Len 返回总长度
func (isb *InplaceStringBuilder) Len() int {
	return isb.length
}

// StringInterning 全局字符串驻留
var globalStringPool = NewStringPool()

// InternString 将字符串加入全局池
func InternString(s string) string {
	return globalStringPool.Intern(s)
}

// ClearGlobalStringPool 清空全局字符串池
func ClearGlobalStringPool() {
	globalStringPool.Clear()
}

// FastStringConcatZC 快速拼接多个字符串（零拷贝版本）
func FastStringConcatZC(parts ...string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}

	// 计算总长度
	totalLen := 0
	for _, part := range parts {
		totalLen += len(part)
	}

	// 一次性分配
	buf := make([]byte, 0, totalLen)
	for _, part := range parts {
		buf = append(buf, part...)
	}

	return BytesToString(buf)
}

// StringSliceToByteSlice 零拷贝转换 []string 中的每个字符串
// 返回的 [][]byte 中每个元素都是只读的
func StringSliceToByteSlice(strings []string) [][]byte {
	result := make([][]byte, len(strings))
	for i, s := range strings {
		result[i] = StringToBytes(s)
	}
	return result
}

// CompareStringsZeroCopy 零拷贝字符串比较
// 返回 -1 (s1 < s2), 0 (s1 == s2), 1 (s1 > s2)
func CompareStringsZeroCopy(s1, s2 string) int {
	// Go 的字符串比较已经很高效，直接使用
	if s1 < s2 {
		return -1
	}
	if s1 > s2 {
		return 1
	}
	return 0
}

// HasPrefixZeroCopy 零拷贝前缀检查
func HasPrefixZeroCopy(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	// 字符串切片是零拷贝的
	return s[:len(prefix)] == prefix
}

// HasSuffixZeroCopy 零拷贝后缀检查
func HasSuffixZeroCopy(s, suffix string) bool {
	if len(suffix) > len(s) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}

// TrimSpaceZeroCopy 零拷贝去除首尾空格
func TrimSpaceZeroCopy(s string) string {
	start := 0
	end := len(s)

	// 查找第一个非空格字符
	for start < end {
		c := s[start]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		start++
	}

	// 查找最后一个非空格字符
	for end > start {
		c := s[end-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		end--
	}

	return s[start:end]
}

// SplitZeroCopy 零拷贝字符串分割
// 返回的切片中的字符串都是原字符串的切片
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

// JoinZeroCopy 零拷贝字符串连接（一次性分配）
func JoinZeroCopy(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}

	// 计算总长度
	totalLen := 0
	for _, part := range parts {
		totalLen += len(part)
	}
	totalLen += (len(parts) - 1) * len(sep)

	// 一次性分配
	buf := make([]byte, 0, totalLen)
	buf = append(buf, parts[0]...)
	for i := 1; i < len(parts); i++ {
		buf = append(buf, sep...)
		buf = append(buf, parts[i]...)
	}

	return BytesToString(buf)
}
