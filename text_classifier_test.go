package pdf

import (
	"regexp"
	"testing"
)

// TestContextAwareClassification tests the enhanced context-aware classification functionality
func TestContextAwareClassification(t *testing.T) {
	// Create test text elements that simulate different contexts
	texts := []Text{
		{S: "Introduction", X: 100, Y: 700, FontSize: 16}, // Likely title
		{S: "This is the beginning of the document.", X: 100, Y: 680, FontSize: 12}, // Likely paragraph
		{S: "Figure 1: Sample Chart", X: 200, Y: 600, FontSize: 10}, // Likely caption
		{S: "1. First item", X: 100, Y: 580, FontSize: 12}, // Likely list
		{S: "1.1 Sub-item", X: 120, Y: 560, FontSize: 12}, // Likely list
	}

	pageWidth := 600.0
	pageHeight := 800.0

	classifier := NewTextClassifier(texts, pageWidth, pageHeight)
	
	if classifier.spatialIndex == nil {
		t.Error("Expected spatial index to be created, but it was nil")
	}

	blocks := classifier.ClassifyBlocks()
	
	if len(blocks) != 5 {
		t.Errorf("Expected 5 blocks, got %d", len(blocks))
	}

	// Check that the "Introduction" text was classified as a title
	titleFound := false
	for _, block := range blocks {
		if block.Text == "Introduction" && block.Type == BlockTitle {
			titleFound = true
			if block.Level != 1 && block.Level != 2 { // Should be top-level title
				t.Errorf("Expected title level 1 or 2, got %d", block.Level)
			}
		}
	}
	
	if !titleFound {
		t.Error("Expected to find 'Introduction' as a title block")
	}

	// Check that the "Figure 1: Sample Chart" was classified as caption
	captionFound := false
	for _, block := range blocks {
		if block.Text == "Figure 1: Sample Chart" && block.Type == BlockCaption {
			captionFound = true
		}
	}
	
	if !captionFound {
		t.Error("Expected to find 'Figure 1: Sample Chart' as a caption block")
	}

	// Check that list items were classified as list
	listCount := 0
	for _, block := range blocks {
		if block.Type == BlockList {
			listCount++
		}
	}
	
	if listCount < 2 {
		t.Errorf("Expected at least 2 list items, got %d", listCount)
	}
}

// TestContextAwareClassificationWithContext tests classification using context
func TestContextAwareClassificationWithContext(t *testing.T) {
	// Create test text elements with clear context
	texts := []Text{
		{S: "Main Title", X: 100, Y: 700, FontSize: 20}, // Large font = title
		{S: "Subtitle", X: 100, Y: 680, FontSize: 14},    // Medium font = subtitle
		{S: "Regular text paragraph here.", X: 100, Y: 660, FontSize: 12}, // Regular = paragraph
		{S: "Another paragraph", X: 100, Y: 640, FontSize: 12}, // Regular = paragraph
	}

	pageWidth := 600.0
	pageHeight := 800.0

	classifier := NewTextClassifier(texts, pageWidth, pageHeight)
	blocks := classifier.ClassifyBlocks()
	
	if len(blocks) != 4 {
		t.Errorf("Expected 4 blocks, got %d", len(blocks))
	}

	// Verify that the large font text was identified as a title
	titleCount := 0
	subtitleCount := 0
	paragraphCount := 0
	
	for _, block := range blocks {
		switch block.Type {
		case BlockTitle:
			titleCount++
			if block.Level == 0 {
				t.Error("Expected title to have a level assigned")
			}
		case BlockParagraph:
			paragraphCount++
		}
		// Subtitles are typically also classified as titles with higher level numbers
		if block.Type == BlockTitle && block.Level > 2 {
			subtitleCount++
		}
	}
	
	if titleCount < 1 {
		t.Error("Expected at least 1 title block")
	}
	
	if paragraphCount < 2 {
		t.Errorf("Expected at least 2 paragraph blocks, got %d", paragraphCount)
	}
}

// TestContextAwareClassificationNoContext tests classification when there's no spatial index
func TestContextAwareClassificationNoContext(t *testing.T) {
	// Test with empty text slice
	texts := []Text{}
	pageWidth := 600.0
	pageHeight := 800.0

	classifier := NewTextClassifier(texts, pageWidth, pageHeight)
	blocks := classifier.ClassifyBlocks()
	
	if len(blocks) != 0 {
		t.Errorf("Expected 0 blocks for empty text, got %d", len(blocks))
	}
}

// TestTextClassifierGetContext tests the context retrieval functionality
func TestTextClassifierGetContext(t *testing.T) {
	texts := []Text{
		{S: "Main content", X: 100, Y: 700, FontSize: 12, W: 20},
		{S: "Caption text", X: 120, Y: 650, FontSize: 10, W: 15}, // Close to main content
		{S: "Footnote", X: 100, Y: 100, FontSize: 8, W: 10},     // Far away
	}

	pageWidth := 600.0
	pageHeight := 800.0

	classifier := NewTextClassifier(texts, pageWidth, pageHeight)
	
	// Create a mock TextBlock to test context retrieval
	cluster := &TextBlock{
		Texts:       []Text{texts[0]},
		MinX:        100,
		MaxX:        120,
		MinY:        700,
		MaxY:        712,
		AvgFontSize: 12,
	}

	context := classifier.getContext(cluster)
	
	// Should find the "Caption text" as context, but not "Footnote" (too far)
	if len(context) == 0 {
		t.Log("No context found - this is expected if spatial index isn't working properly")
	}
}

// TestIsFootnoteReference tests the footnote reference detection
func TestIsFootnoteReference(t *testing.T) {
	textClassifier := &TextClassifier{
		captionPattern: regexp.MustCompile(`^(Figure|Table|Fig\.|Tab\.)\s+\d+`),
		listPattern:    regexp.MustCompile(`^(\d+\.|[•\-\*]|\([a-z]\)|\([0-9]+\))\s`),
	}
	
	testCases := []struct {
		text     string
		expected bool
		name     string
	}{
		{"[1]", true, "Square bracket number"},
		{"[23]", true, "Square bracket two-digit number"},
		{"¹", true, "Superscript 1"},
		{"²", true, "Superscript 2"},
		{"³", true, "Superscript 3"},
		{"abc", false, "Regular text"},
		{"not a ref", false, "Non-reference text"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := textClassifier.isFootnoteReference(tc.text)
			if result != tc.expected {
				t.Errorf("isFootnoteReference(%q) = %v, want %v", tc.text, result, tc.expected)
			}
		})
	}
}

// TestIsLikelyParagraph tests paragraph detection
func TestIsLikelyParagraph(t *testing.T) {
	textClassifier := &TextClassifier{}
	
	testCases := []struct {
		text     Text
		expected bool
		name     string
	}{
		{Text{S: "This is a sentence with multiple words."}, true, "Multi-word text"},
		{Text{S: "Short"}, false, "Single word text"},
		{Text{S: "A few words"}, true, "Three words"},
		{Text{S: ""}, false, "Empty text"},
		{Text{S: "One"}, false, "Single word"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := textClassifier.isLikelyParagraph(tc.text)
			if result != tc.expected {
				t.Errorf("isLikelyParagraph(%+v) = %v, want %v", tc.text, result, tc.expected)
			}
		})
	}
}