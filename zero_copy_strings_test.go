// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"fmt"
	"strings"
	"testing"
)

// TestZeroCopyStringBasic 基本零拷贝操作测试
func TestZeroCopyStringBasic(t *testing.T) {
	// BytesToString
	bytes := []byte("hello world")
	str := BytesToString(bytes)
	if str != "hello world" {
		t.Errorf("BytesToString failed: got %s", str)
	}

	// StringToBytes
	str2 := "test string"
	bytes2 := StringToBytes(str2)
	if string(bytes2) != "test string" {
		t.Errorf("StringToBytes failed: got %s", string(bytes2))
	}

	// SubstringZeroCopy
	substr := SubstringZeroCopy("hello world", 0, 5)
	if substr != "hello" {
		t.Errorf("SubstringZeroCopy failed: got %s", substr)
	}
}

// TestStringBuffer 测试零拷贝字符串缓冲区
func TestStringBuffer(t *testing.T) {
	builder := NewStringBuffer(100)

	builder.WriteString("Hello")
	builder.WriteByte(' ')
	builder.WriteString("World")

	result := builder.StringCopy()
	if result != "Hello World" {
		t.Errorf("Expected 'Hello World', got '%s'", result)
	}

	// 测试重置
	builder.Reset()
	if builder.Len() != 0 {
		t.Errorf("Expected length 0 after reset, got %d", builder.Len())
	}
}

// TestStringPool 测试字符串池
func TestStringPool(t *testing.T) {
	pool := NewStringPool()

	s1 := pool.Intern("test")
	s2 := pool.Intern("test")

	// 同样的字符串应该返回相同的引用
	if s1 != s2 {
		t.Error("String pool should return same reference for identical strings")
	}

	if pool.Size() != 1 {
		t.Errorf("Expected pool size 1, got %d", pool.Size())
	}

	pool.Clear()
	if pool.Size() != 0 {
		t.Errorf("Expected pool size 0 after clear, got %d", pool.Size())
	}
}

// TestInplaceStringBuilder 测试原地字符串构建器
func TestInplaceStringBuilder(t *testing.T) {
	builder := NewInplaceStringBuilder(10)

	builder.Append("Hello")
	builder.Append(" ")
	builder.Append("World")

	result := builder.Build()
	if result != "Hello World" {
		t.Errorf("Expected 'Hello World', got '%s'", result)
	}

	if builder.Len() != 11 {
		t.Errorf("Expected length 11, got %d", builder.Len())
	}
}

// TestFastStringConcat 测试快速字符串拼接
func TestFastStringConcat(t *testing.T) {
	result := FastStringConcatZC("Hello", " ", "World", "!")
	if result != "Hello World!" {
		t.Errorf("Expected 'Hello World!', got '%s'", result)
	}

	// 单个字符串
	single := FastStringConcatZC("single")
	if single != "single" {
		t.Errorf("Expected 'single', got '%s'", single)
	}

	// 空
	empty := FastStringConcatZC()
	if empty != "" {
		t.Errorf("Expected empty string, got '%s'", empty)
	}
}

