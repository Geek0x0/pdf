package pdf

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

// TestFindLastLineComprehensive tests findLastLine with comprehensive edge cases
func TestFindLastLineComprehensive(t *testing.T) {
	tests := []struct {
		name     string
		buf      string
		search   string
		expected int
		desc     string
	}{
		// 基本场景
		{
			name:     "normal_middle_of_buffer",
			buf:      "header\nstartxref\n123\ntrailer",
			search:   "startxref",
			expected: 7,
			desc:     "关键字在 buffer 中间位置",
		},
		{
			name:     "at_start_with_newline_before",
			buf:      "\nstartxref\ndata",
			search:   "startxref",
			expected: 1,
			desc:     "关键字在开头（前面有换行符）",
		},

		// 末尾边界场景
		{
			name:     "exact_end_of_buffer",
			buf:      "data\nstartxref",
			search:   "startxref",
			expected: 5,
			desc:     "关键字正好在 buffer 末尾",
		},
		{
			name:     "one_char_after",
			buf:      "data\nstartxref\n",
			search:   "startxref",
			expected: 5,
			desc:     "关键字后面只有一个换行符",
		},
		{
			name:     "two_chars_after",
			buf:      "data\nstartxref\n\n",
			search:   "startxref",
			expected: 5,
			desc:     "关键字后面有两个换行符",
		},
		{
			name:     "cr_lf_after",
			buf:      "data\nstartxref\r\n",
			search:   "startxref",
			expected: 5,
			desc:     "关键字后面是 CRLF",
		},

		// 开头边界场景
		{
			name:     "no_char_before_fail",
			buf:      "startxref\ndata",
			search:   "startxref",
			expected: -1,
			desc:     "关键字在最开头（前面没有换行符）应该失败",
		},
		{
			name:     "space_before_fail",
			buf:      " startxref\ndata",
			search:   "startxref",
			expected: -1,
			desc:     "关键字前面是空格而非换行符应该失败",
		},

		// 多次出现场景
		{
			name:     "multiple_last_valid",
			buf:      "old\nstartxref\n1\nmid\nstartxref\n2\nend",
			search:   "startxref",
			expected: 20,
			desc:     "多次出现时应返回最后一个有效位置",
		},
		{
			name:     "multiple_only_last_valid",
			buf:      "old startxref\n1\nvalid\nstartxref\n2",
			search:   "startxref",
			expected: 22,
			desc:     "多次出现但只有最后一个有效",
		},
		{
			name:     "multiple_none_valid",
			buf:      "bad startxref bad\nwrong startxref end",
			search:   "startxref",
			expected: -1,
			desc:     "多次出现但都无效（前后都不是换行符）",
		},

		// 不同换行符组合
		{
			name:     "cr_before_lf_after",
			buf:      "data\rstartxref\nend",
			search:   "startxref",
			expected: 5,
			desc:     "CR 在前，LF 在后",
		},
		{
			name:     "lf_before_cr_after",
			buf:      "data\nstartxref\rend",
			search:   "startxref",
			expected: 5,
			desc:     "LF 在前，CR 在后",
		},
		{
			name:     "cr_before_cr_after",
			buf:      "data\rstartxref\rend",
			search:   "startxref",
			expected: 5,
			desc:     "前后都是 CR",
		},

		// 空 buffer 和特殊情况
		{
			name:     "empty_buffer",
			buf:      "",
			search:   "startxref",
			expected: -1,
			desc:     "空 buffer",
		},
		{
			name:     "only_keyword",
			buf:      "startxref",
			search:   "startxref",
			expected: -1,
			desc:     "只有关键字本身（前后都没有换行符）",
		},
		{
			name:     "keyword_with_only_newline_before",
			buf:      "\nstartxref",
			search:   "startxref",
			expected: 1,
			desc:     "只有前面有换行符（关键字在末尾）",
		},

		// 相似但不匹配的情况
		{
			name:     "embedded_in_word",
			buf:      "data\nnostartxrefhere\nend",
			search:   "startxref",
			expected: -1,
			desc:     "关键字嵌入在其他单词中",
		},
		{
			name:     "tab_before",
			buf:      "data\tstartxref\nend",
			search:   "startxref",
			expected: -1,
			desc:     "tab 在前面而非换行符",
		},

		// 长 buffer 场景
		{
			name:     "large_buffer_end",
			buf:      strings.Repeat("x", 5000) + "\nstartxref",
			search:   "startxref",
			expected: 5001,
			desc:     "大 buffer，关键字在末尾",
		},
		{
			name:     "large_buffer_middle",
			buf:      strings.Repeat("x", 2000) + "\nstartxref\n" + strings.Repeat("y", 3000),
			search:   "startxref",
			expected: 2001,
			desc:     "大 buffer，关键字在中间",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findLastLine([]byte(tt.buf), tt.search)
			if result != tt.expected {
				t.Errorf("%s\nfindLastLine() = %d, want %d\nBuffer length: %d\nBuffer (first 100): %q\nBuffer (last 100): %q",
					tt.desc, result, tt.expected, len(tt.buf),
					truncate(tt.buf, 100, true),
					truncate(tt.buf, 100, false))
			}
		})
	}
}

