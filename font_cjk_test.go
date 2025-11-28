// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"testing"
)

func TestNewExtendedCIDFont(t *testing.T) {
	// Create a basic ExtendedCIDFont
	ecf := NewExtendedCIDFont(Value{})
	if ecf == nil {
		t.Fatal("NewExtendedCIDFont returned nil")
	}

	// Test basic functionality - should use default values
	if ecf.GetWidth(0) == 0 { // should have some default
		t.Errorf("Expected non-zero default width, got %d", ecf.GetWidth(0))
	}

	// Test vertical metrics - should use default DW2 values
	vWidth := ecf.VerticalWidth(0)
	if vWidth == 0 { // should have some default
		t.Errorf("Expected non-zero default vertical width, got %f", vWidth)
	}

	_, vy := ecf.VerticalOrigin(0)
	if vy == 0 { // should have some default
		t.Errorf("Expected non-zero default vertical Y origin, got %f", vy)
	}
}

func TestCJKFontDetection(t *testing.T) {
	testCases := []struct {
		fontName string
		expected bool
	}{
		{"SimSun", true},
		{"MS-Gothic", true},
		{"Batang", true},
		{"Arial", false},
		{"Times-Roman", false},
		{"Helvetica", false},
	}

	for _, tc := range testCases {
		result := IsCJKFont(tc.fontName)
		if result != tc.expected {
			t.Errorf("IsCJKFont(%s) = %v, expected %v", tc.fontName, result, tc.expected)
		}
	}
}

func TestCJKOrderingDetection(t *testing.T) {
	testCases := []struct {
		fontName string
		expected string
	}{
		{"SimSun", "GB1"},
		{"SimHei", "GB1"},
		{"MS-Gothic", "Japan1"},
		{"MS-Mincho", "Japan1"},
		{"Batang", "Korea1"},
		{"Arial", ""},
	}

	for _, tc := range testCases {
		result := DetectCJKOrdering(tc.fontName)
		if result != tc.expected {
			t.Errorf("DetectCJKOrdering(%s) = %s, expected %s", tc.fontName, result, tc.expected)
		}
	}
}

func TestCJKTextProcessor(t *testing.T) {
	ecf := NewExtendedCIDFont(Value{})
	processor := NewCJKTextProcessor(ecf, true) // vertical

	text := "Hello（世界）"
	processed := processor.ProcessText(text)

	// For vertical text, punctuation should be replaced with vertical variants
	if processed == text {
		t.Error("Expected text to be processed for vertical writing")
	}

	// Test glyph metrics
	metrics := processor.GetGlyphMetrics(0)
	if metrics.Width == 0 {
		t.Error("Expected non-zero width")
	}
}

func BenchmarkNewExtendedCIDFont(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewExtendedCIDFont(Value{})
	}
}

func BenchmarkExtendedCIDFont_GetWidth(b *testing.B) {
	ecf := NewExtendedCIDFont(Value{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ecf.GetWidth(i % 100)
	}
}

func BenchmarkCJKFontDetection(b *testing.B) {
	fontNames := []string{"SimSun", "Arial", "MS-Gothic", "Times-Roman"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = IsCJKFont(fontNames[i%len(fontNames)])
	}
}
