// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"strings"
	"testing"
	"time"
)

// TestMetadataExtraction tests basic metadata extraction
func TestMetadataExtraction(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test, file not available: %v", err)
		return
	}
	defer f.Close()

	meta, err := r.GetMetadata()
	if err != nil {
		t.Fatalf("GetMetadata() error: %v", err)
	}

	// Just verify we got something, don't require specific values
	t.Logf("Title: %s", meta.Title)
	t.Logf("Author: %s", meta.Author)
	t.Logf("Creator: %s", meta.Creator)
	t.Logf("Producer: %s", meta.Producer)

	if !meta.CreationDate.IsZero() {
		t.Logf("Created: %s", meta.CreationDate)
	}
	if !meta.ModDate.IsZero() {
		t.Logf("Modified: %s", meta.ModDate)
	}
}

// TestMetadataString tests the String() method
func TestMetadataString(t *testing.T) {
	meta := Metadata{
		Title:        "Test Document",
		Author:       "Test Author",
		Subject:      "Testing",
		Keywords:     []string{"test", "pdf"},
		Creator:      "TestApp",
		Producer:     "TestPDF",
		CreationDate: time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC),
		ModDate:      time.Date(2024, 3, 16, 14, 45, 0, 0, time.UTC),
		Trapped:      "False",
		Custom:       map[string]string{"CustomField": "CustomValue"},
	}

	s := meta.String()

	// Check that all fields are present in string representation
	if !strings.Contains(s, "Test Document") {
		t.Error("String() missing title")
	}
	if !strings.Contains(s, "Test Author") {
		t.Error("String() missing author")
	}
	if !strings.Contains(s, "test, pdf") {
		t.Error("String() missing keywords")
	}
	if !strings.Contains(s, "CustomField") {
		t.Error("String() missing custom field")
	}
}

// TestTextClassifier tests the text block classifier
func TestTextClassifier(t *testing.T) {
	texts := []Text{
		// Title (large font, centered, bold)
		{Font: "Arial-Bold", FontSize: 24, X: 250, Y: 750, S: "Document Title", Bold: true},

		// Paragraph
		{Font: "Arial", FontSize: 12, X: 100, Y: 700, S: "This is a paragraph of text."},
		{Font: "Arial", FontSize: 12, X: 100, Y: 685, S: "It continues on the next line."},

		// List item
		{Font: "Arial", FontSize: 12, X: 120, Y: 650, S: "1. First item in list"},
		{Font: "Arial", FontSize: 12, X: 120, Y: 635, S: "2. Second item in list"},

		// Caption (italic)
		{Font: "Arial-Italic", FontSize: 10, X: 100, Y: 400, S: "Figure 1: Test caption", Italic: true},

		// Footnote (small font at bottom)
		{Font: "Arial", FontSize: 8, X: 100, Y: 50, S: "This is a footnote."},
	}

	classifier := NewTextClassifier(texts, 612, 792)
	blocks := classifier.ClassifyBlocks()

	if len(blocks) == 0 {
		t.Fatal("ClassifyBlocks() returned no blocks")
	}

	// Verify we got different block types
	types := make(map[BlockType]bool)
	for _, block := range blocks {
		types[block.Type] = true
		t.Logf("Block: %s - %q", block.Type, block.Text)
	}

	// We should have at least a title and a paragraph
	if !types[BlockTitle] && !types[BlockParagraph] {
		t.Error("Expected to find at least Title or Paragraph blocks")
	}
}

// TestExtractorBuilder tests the extractor builder pattern
func TestExtractorBuilder(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test, file not available: %v", err)
		return
	}
	defer f.Close()

	// Test plain text extraction
	text, err := NewExtractor(r).
		Mode(ModePlain).
		Pages(1).
		ExtractText()
	if err != nil {
		t.Fatalf("ExtractText() error: %v", err)
	}
	if text == "" {
		t.Error("ExtractText() returned empty string")
	}
	t.Logf("Extracted %d characters", len(text))

	// Test styled text extraction
	texts, err := NewExtractor(r).
		Mode(ModeStyled).
		Pages(1).
		ExtractStyledTexts()
	if err != nil {
		t.Fatalf("ExtractStyledTexts() error: %v", err)
	}
	if len(texts) == 0 {
		t.Error("ExtractStyledTexts() returned no texts")
	}
	t.Logf("Extracted %d text runs", len(texts))

	// Test structured extraction
	blocks, err := NewExtractor(r).
		Mode(ModeStructured).
		Pages(1).
		ExtractStructured()
	if err != nil {
		t.Fatalf("ExtractStructured() error: %v", err)
	}
	if len(blocks) == 0 {
		t.Error("ExtractStructured() returned no blocks")
	}
	t.Logf("Extracted %d classified blocks", len(blocks))
}

// TestExtractorWithMetadata tests full extraction with metadata
func TestExtractorWithMetadata(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test, file not available: %v", err)
		return
	}
	defer f.Close()

	result, err := NewExtractor(r).
		Mode(ModePlain).
		Pages(1).
		Extract()
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}

	if result.PageCount == 0 {
		t.Error("Extract() returned zero pages")
	}
	if result.Text == "" {
		t.Error("Extract() returned empty text")
	}

	t.Logf("Metadata: Title=%s, Author=%s", result.Metadata.Title, result.Metadata.Author)
	t.Logf("Extracted %d pages, %d characters", result.PageCount, len(result.Text))
}

