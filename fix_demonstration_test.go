package pdf

import (
	"bytes"
	"testing"
)

// TestFixDemonstration demonstrates the fix working correctly
func TestFixDemonstration(t *testing.T) {
	t.Log("=== 演示修复效果 ===\n")

	// 模拟之前会失败的场景
	testCases := []struct {
		name     string
		pdfEnd   string
		scenario string
	}{
		{
			name:     "标准 PDF 末尾",
			pdfEnd:   "trailer\n<< /Size 10 /Root 1 0 R >>\nstartxref\n12345\n%%EOF\n",
			scenario: "正常的 PDF 文件末尾，有完整的换行符",
		},
		{
			name:     "TrimRight 后的场景（之前会失败）",
			pdfEnd:   "trailer\n<< /Size 10 /Root 1 0 R >>\nstartxref\n12345\n%%EOF",
			scenario: "TrimRight 移除尾部换行后，startxref 后面紧跟数字",
		},
		{
			name:     "startxref 在末尾（之前会失败）",
			pdfEnd:   "trailer\n<< /Size 10 /Root 1 0 R >>\nstartxref",
			scenario: "startxref 正好在 buffer 末尾，没有任何后续内容",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("\n场景：%s", tc.scenario)
			t.Logf("原始内容：%q", tc.pdfEnd)

			// 模拟 NewReaderEncrypted 的处理
			buf := []byte(tc.pdfEnd)
			buf = bytes.TrimRight(buf, "\r\n\t ")

			t.Logf("TrimRight 后：%q (长度: %d)", string(buf), len(buf))

			// 测试 findLastLine
			i := findLastLine(buf, "startxref")

			if i >= 0 {
				t.Logf("✅ 成功找到 startxref，位置: %d", i)
				t.Logf("   找到的内容: %q", string(buf[i:min(i+9, len(buf))]))
			} else {
				t.Errorf("❌ 未找到 startxref - 修复失败！")
			}
		})
	}

	t.Log("\n=== 修复验证完成 ===")
}

// TestBeforeAndAfter 对比修复前后的行为
func TestBeforeAndAfter(t *testing.T) {
	t.Log("=== 修复前后对比 ===\n")

	// 这是一个之前会失败的典型场景
	pdfEnd := "trailer\n<< /Size 10 /Root 1 0 R >>\nstartxref\n116\n%%EOF"

	t.Logf("测试数据：%q", pdfEnd)

	// 步骤 1：初始 buffer
	buf := []byte(pdfEnd)
	t.Logf("\n步骤 1 - 读取 PDF 末尾")
	t.Logf("  Buffer 长度: %d", len(buf))

	// 步骤 2：TrimRight（这是导致问题的关键步骤）
	buf = bytes.TrimRight(buf, "\r\n\t ")
	t.Logf("\n步骤 2 - TrimRight 后")
	t.Logf("  Buffer 长度: %d", len(buf))
	startIdx := len(buf) - 20
	if startIdx < 0 {
		startIdx = 0
	}
	t.Logf("  Buffer 末尾 20 字符: %q", string(buf[startIdx:]))

	// 步骤 3：查找 startxref
	idx := bytes.LastIndex(buf, []byte("startxref"))
	t.Logf("\n步骤 3 - bytes.LastIndex 查找")
	t.Logf("  找到位置: %d", idx)
	if idx >= 0 {
		t.Logf("  idx + 9 = %d", idx+9)
		t.Logf("  len(buf) = %d", len(buf))
		t.Logf("  修复前的条件 (idx+9 >= len(buf)): %v", idx+9 >= len(buf))

		if idx+9 >= len(buf) {
			t.Logf("  ❌ 修复前：这里会返回 -1（失败）")
		}
	}

	// 步骤 4：使用修复后的 findLastLine
	i := findLastLine(buf, "startxref")
	t.Logf("\n步骤 4 - 使用修复后的 findLastLine")
	if i >= 0 {
		t.Logf("  ✅ 修复后：成功找到，位置: %d", i)
		t.Logf("  说明：修复允许 startxref 在 buffer 末尾附近")
	} else {
		t.Errorf("  ❌ 修复失败：仍然找不到 startxref")
	}

	t.Log("\n=== 对比完成 ===")
}
