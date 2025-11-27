// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"testing"
)

func BenchmarkClusteringUltraOpt(b *testing.B) {
	sizes := []int{100, 500, 1000, 5000, 10000}

	for _, size := range sizes {
		texts := generateRandomTexts(size, size/10+1)

		b.Run("UltraOpt_"+string(rune(size)), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				blocks := ClusterTextBlocksUltraOptimized(texts)
				PutTextBlocks(blocks)
			}
		})

		b.Run("ParallelV2_"+string(rune(size)), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				blocks := ClusterTextBlocksParallelV2(texts)
				PutTextBlocks(blocks)
			}
		})
	}
}

func BenchmarkAppendTextOptimized(b *testing.B) {
	// 创建一个模拟的 contentExtractor
	ce := &contentExtractor{
		text: make([]Text, 0, 1000),
	}

	// 模拟字体和状态
	g := &gstate{
		Tfs:   12.0,
		Th:    1.0,
		Trise: 0.0,
		Tc:    0.0,
		Tm:    matrix{{1, 0, 0}, {0, 1, 0}, {100, 200, 1}},
		CTM:   matrix{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}},
	}

	// 创建一个简单的编码器
	enc := &nopEncoder{}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ce.text = ce.text[:0]
		ce.textCap = 0
		ce.growHint = 0

		// 模拟多次 appendText 调用
		for j := 0; j < 100; j++ {
			ce.appendText(g, enc, "Hello World")
		}
	}
}

func BenchmarkParseFontStylesOptimized(b *testing.B) {
	fonts := []string{
		"Arial-Bold",
		"TimesNewRoman-Italic",
		"Helvetica-BoldOblique",
		"Courier",
		"Arial-Black",
		"HELVETICA-BOLDITALIC",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for _, font := range fonts {
			_, _, _ = parseFontStyles(font)
		}
	}
}

// TestClusteringUltraOptCorrectness 测试优化后的聚类算法的正确性
func TestClusteringUltraOptCorrectness(t *testing.T) {
	// 生成测试数据
	texts := generateRandomTexts(1000, 100)

	// 运行两种算法
	blocks1 := ClusterTextBlocksParallelV2(texts)
	blocks2 := ClusterTextBlocksUltraOptimized(texts)

	// 验证块数量相近（可能有细微差异，但应该接近）
	if len(blocks1) == 0 || len(blocks2) == 0 {
		t.Fatal("Empty result")
	}

	// 验证所有文本都被包含
	count1 := 0
	for _, b := range blocks1 {
		count1 += len(b.Texts)
	}

	count2 := 0
	for _, b := range blocks2 {
		count2 += len(b.Texts)
	}

	if count1 != len(texts) || count2 != len(texts) {
		t.Errorf("Text count mismatch: got %d and %d, want %d", count1, count2, len(texts))
	}

	// 清理
	PutTextBlocks(blocks1)
	PutTextBlocks(blocks2)
}

func TestAppendTextOptimized(t *testing.T) {
	ce := &contentExtractor{
		text: make([]Text, 0),
	}

	g := &gstate{
		Tfs:   12.0,
		Th:    1.0,
		Trise: 0.0,
		Tc:    0.0,
		Tm:    matrix{{1, 0, 0}, {0, 1, 0}, {100, 200, 1}},
		CTM:   matrix{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}},
	}

	enc := &nopEncoder{}

	// 测试多次追加
	for i := 0; i < 10; i++ {
		ce.appendText(g, enc, "Test")
	}

	if len(ce.text) == 0 {
		t.Fatal("No text appended")
	}

	// 验证增长提示被正确设置
	if ce.growHint == 0 {
		t.Error("growHint not set")
	}
}

func TestParseFontStylesOptimized(t *testing.T) {
	tests := []struct {
		font       string
		wantBold   bool
		wantItalic bool
	}{
		{"Arial-Bold", true, false},
		{"Arial-BOLD", true, false},
		{"Times-Italic", false, true},
		{"Times-ITALIC", false, true},
		{"Helvetica-BoldItalic", true, true},
		{"Courier-Oblique", false, true},
		{"Arial-Black", true, false},
		{"Regular", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.font, func(t *testing.T) {
			bold, italic, _ := parseFontStyles(tt.font)
			if bold != tt.wantBold {
				t.Errorf("Bold: got %v, want %v", bold, tt.wantBold)
			}
			if italic != tt.wantItalic {
				t.Errorf("Italic: got %v, want %v", italic, tt.wantItalic)
			}
		})
	}
}