// TestGetTextByType tests filtering blocks by type
func TestGetTextByType(t *testing.T) {
	blocks := []ClassifiedBlock{
		{Type: BlockTitle, Text: "Title 1"},
		{Type: BlockParagraph, Text: "Paragraph 1"},
		{Type: BlockTitle, Text: "Title 2"},
		{Type: BlockList, Text: "List item"},
		{Type: BlockParagraph, Text: "Paragraph 2"},
	}

	titles := GetTextByType(blocks, BlockTitle)
	if len(titles) != 2 {
		t.Errorf("GetTextByType(BlockTitle) = %d blocks, want 2", len(titles))
	}

	paragraphs := GetTextByType(blocks, BlockParagraph)
	if len(paragraphs) != 2 {
		t.Errorf("GetTextByType(BlockParagraph) = %d blocks, want 2", len(paragraphs))
	}

	lists := GetTextByType(blocks, BlockList)
	if len(lists) != 1 {
		t.Errorf("GetTextByType(BlockList) = %d blocks, want 1", len(lists))
	}
}

// TestGetTitles tests filtering titles by level
func TestGetTitles(t *testing.T) {
	blocks := []ClassifiedBlock{
		{Type: BlockTitle, Level: 1, Text: "H1 Title"},
		{Type: BlockTitle, Level: 2, Text: "H2 Title"},
		{Type: BlockTitle, Level: 1, Text: "Another H1"},
		{Type: BlockParagraph, Text: "Not a title"},
	}

	// Get all titles
	allTitles := GetTitles(blocks, 0)
	if len(allTitles) != 3 {
		t.Errorf("GetTitles(0) = %d blocks, want 3", len(allTitles))
	}

	// Get only H1 titles
	h1Titles := GetTitles(blocks, 1)
	if len(h1Titles) != 2 {
		t.Errorf("GetTitles(1) = %d blocks, want 2", len(h1Titles))
	}

	// Get only H2 titles
	h2Titles := GetTitles(blocks, 2)
	if len(h2Titles) != 1 {
		t.Errorf("GetTitles(2) = %d blocks, want 1", len(h2Titles))
	}
}

// TestSmartOrderingWithExtractor tests smart ordering through extractor
func TestSmartOrderingWithExtractor(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test, file not available: %v", err)
		return
	}
	defer f.Close()

	// Extract with smart ordering enabled
	smartText, err := NewExtractor(r).
		SmartOrdering(true).
		Pages(1).
		ExtractText()
	if err != nil {
		t.Fatalf("ExtractText() with smart ordering error: %v", err)
	}

	// Extract with smart ordering disabled
	simpleText, err := NewExtractor(r).
		SmartOrdering(false).
		Pages(1).
		ExtractText()
	if err != nil {
		t.Fatalf("ExtractText() without smart ordering error: %v", err)
	}

	// Both should return text (may or may not be the same depending on layout)
	if smartText == "" {
		t.Error("Smart ordering returned empty text")
	}
	if simpleText == "" {
		t.Error("Simple ordering returned empty text")
	}

	t.Logf("Smart: %d chars, Simple: %d chars", len(smartText), len(simpleText))
}

// TestParseFontStyles tests font style parsing
func TestParseFontStyles(t *testing.T) {
	tests := []struct {
		fontName      string
		wantBold      bool
		wantItalic    bool
		wantUnderline bool
	}{
		{"Arial", false, false, false},
		{"Arial-Bold", true, false, false},
		{"Arial-Black", true, false, false},
		{"Arial-Italic", false, true, false},
		{"Arial-Oblique", false, true, false},
		{"Arial-BoldItalic", true, true, false},
		{"TimesNewRomanPS-BoldMT", true, false, false},
		{"CourierNewPS-ItalicMT", false, true, false},
	}

	for _, tt := range tests {
		gotBold, gotItalic, gotUnderline := parseFontStyles(tt.fontName)
		if gotBold != tt.wantBold || gotItalic != tt.wantItalic || gotUnderline != tt.wantUnderline {
			t.Errorf("parseFontStyles(%q) = (%v, %v, %v), want (%v, %v, %v)",
				tt.fontName, gotBold, gotItalic, gotUnderline, tt.wantBold, tt.wantItalic, tt.wantUnderline)
		}
	}
}

// TestResourceManager tests the resource manager
func TestResourceManager(t *testing.T) {
	rm := NewResourceManager()
	if rm == nil {
		t.Fatal("NewResourceManager() returned nil")
	}

	// Mock closer
	closed := false
	mockCloser := &mockCloser{closed: &closed}

	rm.Add(mockCloser)
	if len(rm.resources) != 1 {
		t.Errorf("Expected 1 resource, got %d", len(rm.resources))
	}

	err := rm.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
	if !closed {
		t.Error("Resource was not closed")
	}
	if len(rm.resources) != 0 {
		t.Error("Resources not cleared after Close()")
	}
}

type mockCloser struct {
	closed *bool
}

func (m *mockCloser) Close() error {
	*m.closed = true
	return nil
}

// TestConnectionPool tests the connection pool
func TestConnectionPool(t *testing.T) {
	created := 0
	closed := 0

	newFunc := func() interface{} {
		created++
		return &mockConnection{id: created}
	}

	closeFunc := func(conn interface{}) {
		closed++
	}

	pool := NewConnectionPool(2, newFunc, closeFunc)
	if pool == nil {
		t.Fatal("NewConnectionPool() returned nil")
	}

	// Get connections
	conn1 := pool.Get()
	conn2 := pool.Get()
	conn3 := pool.Get() // Should create new

	if created != 3 {
		t.Errorf("Expected 3 connections created, got %d", created)
	}

	// Put back
	pool.Put(conn1)
	pool.Put(conn2)
	pool.Put(conn3) // Pool full, should close

	if closed != 1 {
		t.Errorf("Expected 1 connection closed, got %d", closed)
	}

	pool.Close()
	if closed != 3 {
		t.Errorf("Expected all connections closed, got %d", closed)
	}
}

type mockConnection struct {
	id int
}