// TestBufferTrimRightEdgeCases tests TrimRight behavior that affects startxref finding
func TestBufferTrimRightEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		desc     string
	}{
		{
			name:     "remove_trailing_newlines",
			input:    "content\n\n\n",
			expected: "content",
			desc:     "移除多个尾部换行符",
		},
		{
			name:     "remove_mixed_whitespace",
			input:    "content\n\t \r\n",
			expected: "content",
			desc:     "移除混合空白字符",
		},
		{
			name:     "keep_internal_whitespace",
			input:    "con tent\n",
			expected: "con tent",
			desc:     "保留内部空白",
		},
		{
			name:     "trim_to_keyword",
			input:    "startxref\n\n",
			expected: "startxref",
			desc:     "trim 到关键字",
		},
		{
			name:     "no_trailing_whitespace",
			input:    "content",
			expected: "content",
			desc:     "没有尾部空白",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := strings.TrimRight(tt.input, "\r\n\t ")
			if result != tt.expected {
				t.Errorf("%s\nTrimRight() = %q, want %q", tt.desc, result, tt.expected)
			}
		})
	}
}

// TestEOFMarkerEdgeCases tests %%EOF marker detection edge cases
func TestEOFMarkerEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		input  []byte
		hasEOF bool
		eofPos int
		desc   string
	}{
		{
			name:   "normal_eof_at_end",
			input:  []byte("content\n%%EOF\n"),
			hasEOF: true,
			eofPos: 8,
			desc:   "%%EOF 在末尾",
		},
		{
			name:   "eof_exact_end",
			input:  []byte("content\n%%EOF"),
			hasEOF: true,
			eofPos: 8,
			desc:   "%%EOF 正好在末尾",
		},
		{
			name:   "eof_with_garbage_after",
			input:  []byte("content\n%%EOF\ngarbage"),
			hasEOF: true,
			eofPos: 8,
			desc:   "%%EOF 后面有垃圾数据",
		},
		{
			name:   "multiple_eof",
			input:  []byte("%%EOF\ncontent\n%%EOF"),
			hasEOF: true,
			eofPos: 14,
			desc:   "多个 %%EOF，应该找最后一个",
		},
		{
			name:   "no_eof",
			input:  []byte("content without eof"),
			hasEOF: false,
			eofPos: -1,
			desc:   "没有 %%EOF",
		},
		{
			name:   "partial_eof",
			input:  []byte("content\n%%EO"),
			hasEOF: false,
			eofPos: -1,
			desc:   "不完整的 %%EOF",
		},
		{
			name:   "case_sensitive",
			input:  []byte("content\n%%eof"),
			hasEOF: false,
			eofPos: -1,
			desc:   "小写的 eof（不匹配）",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := bytes.LastIndex(tt.input, []byte("%%EOF"))
			hasEOF := idx >= 0

			if hasEOF != tt.hasEOF {
				t.Errorf("%s\nhasEOF = %v, want %v", tt.desc, hasEOF, tt.hasEOF)
			}
			if idx != tt.eofPos {
				t.Errorf("%s\neofPos = %d, want %d", tt.desc, idx, tt.eofPos)
			}
		})
	}
}

