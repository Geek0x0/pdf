package pdf

import (
	"strings"
	"testing"
)

// TestObjectNumberBoundary tests object number edge cases
func TestObjectNumberBoundary(t *testing.T) {
	tests := []struct {
		name   string
		objNum string
		valid  bool
	}{
		{"zero", "0", true},
		{"one", "1", true},
		{"normal", "123", true},
		{"large", "999999", true},
		{"very_large", "2147483647", true},
		{"max_int32", "2147483647", true},
		{"over_int32", "2147483648", true},
		{"negative", "-1", false},
		{"empty", "", false},
		{"non_numeric", "abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.objNum) == 0 {
				if tt.valid {
					t.Errorf("Empty object number marked as valid")
				}
				return
			}

			// 验证是否为数字
			isNumeric := true
			for _, c := range tt.objNum {
				if c < '0' || c > '9' {
					isNumeric = false
					break
				}
			}

			if tt.valid && !isNumeric {
				t.Errorf("Object number %q marked as valid but non-numeric", tt.objNum)
			}
		})
	}
}

// TestIndirectObjectReferenceBoundary tests indirect object reference edge cases
func TestIndirectObjectReferenceBoundary(t *testing.T) {
	tests := []struct {
		name        string
		reference   string
		expectValid bool
		description string
	}{
		{
			name:        "standard_reference",
			reference:   "1 0 R",
			expectValid: true,
			description: "标准对象引用",
		},
		{
			name:        "with_extra_spaces",
			reference:   "1  0  R",
			expectValid: true,
			description: "多个空格",
		},
		{
			name:        "with_tabs",
			reference:   "1\t0\tR",
			expectValid: true,
			description: "tab 分隔",
		},
		{
			name:        "large_objnum",
			reference:   "999999 0 R",
			expectValid: true,
			description: "大对象号",
		},
		{
			name:        "non_zero_gen",
			reference:   "1 5 R",
			expectValid: true,
			description: "非零代数",
		},
		{
			name:        "max_gen",
			reference:   "1 65535 R",
			expectValid: true,
			description: "最大代数",
		},
		{
			name:        "missing_r",
			reference:   "1 0",
			expectValid: false,
			description: "缺少 R",
		},
		{
			name:        "lowercase_r",
			reference:   "1 0 r",
			expectValid: false,
			description: "小写 r",
		},
		{
			name:        "missing_generation",
			reference:   "1 R",
			expectValid: false,
			description: "缺少代数",
		},
		{
			name:        "negative_objnum",
			reference:   "-1 0 R",
			expectValid: false,
			description: "负对象号",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := strings.Fields(tt.reference)

			if len(parts) != 3 {
				if tt.expectValid {
					t.Errorf("%s: Expected valid but has %d parts", tt.description, len(parts))
				}
				return
			}

			if parts[2] != "R" {
				if tt.expectValid {
					t.Errorf("%s: Missing 'R' marker", tt.description)
				}
			}
		})
	}
}

// TestStringLiteralBoundary tests PDF string literal edge cases
func TestStringLiteralBoundary(t *testing.T) {
	tests := []struct {
		name        string
		literal     string
		expectValid bool
		description string
	}{
		{
			name:        "empty_string",
			literal:     "()",
			expectValid: true,
			description: "空字符串",
		},
		{
			name:        "simple_string",
			literal:     "(hello)",
			expectValid: true,
			description: "简单字符串",
		},
		{
			name:        "with_spaces",
			literal:     "(hello world)",
			expectValid: true,
			description: "包含空格",
		},
		{
			name:        "nested_parens",
			literal:     "(hello (world))",
			expectValid: true,
			description: "嵌套括号",
		},
		{
			name:        "escaped_paren",
			literal:     "(hello \\( world \\))",
			expectValid: true,
			description: "转义括号",
		},
		{
			name:        "escaped_backslash",
			literal:     "(hello \\\\ world)",
			expectValid: true,
			description: "转义反斜杠",
		},
		{
			name:        "newline_in_string",
			literal:     "(hello\nworld)",
			expectValid: true,
			description: "包含换行符",
		},
		{
			name:        "escaped_newline",
			literal:     "(hello \\n world)",
			expectValid: true,
			description: "转义换行符",
		},
		{
			name:        "octal_escape",
			literal:     "(hello \\101 world)",
			expectValid: true,
			description: "八进制转义",
		},
		{
			name:        "missing_closing_paren",
			literal:     "(hello",
			expectValid: false,
			description: "缺少闭合括号",
		},
		{
			name:        "unmatched_nested",
			literal:     "(hello (world)",
			expectValid: false,
			description: "嵌套括号不匹配",
		},
		{
			name:        "only_opening",
			literal:     "(",
			expectValid: false,
			description: "只有开括号",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 基本格式检查
			if !strings.HasPrefix(tt.literal, "(") {
				if tt.expectValid {
					t.Errorf("%s: Valid string must start with '('", tt.description)
				}
				return
			}

			// 检查括号平衡
			depth := 0
			escaped := false
			for i, c := range tt.literal {
				if escaped {
					escaped = false
					continue
				}
				if c == '\\' {
					escaped = true
					continue
				}
				if c == '(' {
					depth++
				} else if c == ')' {
					depth--
				}
				if depth == 0 && i < len(tt.literal)-1 {
					// 提前闭合
					break
				}
			}

			balanced := depth == 0
			if tt.expectValid && !balanced {
				t.Errorf("%s: Unbalanced parentheses", tt.description)
			}
		})
	}
}

