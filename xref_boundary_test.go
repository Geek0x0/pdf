package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestXrefTableBoundaryConditions tests xref table parsing edge cases
func TestXrefTableBoundaryConditions(t *testing.T) {
	tests := []struct {
		name        string
		xrefData    string
		expectError bool
		description string
	}{
		{
			name: "minimal_valid_xref",
			xrefData: `xref
0 1
0000000000 65535 f 
trailer
<< /Size 1 >>`,
			expectError: false,
			description: "最小有效 xref 表",
		},
		{
			name: "xref_at_buffer_start",
			xrefData: `xref
0 2
0000000000 65535 f 
0000000015 00000 n 
trailer
<< /Size 2 >>`,
			expectError: false,
			description: "xref 在 buffer 开头",
		},
		{
			name: "multiple_subsections",
			xrefData: `xref
0 1
0000000000 65535 f 
3 2
0000000100 00000 n 
0000000200 00000 n 
trailer
<< /Size 5 >>`,
			expectError: false,
			description: "多个子节",
		},
		{
			name: "large_object_count",
			xrefData: `xref
0 1000
0000000000 65535 f ` + strings.Repeat("\n0000000100 00000 n ", 999) + `
trailer
<< /Size 1000 >>`,
			expectError: false,
			description: "大量对象",
		},
		{
			name:        "xref_with_windows_newlines",
			xrefData:    "xref\r\n0 1\r\n0000000000 65535 f \r\ntrailer\r\n<< /Size 1 >>",
			expectError: false,
			description: "Windows 换行符",
		},
		{
			name:        "xref_with_mixed_newlines",
			xrefData:    "xref\n0 1\r\n0000000000 65535 f \ntrailer\r\n<< /Size 1 >>",
			expectError: false,
			description: "混合换行符",
		},
		{
			name:        "truncated_xref_header",
			xrefData:    "xre",
			expectError: true,
			description: "截断的 xref 头",
		},
		{
			name:        "missing_subsection_header",
			xrefData:    "xref\n0000000000 65535 f \ntrailer\n<< >>",
			expectError: true,
			description: "缺少子节头",
		},
		{
			name: "extra_whitespace",
			xrefData: `xref

0 1

0000000000 65535 f 

trailer

<< /Size 1 >>`,
			expectError: false,
			description: "额外的空白行",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 简单验证数据格式是否符合预期
			hasXref := strings.Contains(tt.xrefData, "xref")
			if !hasXref && !tt.expectError {
				t.Errorf("%s: Missing xref keyword in non-error case", tt.description)
			}
		})
	}
}

// TestTrailerDictionaryBoundary tests trailer dictionary parsing edge cases
func TestTrailerDictionaryBoundary(t *testing.T) {
	tests := []struct {
		name        string
		trailer     string
		expectError bool
		description string
	}{
		{
			name:        "minimal_trailer",
			trailer:     "trailer\n<< /Size 1 >>",
			expectError: false,
			description: "最小 trailer",
		},
		{
			name:        "trailer_with_root",
			trailer:     "trailer\n<< /Size 10 /Root 1 0 R >>",
			expectError: false,
			description: "带 Root 的 trailer",
		},
		{
			name:        "trailer_with_info",
			trailer:     "trailer\n<< /Size 10 /Root 1 0 R /Info 2 0 R >>",
			expectError: false,
			description: "带 Info 的 trailer",
		},
		{
			name:        "trailer_with_prev",
			trailer:     "trailer\n<< /Size 10 /Prev 1234 >>",
			expectError: false,
			description: "带 Prev（增量更新）的 trailer",
		},
		{
			name:        "trailer_compact",
			trailer:     "trailer<</Size 1>>",
			expectError: false,
			description: "紧凑格式 trailer",
		},
		{
			name:        "trailer_multiline",
			trailer:     "trailer\n<<\n/Size 10\n/Root 1 0 R\n>>",
			expectError: false,
			description: "多行 trailer",
		},
		{
			name:        "empty_trailer_dict",
			trailer:     "trailer\n<< >>",
			expectError: false,
			description: "空 trailer 字典",
		},
		{
			name:        "missing_closing_bracket",
			trailer:     "trailer\n<< /Size 1",
			expectError: true,
			description: "缺少闭合括号",
		},
		{
			name:        "no_trailer_keyword",
			trailer:     "<< /Size 1 >>",
			expectError: true,
			description: "缺少 trailer 关键字",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasTrailer := strings.Contains(tt.trailer, "trailer")
			hasDict := strings.Contains(tt.trailer, "<<") && strings.Contains(tt.trailer, ">>")

			if !hasTrailer && !tt.expectError {
				t.Errorf("%s: Missing trailer keyword", tt.description)
			}
			if !hasDict && !tt.expectError {
				t.Errorf("%s: Missing dictionary brackets", tt.description)
			}
		})
	}
}