// TestPDFEndStructureVariations tests various PDF end structure patterns
func TestPDFEndStructureVariations(t *testing.T) {
	tests := []struct {
		name        string
		pdfEnd      string
		shouldFind  bool
		description string
	}{
		{
			name:        "standard_structure",
			pdfEnd:      "trailer\n<< /Size 10 >>\nstartxref\n12345\n%%EOF\n",
			shouldFind:  true,
			description: "标准的 PDF 末尾结构",
		},
		{
			name:        "no_trailing_newline",
			pdfEnd:      "trailer\n<< /Size 10 >>\nstartxref\n12345\n%%EOF",
			shouldFind:  true,
			description: "%%EOF 后没有换行符",
		},
		{
			name:        "compact_structure",
			pdfEnd:      "trailer<</Size 10>>\nstartxref\n12345\n%%EOF",
			shouldFind:  true,
			description: "紧凑格式（字典间无空格）",
		},
		{
			name:        "windows_line_endings",
			pdfEnd:      "trailer\r\n<< /Size 10 >>\r\nstartxref\r\n12345\r\n%%EOF\r\n",
			shouldFind:  true,
			description: "Windows 风格换行符（CRLF）",
		},
		{
			name:        "mixed_line_endings",
			pdfEnd:      "trailer\n<< /Size 10 >>\r\nstartxref\n12345\r\n%%EOF",
			shouldFind:  true,
			description: "混合换行符",
		},
		{
			name:        "extra_whitespace",
			pdfEnd:      "trailer\n<< /Size 10 >>\n  \nstartxref\n12345\n%%EOF\n\n\n",
			shouldFind:  true,
			description: "额外的空白行",
		},
		{
			name:        "minimal_valid",
			pdfEnd:      "\nstartxref\n0\n%%EOF",
			shouldFind:  true,
			description: "最小有效结构",
		},
		{
			name:        "startxref_at_end_no_offset",
			pdfEnd:      "trailer\n<< >>\nstartxref",
			shouldFind:  true,
			description: "startxref 在末尾（没有 offset 和 EOF）",
		},
		{
			name:        "large_offset",
			pdfEnd:      "trailer\n<< /Size 1000 >>\nstartxref\n999999999\n%%EOF",
			shouldFind:  true,
			description: "大偏移量",
		},
		{
			name:        "zero_offset",
			pdfEnd:      "trailer\n<< /Size 1 >>\nstartxref\n0\n%%EOF",
			shouldFind:  true,
			description: "零偏移量",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟实际的处理流程
			buf := []byte(tt.pdfEnd)

			// Step 1: TrimRight
			buf = bytes.TrimRight(buf, "\r\n\t ")

			// Step 2: Find %%EOF
			eofIdx := bytes.LastIndex(buf, []byte("%%EOF"))
			if eofIdx >= 0 {
				buf = buf[:eofIdx+5]
			}

			// Step 3: Find startxref
			i := findLastLine(buf, "startxref")

			found := i >= 0
			if found != tt.shouldFind {
				t.Errorf("%s\nExpected to find startxref: %v, but found: %v (position: %d)\nProcessed buffer: %q",
					tt.description, tt.shouldFind, found, i, string(buf))
			}
		})
	}
}

// TestStartxrefWithVariousOffsets tests startxref with different offset formats
func TestStartxrefWithVariousOffsets(t *testing.T) {
	tests := []struct {
		name   string
		offset string
		valid  bool
	}{
		{"single_digit", "0", true},
		{"small_number", "123", true},
		{"medium_number", "12345", true},
		{"large_number", "9876543210", true},
		{"with_spaces_before", "  123", true},
		{"with_spaces_after", "123  ", true},
		{"with_tab", "123\t", true},
		{"negative", "-123", true}, // 解析会成功但语义上无效
		{"empty", "", false},
		{"non_numeric", "abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pdfEnd := fmt.Sprintf("\nstartxref\n%s\n%%EOF", tt.offset)
			buf := []byte(pdfEnd)

			i := findLastLine(buf, "startxref")
			found := i >= 0

			if !found && tt.valid {
				t.Errorf("Failed to find startxref with offset %q", tt.offset)
			}
		})
	}
}

// TestConcurrentBufferAccess tests thread-safety concerns
func TestConcurrentBufferAccess(t *testing.T) {
	// 虽然 findLastLine 本身是无状态的，但测试并发访问的安全性
	pdfEnd := "trailer\n<< /Size 10 >>\nstartxref\n12345\n%%EOF"
	buf := []byte(pdfEnd)

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				result := findLastLine(buf, "startxref")
				if result < 0 {
					t.Errorf("Concurrent access failed to find startxref")
				}
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestUnicodeAndBinaryData tests handling of non-ASCII data
func TestUnicodeAndBinaryData(t *testing.T) {
	tests := []struct {
		name     string
		pdfEnd   []byte
		expected int
	}{
		{
			name:     "ascii_only",
			pdfEnd:   []byte("\nstartxref\n123\n"),
			expected: 1,
		},
		{
			name:     "with_unicode_before",
			pdfEnd:   []byte("中文\nstartxref\n123\n"),
			expected: 7, // "中文" 是 6 字节 + 1 换行符
		},
		{
			name:     "with_binary_before",
			pdfEnd:   []byte("\x00\x01\x02\nstartxref\n123\n"),
			expected: 4,
		},
		{
			name:     "binary_after_startxref",
			pdfEnd:   []byte("\nstartxref\n\x00\x01\x02"),
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findLastLine(tt.pdfEnd, "startxref")
			if result != tt.expected {
				t.Errorf("Expected %d, got %d for buffer: %v", tt.expected, result, tt.pdfEnd)
			}
		})
	}
}

