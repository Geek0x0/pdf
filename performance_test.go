// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"strings"
	"testing"
)

// Test performance optimizations

func TestOptimizedGetPlainText(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		t.Skip("PDF has no pages")
	}

	page := r.Page(1)

	// Test original
	text1, err := page.GetPlainText(nil)
	if err != nil {
		t.Fatalf("GetPlainText failed: %v", err)
	}

	// Test optimized
	text2, err := page.OptimizedGetPlainText(nil)
	if err != nil {
		t.Fatalf("OptimizedGetPlainText failed: %v", err)
	}

	// Results should be identical
	if text1 != text2 {
		t.Errorf("Optimized version produces different output:\nOriginal: %q\nOptimized: %q", text1, text2)
	}
}

func TestBuilderPool(t *testing.T) {
	// Test basic usage
	builder := GetBuilder()
	if builder == nil {
		t.Fatal("GetBuilder returned nil")
	}

	builder.WriteString("test")
	result := builder.String()
	if result != "test" {
		t.Errorf("expected 'test', got %q", result)
	}

	PutBuilder(builder)

	// Get another builder - should be reused
	builder2 := GetBuilder()
	if builder2 == nil {
		t.Fatal("GetBuilder returned nil on second call")
	}

	// Should be reset
	if builder2.Len() != 0 {
		t.Errorf("expected empty builder, got length %d", builder2.Len())
	}

	PutBuilder(builder2)
}

func TestTextPool(t *testing.T) {
	// Test basic usage
	text := GetText()
	if text == nil {
		t.Fatal("GetText returned nil")
	}

	text.S = "test"
	text.X = 10.0
	text.Y = 20.0

	PutText(text)

	// Get another text - should be reused
	text2 := GetText()
	if text2 == nil {
		t.Fatal("GetText returned nil on second call")
	}

	// Should be reset
	if text2.S != "" || text2.X != 0 || text2.Y != 0 {
		t.Errorf("expected reset text, got %+v", text2)
	}

	PutText(text2)
}

func TestFastStringBuilder(t *testing.T) {
	builder := NewFastStringBuilder(100)

	builder.WriteString("Hello ")
	builder.WriteString("World")

	result := builder.String()
	if result != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", result)
	}

	if builder.Len() != 11 {
		t.Errorf("expected length 11, got %d", builder.Len())
	}

	builder.Reset()
	if builder.Len() != 0 {
		t.Errorf("expected length 0 after reset, got %d", builder.Len())
	}
}

func TestLazyPage(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		t.Skip("PDF has no pages")
	}

	lazyPage := NewLazyPage(r, 1)

	// Initially not loaded
	if lazyPage.IsLoaded() {
		t.Error("expected page not to be loaded initially")
	}

	// Load content
	content := lazyPage.GetContent()
	if content == nil {
		t.Fatal("GetContent returned nil")
	}

	// Should be loaded now
	if !lazyPage.IsLoaded() {
		t.Error("expected page to be loaded after GetContent")
	}

	// Second call should return same content
	content2 := lazyPage.GetContent()
	if content2 != content {
		t.Error("expected same content pointer on second call")
	}

	// Release and check
	lazyPage.Release()
	if lazyPage.IsLoaded() {
		t.Error("expected page not to be loaded after Release")
	}
}

func TestLazyPageManager(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		t.Skip("PDF has no pages")
	}

	manager := NewLazyPageManager(r, 2)
	defer manager.Clear()

	// Get first page
	page1 := manager.GetPage(1)
	if page1 == nil {
		t.Fatal("GetPage(1) returned nil")
	}

	// Get second page (if exists)
	if r.NumPage() >= 2 {
		page2 := manager.GetPage(2)
		if page2 == nil {
			t.Fatal("GetPage(2) returned nil")
		}

		// Check stats
		total, loaded := manager.GetStats()
		if total != 2 {
			t.Errorf("expected 2 total pages, got %d", total)
		}
		if loaded != 0 {
			t.Errorf("expected 0 loaded pages, got %d", loaded)
		}
	}

	// Clear all
	manager.Clear()
	total, loaded := manager.GetStats()
	if total != 0 || loaded != 0 {
		t.Errorf("expected all cleared, got total=%d, loaded=%d", total, loaded)
	}
}