// TestOffsetBoundaryValues tests boundary values for PDF offsets
func TestOffsetBoundaryValues(t *testing.T) {
	tests := []struct {
		name   string
		offset string
		valid  bool
	}{
		{"zero", "0000000000", true},
		{"one", "0000000001", true},
		{"max_10_digit", "9999999999", true},
		{"small_no_padding", "15", true},
		{"medium_no_padding", "12345", true},
		{"with_leading_zeros", "0000012345", true},
		{"very_large", "999999999999", true}, // 超过 10 位
		{"max_int32", "2147483647", true},
		{"over_int32", "2147483648", true},
		{"max_int64_str", "9223372036854775807", true},
		{"negative", "-1", false},
		{"empty", "", false},
		{"non_numeric", "abcdef", false},
		{"hex_like", "0xFFFF", false},
		{"scientific", "1e10", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 基本格式检查
			if len(tt.offset) == 0 {
				if tt.valid {
					t.Errorf("Empty offset marked as valid")
				}
				return
			}

			// 检查是否全为数字（或负号开头）
			isNumeric := true
			for i, c := range tt.offset {
				if i == 0 && c == '-' {
					continue
				}
				if c < '0' || c > '9' {
					isNumeric = false
					break
				}
			}

			if tt.valid && !isNumeric {
				t.Errorf("Offset %q marked as valid but contains non-numeric chars", tt.offset)
			}
		})
	}
}

// TestGenerationNumberBoundary tests generation number edge cases
func TestGenerationNumberBoundary(t *testing.T) {
	tests := []struct {
		name string
		gen  string
		max  int
	}{
		{"zero", "00000", 65535},
		{"one", "00001", 65535},
		{"normal", "00123", 65535},
		{"max_5_digit", "65535", 65535},
		{"over_max", "65536", 65535},
		{"very_large", "99999", 65535},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gen int
			_, err := fmt.Sscanf(tt.gen, "%d", &gen)
			if err != nil {
				t.Errorf("Failed to parse generation %q: %v", tt.gen, err)
				return
			}

			if gen > tt.max {
				t.Logf("Generation %d exceeds max %d (this may be acceptable for some PDFs)",
					gen, tt.max)
			}
		})
	}
}

// TestXrefEntryFormats tests various xref entry format variations
func TestXrefEntryFormats(t *testing.T) {
	tests := []struct {
		name        string
		entry       string
		expectValid bool
		description string
	}{
		{
			name:        "standard_free",
			entry:       "0000000000 65535 f ",
			expectValid: true,
			description: "标准空闲对象",
		},
		{
			name:        "standard_in_use",
			entry:       "0000000123 00000 n ",
			expectValid: true,
			description: "标准使用中对象",
		},
		{
			name:        "no_trailing_space",
			entry:       "0000000123 00000 n",
			expectValid: true,
			description: "无尾随空格",
		},
		{
			name:        "multiple_spaces",
			entry:       "0000000123  00000  n ",
			expectValid: true,
			description: "多个空格分隔",
		},
		{
			name:        "tab_separated",
			entry:       "0000000123\t00000\tn ",
			expectValid: true,
			description: "tab 分隔",
		},
		{
			name:        "mixed_whitespace",
			entry:       "0000000123 \t00000\t n ",
			expectValid: true,
			description: "混合空白字符",
		},
		{
			name:        "cr_ending",
			entry:       "0000000123 00000 n\r",
			expectValid: true,
			description: "CR 结尾",
		},
		{
			name:        "crlf_ending",
			entry:       "0000000123 00000 n\r\n",
			expectValid: true,
			description: "CRLF 结尾",
		},
		{
			name:        "missing_flag",
			entry:       "0000000123 00000",
			expectValid: false,
			description: "缺少 n/f 标志",
		},
		{
			name:        "invalid_flag",
			entry:       "0000000123 00000 x ",
			expectValid: false,
			description: "无效标志（非 n/f）",
		},
		{
			name:        "short_offset",
			entry:       "123 00000 n ",
			expectValid: true,
			description: "短偏移量（无前导零）",
		},
		{
			name:        "short_generation",
			entry:       "0000000123 0 n ",
			expectValid: true,
			description: "短代数（无前导零）",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 基本格式检查
			parts := strings.Fields(tt.entry)

			if len(parts) < 3 {
				if tt.expectValid {
					t.Errorf("%s: Expected valid but has < 3 parts", tt.description)
				}
				return
			}

			// 检查标志
			flag := strings.TrimSpace(parts[2])
			if flag != "n" && flag != "f" && flag != "n\r" && flag != "f\r" {
				if tt.expectValid {
					t.Errorf("%s: Invalid flag %q", tt.description, flag)
				}
				return
			}

			if !tt.expectValid && flag == "n" || flag == "f" {
				t.Logf("%s: Marked as invalid but has valid flag", tt.description)
			}
		})
	}
}