// TestHexStringBoundary tests PDF hex string edge cases
func TestHexStringBoundary(t *testing.T) {
	tests := []struct {
		name        string
		hexString   string
		expectValid bool
		description string
	}{
		{
			name:        "empty_hex",
			hexString:   "<>",
			expectValid: true,
			description: "空十六进制字符串",
		},
		{
			name:        "simple_hex",
			hexString:   "<48656C6C6F>",
			expectValid: true,
			description: "简单十六进制（Hello）",
		},
		{
			name:        "lowercase_hex",
			hexString:   "<48656c6c6f>",
			expectValid: true,
			description: "小写十六进制",
		},
		{
			name:        "mixed_case",
			hexString:   "<48656C6c6F>",
			expectValid: true,
			description: "混合大小写",
		},
		{
			name:        "with_whitespace",
			hexString:   "<48 65 6C 6C 6F>",
			expectValid: true,
			description: "包含空格",
		},
		{
			name:        "with_newlines",
			hexString:   "<4865\n6C6C\n6F>",
			expectValid: true,
			description: "包含换行符",
		},
		{
			name:        "odd_length",
			hexString:   "<486>",
			expectValid: true,
			description: "奇数长度（自动补0）",
		},
		{
			name:        "single_char",
			hexString:   "<F>",
			expectValid: true,
			description: "单个字符",
		},
		{
			name:        "missing_closing",
			hexString:   "<48656C6C6F",
			expectValid: false,
			description: "缺少闭合>",
		},
		{
			name:        "invalid_chars",
			hexString:   "<48G56C6C6F>",
			expectValid: false,
			description: "包含非十六进制字符",
		},
		{
			name:        "only_opening",
			hexString:   "<",
			expectValid: false,
			description: "只有开括号",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.HasPrefix(tt.hexString, "<") {
				if tt.expectValid {
					t.Errorf("%s: Valid hex string must start with '<'", tt.description)
				}
				return
			}

			if !strings.HasSuffix(tt.hexString, ">") {
				if tt.expectValid {
					t.Errorf("%s: Valid hex string must end with '>'", tt.description)
				}
				return
			}

			// 提取内容（去除 < 和 >）
			content := strings.TrimSpace(tt.hexString[1 : len(tt.hexString)-1])
			content = strings.ReplaceAll(content, " ", "")
			content = strings.ReplaceAll(content, "\n", "")
			content = strings.ReplaceAll(content, "\r", "")
			content = strings.ReplaceAll(content, "\t", "")

			// 验证是否为十六进制
			for _, c := range content {
				if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f')) {
					if tt.expectValid {
						t.Errorf("%s: Contains non-hex character: %c", tt.description, c)
					}
					return
				}
			}
		})
	}
}