func TestBatchExtractText(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		t.Skip("PDF has no pages")
	}

	pageNums := []int{1}
	if r.NumPage() >= 2 {
		pageNums = append(pageNums, 2)
	}

	// Test without lazy loading
	results1, err := r.BatchExtractText(pageNums, false)
	if err != nil {
		t.Fatalf("BatchExtractText (no lazy) failed: %v", err)
	}

	if len(results1) != len(pageNums) {
		t.Errorf("expected %d results, got %d", len(pageNums), len(results1))
	}

	for _, pageNum := range pageNums {
		if text, ok := results1[pageNum]; !ok {
			t.Errorf("missing result for page %d", pageNum)
		} else if len(text) == 0 {
			t.Errorf("empty text for page %d", pageNum)
		}
	}

	// Test with lazy loading
	results2, err := r.BatchExtractText(pageNums, true)
	if err != nil {
		t.Fatalf("BatchExtractText (lazy) failed: %v", err)
	}

	if len(results2) != len(pageNums) {
		t.Errorf("expected %d results, got %d", len(pageNums), len(results2))
	}

	// Results should be identical
	for _, pageNum := range pageNums {
		if results1[pageNum] != results2[pageNum] {
			t.Errorf("different results for page %d:\nNo lazy: %q\nLazy: %q",
				pageNum, results1[pageNum], results2[pageNum])
		}
	}
}

func TestStreamingTextExtractor(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		t.Skip("PDF has no pages")
	}

	extractor := NewStreamingTextExtractor(r, 5)
	defer extractor.Close()

	// Extract page by page
	var allText strings.Builder
	pageCount := 0

	for {
		pageNum, text, hasMore, err := extractor.NextPage()
		if err != nil {
			t.Fatalf("NextPage failed: %v", err)
		}

		if pageNum > 0 {
			pageCount++
			if len(text) == 0 {
				t.Errorf("empty text for page %d", pageNum)
			}
			allText.WriteString(text)
		}

		if !hasMore {
			break
		}
	}

	if pageCount != r.NumPage() {
		t.Errorf("expected %d pages, got %d", r.NumPage(), pageCount)
	}

	// Test progress
	progress := extractor.GetProgress()
	if progress != 1.0 {
		t.Errorf("expected progress 1.0, got %f", progress)
	}

	// Test reset
	extractor.Reset()
	progress = extractor.GetProgress()
	if progress != 0.0 {
		t.Errorf("expected progress 0.0 after reset, got %f", progress)
	}
}

func TestStreamingExtractorBatch(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		t.Skip("PDF has no pages")
	}

	extractor := NewStreamingTextExtractor(r, 2)
	defer extractor.Close()

	// Extract in batches
	batchCount := 0
	totalPages := 0

	for {
		results, hasMore, err := extractor.NextBatch()
		if err != nil {
			t.Fatalf("NextBatch failed: %v", err)
		}

		if len(results) > 0 {
			batchCount++
			totalPages += len(results)

			for pageNum, text := range results {
				if len(text) == 0 {
					t.Errorf("empty text for page %d", pageNum)
				}
			}
		}

		if !hasMore {
			break
		}
	}

	if totalPages != r.NumPage() {
		t.Errorf("expected %d pages, got %d", r.NumPage(), totalPages)
	}
}

func TestOptimizedGetTextByRow(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		t.Skip("PDF has no pages")
	}

	page := r.Page(1)

	// Test optimized version
	rows, err := page.OptimizedGetTextByRow()
	if err != nil {
		t.Fatalf("OptimizedGetTextByRow failed: %v", err)
	}

	if len(rows) == 0 {
		t.Error("expected non-empty rows")
	}

	// Compare with original
	rows2, err := page.GetTextByRow()
	if err != nil {
		t.Fatalf("GetTextByRow failed: %v", err)
	}

	if len(rows) != len(rows2) {
		t.Errorf("different number of rows: optimized=%d, original=%d", len(rows), len(rows2))
	}
}

func TestOptimizedGetTextByColumn(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		t.Skip("PDF has no pages")
	}

	page := r.Page(1)

	// Test optimized version
	columns, err := page.OptimizedGetTextByColumn()
	if err != nil {
		t.Fatalf("OptimizedGetTextByColumn failed: %v", err)
	}

	if len(columns) == 0 {
		t.Error("expected non-empty columns")
	}

	// Compare with original
	columns2, err := page.GetTextByColumn()
	if err != nil {
		t.Fatalf("GetTextByColumn failed: %v", err)
	}

	if len(columns) != len(columns2) {
		t.Errorf("different number of columns: optimized=%d, original=%d", len(columns), len(columns2))
	}
}

func TestTextPoolBySize(t *testing.T) {
	// Test small text pool
	smallText := GetTextBySize(50)
	if smallText == nil {
		t.Fatal("GetTextBySize(50) returned nil")
	}

	// Test large text pool
	largeText := GetTextBySize(150)
	if largeText == nil {
		t.Fatal("GetTextBySize(150) returned nil")
	}

	// Return them
	PutText(smallText)
	PutText(largeText)
}