// TestVeryLargeBuffers tests performance with large buffers
func TestVeryLargeBuffers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large buffer test in short mode")
	}

	sizes := []int{
		1024,    // 1KB
		10240,   // 10KB
		102400,  // 100KB
		1024000, // 1MB
	}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			// 创建大 buffer，startxref 在末尾
			buf := make([]byte, size)
			for i := range buf {
				buf[i] = 'x'
			}

			// 在末尾添加 startxref
			trailer := "\nstartxref\n12345"
			copy(buf[size-len(trailer):], trailer)

			result := findLastLine(buf, "startxref")
			expectedPos := size - len(trailer) + 1

			if result != expectedPos {
				t.Errorf("For buffer size %d, expected position %d, got %d",
					size, expectedPos, result)
			}
		})
	}
}

// TestMalformedPDFEndings tests various malformed PDF endings
func TestMalformedPDFEndings(t *testing.T) {
	tests := []struct {
		name        string
		pdfEnd      string
		shouldFind  bool
		description string
	}{
		{
			name:        "missing_newline_before_startxref",
			pdfEnd:      "trailerstartxref\n123\n%%EOF",
			shouldFind:  false,
			description: "startxref 前面缺少换行符",
		},
		{
			name:        "startxref_misspelled",
			pdfEnd:      "trailer\nstartxre\n123\n%%EOF",
			shouldFind:  false,
			description: "startxref 拼写错误",
		},
		{
			name:        "extra_text_after_startxref",
			pdfEnd:      "trailer\nstartxrefextra\n123\n%%EOF",
			shouldFind:  false,
			description: "startxref 后面紧跟额外文本",
		},
		{
			name:        "duplicate_startxref_inline",
			pdfEnd:      "trailer\nstartxref startxref\n123\n%%EOF",
			shouldFind:  false,
			description: "同一行有重复的 startxref",
		},
		{
			name:        "truncated_at_startxref",
			pdfEnd:      "trailer\nstartxre",
			shouldFind:  false,
			description: "在 startxref 中间截断",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := []byte(tt.pdfEnd)
			result := findLastLine(buf, "startxref")
			found := result >= 0

			if found != tt.shouldFind {
				t.Errorf("%s\nExpected found=%v, got found=%v (position: %d)",
					tt.description, tt.shouldFind, found, result)
			}
		})
	}
}

// Helper function to truncate string for display
func truncate(s string, maxLen int, fromStart bool) string {
	if len(s) <= maxLen {
		return s
	}
	if fromStart {
		return s[:maxLen] + "..."
	}
	return "..." + s[len(s)-maxLen:]
}

// TestBufferReaderEdgeCases tests buffer reading edge cases
func TestBufferReaderEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		readSize int
		desc     string
	}{
		{
			name:     "exact_boundary",
			content:  strings.Repeat("x", 1024) + "\nstartxref\n",
			readSize: 1024,
			desc:     "内容正好在读取边界",
		},
		{
			name:     "crosses_boundary",
			content:  strings.Repeat("x", 1020) + "\nstartxref\n",
			readSize: 1024,
			desc:     "关键字跨越读取边界",
		},
		{
			name:     "multiple_boundaries",
			content:  strings.Repeat("x", 2048) + "\nstartxref\n",
			readSize: 1024,
			desc:     "跨越多个读取边界",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟分块读取
			reader := strings.NewReader(tt.content)
			var accumulated []byte

			buf := make([]byte, tt.readSize)
			for {
				n, err := reader.Read(buf)
				if n > 0 {
					accumulated = append(accumulated, buf[:n]...)
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("Read error: %v", err)
				}
			}

			result := findLastLine(accumulated, "startxref")
			if result < 0 {
				t.Errorf("%s\nFailed to find startxref after chunked reading", tt.desc)
			}
		})
	}
}