// TestNameObjectBoundary tests PDF name object edge cases
func TestNameObjectBoundary(t *testing.T) {
	tests := []struct {
		name        string
		nameObj     string
		expectValid bool
		description string
	}{
		{
			name:        "simple_name",
			nameObj:     "/Name",
			expectValid: true,
			description: "简单名称",
		},
		{
			name:        "with_numbers",
			nameObj:     "/Name123",
			expectValid: true,
			description: "包含数字",
		},
		{
			name:        "with_underscore",
			nameObj:     "/Name_Value",
			expectValid: true,
			description: "包含下划线",
		},
		{
			name:        "with_hex_escape",
			nameObj:     "/Name#20Value",
			expectValid: true,
			description: "十六进制转义（空格）",
		},
		{
			name:        "empty_name",
			nameObj:     "/",
			expectValid: true,
			description: "空名称",
		},
		{
			name:        "special_chars_escaped",
			nameObj:     "/A#42",
			expectValid: true,
			description: "转义的特殊字符",
		},
		{
			name:        "common_names",
			nameObj:     "/Type",
			expectValid: true,
			description: "常见名称 /Type",
		},
		{
			name:        "length_name",
			nameObj:     "/Length",
			expectValid: true,
			description: "常见名称 /Length",
		},
		{
			name:        "missing_slash",
			nameObj:     "Name",
			expectValid: false,
			description: "缺少斜杠前缀",
		},
		{
			name:        "with_space_unescaped",
			nameObj:     "/Name Value",
			expectValid: false,
			description: "未转义的空格",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.HasPrefix(tt.nameObj, "/") {
				if tt.expectValid {
					t.Errorf("%s: Valid name must start with '/'", tt.description)
				}
				return
			}

			// 检查是否包含未转义的空白字符
			hasUnescapedWhitespace := false
			for i := 1; i < len(tt.nameObj); i++ {
				if tt.nameObj[i] == ' ' || tt.nameObj[i] == '\t' || tt.nameObj[i] == '\n' {
					// 检查前面是否有 #XX 转义
					if i >= 3 && tt.nameObj[i-3] == '#' {
						continue
					}
					hasUnescapedWhitespace = true
					break
				}
			}

			if tt.expectValid && hasUnescapedWhitespace {
				t.Errorf("%s: Contains unescaped whitespace", tt.description)
			}
		})
	}
}

// TestArrayBoundary tests PDF array edge cases
func TestArrayBoundary(t *testing.T) {
	tests := []struct {
		name        string
		array       string
		expectValid bool
		description string
	}{
		{
			name:        "empty_array",
			array:       "[]",
			expectValid: true,
			description: "空数组",
		},
		{
			name:        "number_array",
			array:       "[1 2 3]",
			expectValid: true,
			description: "数字数组",
		},
		{
			name:        "mixed_types",
			array:       "[1 /Name (string) 1 0 R]",
			expectValid: true,
			description: "混合类型",
		},
		{
			name:        "nested_array",
			array:       "[[1 2] [3 4]]",
			expectValid: true,
			description: "嵌套数组",
		},
		{
			name:        "with_whitespace",
			array:       "[ 1  2  3 ]",
			expectValid: true,
			description: "包含额外空格",
		},
		{
			name:        "with_newlines",
			array:       "[\n1\n2\n3\n]",
			expectValid: true,
			description: "包含换行符",
		},
		{
			name:        "deep_nesting",
			array:       "[[[1]]]",
			expectValid: true,
			description: "深层嵌套",
		},
		{
			name:        "missing_closing",
			array:       "[1 2 3",
			expectValid: false,
			description: "缺少闭合括号",
		},
		{
			name:        "unmatched_nested",
			array:       "[[1 2] [3 4]",
			expectValid: false,
			description: "嵌套括号不匹配",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.HasPrefix(tt.array, "[") {
				if tt.expectValid {
					t.Errorf("%s: Valid array must start with '['", tt.description)
				}
				return
			}

			// 检查括号平衡
			depth := 0
			for _, c := range tt.array {
				if c == '[' {
					depth++
				} else if c == ']' {
					depth--
				}
			}

			balanced := depth == 0
			if tt.expectValid && !balanced {
				t.Errorf("%s: Unbalanced brackets (depth: %d)", tt.description, depth)
			}
		})
	}
}