// TestTrimSpaceZeroCopy 测试零拷贝去空格
func TestTrimSpaceZeroCopy(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  hello  ", "hello"},
		{"\t\nworld\r\n", "world"},
		{"no spaces", "no spaces"},
		{"   ", ""},
		{"", ""},
	}

	for _, tt := range tests {
		result := TrimSpaceZeroCopy(tt.input)
		if result != tt.expected {
			t.Errorf("TrimSpaceZeroCopy(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// TestSplitZeroCopy 测试零拷贝分割
func TestSplitZeroCopy(t *testing.T) {
	result := SplitZeroCopy("a,b,c,d", ',')
	expected := []string{"a", "b", "c", "d"}

	if len(result) != len(expected) {
		t.Errorf("Expected %d parts, got %d", len(expected), len(result))
	}

	for i, v := range expected {
		if result[i] != v {
			t.Errorf("Part %d: expected %q, got %q", i, v, result[i])
		}
	}
}

// TestJoinZeroCopy 测试零拷贝连接
func TestJoinZeroCopy(t *testing.T) {
	parts := []string{"a", "b", "c", "d"}
	result := JoinZeroCopy(parts, ",")

	if result != "a,b,c,d" {
		t.Errorf("Expected 'a,b,c,d', got '%s'", result)
	}

	// 单个元素
	single := JoinZeroCopy([]string{"single"}, ",")
	if single != "single" {
		t.Errorf("Expected 'single', got '%s'", single)
	}

	// 空
	empty := JoinZeroCopy([]string{}, ",")
	if empty != "" {
		t.Errorf("Expected empty string, got '%s'", empty)
	}
}

// TestHasPrefixSuffixZeroCopy 测试前缀后缀检查
func TestHasPrefixSuffixZeroCopy(t *testing.T) {
	str := "hello world"

	if !HasPrefixZeroCopy(str, "hello") {
		t.Error("Should have prefix 'hello'")
	}

	if HasPrefixZeroCopy(str, "world") {
		t.Error("Should not have prefix 'world'")
	}

	if !HasSuffixZeroCopy(str, "world") {
		t.Error("Should have suffix 'world'")
	}

	if HasSuffixZeroCopy(str, "hello") {
		t.Error("Should not have suffix 'hello'")
	}
}

// BenchmarkStringOperations 对比标准库和零拷贝版本的性能
func BenchmarkStringOperations(b *testing.B) {
	b.Run("BytesToString/Standard", func(b *testing.B) {
		bytes := []byte("hello world this is a test string")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = string(bytes)
		}
	})

	b.Run("BytesToString/ZeroCopy", func(b *testing.B) {
		bytes := []byte("hello world this is a test string")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = BytesToString(bytes)
		}
	})

	b.Run("StringConcat/Standard", func(b *testing.B) {
		parts := []string{"hello", " ", "world", " ", "test"}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = strings.Join(parts, "")
		}
	})

	b.Run("StringConcat/ZeroCopy", func(b *testing.B) {
		parts := []string{"hello", " ", "world", " ", "test"}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = FastStringConcatZC(parts...)
		}
	})

	b.Run("StringBuilder/Standard", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var builder strings.Builder
			builder.Grow(100)
			builder.WriteString("hello")
			builder.WriteByte(' ')
			builder.WriteString("world")
			_ = builder.String()
		}
	})

	b.Run("StringBuilder/ZeroCopy", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			builder := NewStringBuffer(100)
			builder.WriteString("hello")
			builder.WriteByte(' ')
			builder.WriteString("world")
			_ = builder.StringCopy()
		}
	})

	b.Run("TrimSpace/Standard", func(b *testing.B) {
		str := "   hello world   "
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = strings.TrimSpace(str)
		}
	})

	b.Run("TrimSpace/ZeroCopy", func(b *testing.B) {
		str := "   hello world   "
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = TrimSpaceZeroCopy(str)
		}
	})

	b.Run("Split/Standard", func(b *testing.B) {
		str := "a,b,c,d,e,f,g,h,i,j"
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = strings.Split(str, ",")
		}
	})

	b.Run("Split/ZeroCopy", func(b *testing.B) {
		str := "a,b,c,d,e,f,g,h,i,j"
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = SplitZeroCopy(str, ',')
		}
	})
}

// ExampleStringBuffer 演示 StringBuffer 的使用
func ExampleStringBuffer() {
	builder := NewStringBuffer(100)

	builder.WriteString("Hello")
	builder.WriteByte(' ')
	builder.WriteString("World")

	result := builder.StringCopy()
	fmt.Println(result)
	// Output: Hello World
}

// ExampleFastStringConcatZC 演示快速字符串拼接
func ExampleFastStringConcatZC() {
	result := FastStringConcatZC("Hello", " ", "World", "!")
	fmt.Println(result)
	// Output: Hello World!
}

// ExampleStringPool 演示字符串池的使用
func ExampleStringPool() {
	pool := NewStringPool()

	// 常用字符串放入池中
	fontName1 := pool.Intern("Arial")
	fontName2 := pool.Intern("Arial") // 重复的字符串会复用

	fmt.Println(fontName1 == fontName2) // 指针相等
	fmt.Println(pool.Size())
	// Output:
	// true
	// 1
}

// ExampleTrimSpaceZeroCopy 演示零拷贝去空格
func ExampleTrimSpaceZeroCopy() {
	str := "   hello world   "
	result := TrimSpaceZeroCopy(str)
	fmt.Println(result)
	// Output: hello world
}

// ExampleSplitZeroCopy 演示零拷贝分割
func ExampleSplitZeroCopy() {
	str := "a,b,c,d"
	parts := SplitZeroCopy(str, ',')
	for _, part := range parts {
		fmt.Println(part)
	}
	// Output:
	// a
	// b
	// c
	// d
}

// ExampleJoinZeroCopy 演示零拷贝连接
func ExampleJoinZeroCopy() {
	parts := []string{"apple", "banana", "cherry"}
	result := JoinZeroCopy(parts, ", ")
	fmt.Println(result)
	// Output: apple, banana, cherry
}

// 演示在实际 PDF 处理中如何使用零拷贝优化
func Example_zeroCopyInPDFProcessing() {
	// 假设从 PDF 提取了一些文本块
	texts := []string{
		"  First paragraph  ",
		"  Second paragraph  ",
		"  Third paragraph  ",
	}

	// 使用零拷贝操作处理
	builder := NewStringBuffer(1024)

	for i, text := range texts {
		// 去除首尾空格（零拷贝）
		trimmed := TrimSpaceZeroCopy(text)
		builder.WriteString(trimmed)

		if i < len(texts)-1 {
			builder.WriteString("\n")
		}
	}

	result := builder.StringCopy()
	fmt.Println(result)
	// Output:
	// First paragraph
	// Second paragraph
	// Third paragraph
}