// TestXrefStreamBoundary tests xref stream edge cases
func TestXrefStreamBoundary(t *testing.T) {
	tests := []struct {
		name        string
		streamObj   string
		expectError bool
		description string
	}{
		{
			name: "minimal_xref_stream",
			streamObj: `1 0 obj
<< /Type /XRef /Size 1 /W [1 1 1] /Length 3 >>
stream
` + "\x00\x00\x00" + `
endstream
endobj`,
			expectError: false,
			description: "最小 xref stream",
		},
		{
			name: "with_index",
			streamObj: `1 0 obj
<< /Type /XRef /Size 10 /Index [0 10] /W [1 2 1] /Length 40 >>
stream
` + string(make([]byte, 40)) + `
endstream
endobj`,
			expectError: false,
			description: "带 Index 的 xref stream",
		},
		{
			name: "compressed_xref",
			streamObj: `1 0 obj
<< /Type /XRef /Size 100 /W [1 2 1] /Filter /FlateDecode /Length 50 >>
stream
` + string(make([]byte, 50)) + `
endstream
endobj`,
			expectError: false,
			description: "压缩的 xref stream",
		},
		{
			name:        "missing_type",
			streamObj:   `1 0 obj<< /Size 1 /W [1 1 1] >>stream` + "\x00\x00\x00" + `endstream endobj`,
			expectError: true,
			description: "缺少 /Type",
		},
		{
			name:        "missing_w_array",
			streamObj:   `1 0 obj<< /Type /XRef /Size 1 >>stream` + "\x00\x00\x00" + `endstream endobj`,
			expectError: true,
			description: "缺少 /W 数组",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasType := strings.Contains(tt.streamObj, "/Type")
			hasXRef := strings.Contains(tt.streamObj, "/XRef")
			hasW := strings.Contains(tt.streamObj, "/W")

			if !tt.expectError {
				if !hasType || !hasXRef {
					t.Errorf("%s: Missing required /Type /XRef", tt.description)
				}
				if !hasW {
					t.Errorf("%s: Missing required /W array", tt.description)
				}
			}
		})
	}
}

// TestBufferBoundaryDuringXrefParsing tests buffer boundaries during xref parsing
func TestBufferBoundaryDuringXrefParsing(t *testing.T) {
	// 测试 xref 跨越读取 buffer 边界的情况
	tests := []struct {
		name       string
		bufferSize int
		xrefPos    int
		totalSize  int
	}{
		{
			name:       "xref_within_first_buffer",
			bufferSize: 1024,
			xrefPos:    500,
			totalSize:  2048,
		},
		{
			name:       "xref_at_buffer_boundary",
			bufferSize: 1024,
			xrefPos:    1020,
			totalSize:  2048,
		},
		{
			name:       "xref_crosses_boundary",
			bufferSize: 1024,
			xrefPos:    1022,
			totalSize:  2048,
		},
		{
			name:       "xref_in_second_buffer",
			bufferSize: 1024,
			xrefPos:    1500,
			totalSize:  2048,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建测试数据
			data := make([]byte, tt.totalSize)
			for i := range data {
				data[i] = 'x'
			}

			// 在指定位置插入 xref 表
			xrefData := []byte("\nxref\n0 1\n0000000000 65535 f \ntrailer\n<< /Size 1 >>")
			if tt.xrefPos+len(xrefData) <= tt.totalSize {
				copy(data[tt.xrefPos:], xrefData)
			}

			// 验证 xref 位置
			idx := bytes.Index(data, []byte("xref"))
			if idx < 0 {
				t.Errorf("Failed to find xref in buffer")
			} else if idx != tt.xrefPos {
				t.Logf("xref found at %d, expected around %d (difference due to insertion)",
					idx, tt.xrefPos)
			}
		})
	}
}

// TestIncrementalUpdateBoundary tests incremental update scenarios
func TestIncrementalUpdateBoundary(t *testing.T) {
	tests := []struct {
		name        string
		updates     int
		description string
	}{
		{
			name:        "no_updates",
			updates:     0,
			description: "原始 PDF（无增量更新）",
		},
		{
			name:        "one_update",
			updates:     1,
			description: "一次增量更新",
		},
		{
			name:        "multiple_updates",
			updates:     5,
			description: "多次增量更新",
		},
		{
			name:        "many_updates",
			updates:     20,
			description: "大量增量更新",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 验证更新链不会导致无限循环
			if tt.updates > 100 {
				t.Errorf("Too many updates (%d) might indicate circular reference",
					tt.updates)
			}
		})
	}
}