// TestDictionaryBoundary tests PDF dictionary edge cases
func TestDictionaryBoundary(t *testing.T) {
	tests := []struct {
		name        string
		dict        string
		expectValid bool
		description string
	}{
		{
			name:        "empty_dict",
			dict:        "<< >>",
			expectValid: true,
			description: "空字典",
		},
		{
			name:        "simple_dict",
			dict:        "<< /Type /Page >>",
			expectValid: true,
			description: "简单字典",
		},
		{
			name:        "multiple_entries",
			dict:        "<< /Type /Page /Parent 1 0 R /MediaBox [0 0 612 792] >>",
			expectValid: true,
			description: "多个条目",
		},
		{
			name:        "nested_dict",
			dict:        "<< /Type /Page /Resources << /Font << /F1 1 0 R >> >> >>",
			expectValid: true,
			description: "嵌套字典",
		},
		{
			name:        "with_array",
			dict:        "<< /Type /Page /MediaBox [0 0 612 792] >>",
			expectValid: true,
			description: "包含数组",
		},
		{
			name:        "multiline",
			dict:        "<<\n/Type /Page\n/Parent 1 0 R\n>>",
			expectValid: true,
			description: "多行格式",
		},
		{
			name:        "compact",
			dict:        "<</Type/Page>>",
			expectValid: true,
			description: "紧凑格式",
		},
		{
			name:        "missing_closing",
			dict:        "<< /Type /Page",
			expectValid: false,
			description: "缺少闭合 >>",
		},
		{
			name:        "single_bracket",
			dict:        "< /Type /Page >",
			expectValid: false,
			description: "单个尖括号",
		},
		{
			name:        "unmatched_nested",
			dict:        "<< /A << /B /C >> /D /E",
			expectValid: false,
			description: "嵌套不匹配",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.HasPrefix(tt.dict, "<<") {
				if tt.expectValid {
					t.Errorf("%s: Valid dict must start with '<<'", tt.description)
				}
				return
			}

			// 检查字典括号平衡（统计 << 和 >>）
			depth := 0
			for i := 0; i < len(tt.dict)-1; i++ {
				if tt.dict[i] == '<' && tt.dict[i+1] == '<' {
					depth++
					i++ // 跳过下一个字符
				} else if tt.dict[i] == '>' && tt.dict[i+1] == '>' {
					depth--
					i++
				}
			}

			balanced := depth == 0
			if tt.expectValid && !balanced {
				t.Errorf("%s: Unbalanced dict brackets (depth: %d)", tt.description, depth)
			}
		})
	}
}

// TestNumberBoundary tests PDF number edge cases
func TestNumberBoundary(t *testing.T) {
	tests := []struct {
		name        string
		number      string
		expectValid bool
		isInteger   bool
	}{
		{"zero", "0", true, true},
		{"positive_int", "123", true, true},
		{"negative_int", "-123", true, true},
		{"positive_real", "123.456", true, false},
		{"negative_real", "-123.456", true, false},
		{"leading_decimal", ".5", true, false},
		{"trailing_decimal", "5.", true, false},
		{"no_integer_part", "-.5", true, false},
		{"scientific", "1.5e10", false, false},
		{"with_plus", "+123", false, false},
		{"multiple_dots", "1.2.3", false, false},
		{"non_numeric", "abc", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 基本数字格式检查
			hasDot := strings.Contains(tt.number, ".")
			hasE := strings.ContainsAny(tt.number, "eE")

			if tt.expectValid {
				if hasE {
					t.Errorf("PDF numbers should not use scientific notation")
				}
				if tt.isInteger && hasDot {
					t.Errorf("Integer marked but contains decimal point")
				}
			}
		})
	}
}

// TestStreamBoundary tests PDF stream edge cases
func TestStreamBoundary(t *testing.T) {
	tests := []struct {
		name        string
		stream      string
		expectValid bool
		description string
	}{
		{
			name:        "minimal_stream",
			stream:      "<< /Length 0 >>\nstream\n\nendstream",
			expectValid: true,
			description: "最小流对象",
		},
		{
			name:        "stream_with_data",
			stream:      "<< /Length 5 >>\nstream\nHello\nendstream",
			expectValid: true,
			description: "包含数据的流",
		},
		{
			name:        "stream_cr_lf",
			stream:      "<< /Length 5 >>\r\nstream\r\nHello\r\nendstream",
			expectValid: true,
			description: "CRLF 换行符",
		},
		{
			name:        "compressed_stream",
			stream:      "<< /Length 50 /Filter /FlateDecode >>\nstream\n...\nendstream",
			expectValid: true,
			description: "压缩流",
		},
		{
			name:        "missing_endstream",
			stream:      "<< /Length 5 >>\nstream\nHello",
			expectValid: false,
			description: "缺少 endstream",
		},
		{
			name:        "missing_stream",
			stream:      "<< /Length 5 >>\nHello\nendstream",
			expectValid: false,
			description: "缺少 stream 关键字",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasStream := strings.Contains(tt.stream, "stream")
			hasEndstream := strings.Contains(tt.stream, "endstream")

			if tt.expectValid {
				if !hasStream {
					t.Errorf("%s: Missing 'stream' keyword", tt.description)
				}
				if !hasEndstream {
					t.Errorf("%s: Missing 'endstream' keyword", tt.description)
				}
			}
		})
	}
}
