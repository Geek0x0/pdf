// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"math"
	"regexp"
	"strings"
	"unicode"
)

// BlockType represents the semantic type of a text block
type BlockType int

const (
	BlockUnknown   BlockType = iota
	BlockTitle               // Title or heading
	BlockParagraph           // Regular paragraph
	BlockList                // List item (numbered or bulleted)
	BlockCaption             // Image or table caption
	BlockFootnote            // Footnote or endnote
	BlockHeader              // Page header
	BlockFooter              // Page footer
)

// String returns the string representation of BlockType
func (bt BlockType) String() string {
	switch bt {
	case BlockTitle:
		return "Title"
	case BlockParagraph:
		return "Paragraph"
	case BlockList:
		return "List"
	case BlockCaption:
		return "Caption"
	case BlockFootnote:
		return "Footnote"
	case BlockHeader:
		return "Header"
	case BlockFooter:
		return "Footer"
	default:
		return "Unknown"
	}
}

// ClassifiedBlock represents a classified block of text with semantic information
type ClassifiedBlock struct {
	Type    BlockType // Semantic type of the block
	Level   int       // Hierarchy level (for titles: 1=h1, 2=h2, etc.)
	Content []Text    // Text runs in this block
	Bounds  Rect      // Bounding box
	Text    string    // Concatenated text content
}

// TextClassifier classifies text runs into semantic blocks
type TextClassifier struct {
	texts          []Text
	avgFontSize    float64
	maxFontSize    float64
	minFontSize    float64
	pageHeight     float64
	pageWidth      float64
	listPattern    *regexp.Regexp
	captionPattern *regexp.Regexp
	spatialIndex   SpatialIndexInterface // Spatial index for context-aware classification
}

// NewTextClassifier creates a new text classifier
func NewTextClassifier(texts []Text, pageWidth, pageHeight float64) *TextClassifier {
	if len(texts) == 0 {
		return &TextClassifier{
			texts:          texts,
			pageWidth:      pageWidth,
			pageHeight:     pageHeight,
			listPattern:    regexp.MustCompile(`^(\d+\.|[•\-\*]|\([a-z]\)|\([0-9]+\))\s`),
			captionPattern: regexp.MustCompile(`^(Figure|Table|Fig\.|Tab\.)\s+\d+`),
		}
	}

	tc := &TextClassifier{
		texts:          texts,
		pageWidth:      pageWidth,
		pageHeight:     pageHeight,
		listPattern:    regexp.MustCompile(`^(\d+\.|[•\-\*]|\([a-z]\)|\([0-9]+\))\s`),
		captionPattern: regexp.MustCompile(`^(Figure|Table|Fig\.|Tab\.)\s+\d+`),
	}

	// Calculate font statistics
	var totalSize float64
	tc.maxFontSize = 0
	tc.minFontSize = math.MaxFloat64

	for _, t := range texts {
		totalSize += t.FontSize
		if t.FontSize > tc.maxFontSize {
			tc.maxFontSize = t.FontSize
		}
		if t.FontSize < tc.minFontSize {
			tc.minFontSize = t.FontSize
		}
	}

	tc.avgFontSize = totalSize / float64(len(texts))

	// Create spatial index for context-aware classification
	tc.spatialIndex = NewSpatialIndexInterface(texts)

	return tc
}

// ClassifyBlocks classifies text runs into semantic blocks
func (tc *TextClassifier) ClassifyBlocks() []ClassifiedBlock {
	if len(tc.texts) == 0 {
		return nil
	}

	// First, cluster texts into logical blocks using smart ordering
	clusters := clusterTextBlocks(tc.texts)

	// Then classify each cluster
	var blocks []ClassifiedBlock
	for _, cluster := range clusters {
		block := tc.classifyCluster(cluster)
		blocks = append(blocks, block)
	}

	return blocks
}

// classifyCluster determines the type of a text cluster
func (tc *TextClassifier) classifyCluster(cluster *TextBlock) ClassifiedBlock {
	// Concatenate text content
	var textContent strings.Builder
	for _, t := range cluster.Texts {
		textContent.WriteString(t.S)
	}
	text := strings.TrimSpace(textContent.String())

	center := cluster.Center()
	block := ClassifiedBlock{
		Content: cluster.Texts,
		Bounds:  cluster.Bounds(),
		Text:    text,
		Type:    BlockUnknown,
		Level:   0,
	}

	if text == "" {
		return block
	}

	// Get context from nearby text
	context := tc.getContext(cluster)

	// Check for header/footer based on position and context
	if tc.isHeaderPosition(center.Y) {
		block.Type = BlockHeader
		return block
	}
	if tc.isFooterPosition(center.Y) {
		block.Type = BlockFooter
		return block
	}

	// Check for caption with context awareness
	if tc.captionPattern.MatchString(text) || tc.isCaptionWithContext(text, context) {
		block.Type = BlockCaption
		return block
	}

	// Check for list item with context awareness
	if tc.listPattern.MatchString(text) || tc.isListWithContext(text, context) {
		block.Type = BlockList
		return block
	}

	// Check for footnote with context awareness (small font at bottom)
	if tc.isFootnote(cluster, center.Y) || tc.isFootnoteWithContext(cluster, center.Y, context) {
		block.Type = BlockFootnote
		return block
	}

	// Check for title with context awareness
	if tc.isTitle(cluster) || tc.isTitleWithContext(cluster, context) {
		block.Type = BlockTitle
		block.Level = tc.getTitleLevel(cluster.AvgFontSize)
		return block
	}

	// Use context to determine paragraph vs other types
	if tc.isParagraphWithContext(context) {
		block.Type = BlockParagraph
		return block
	}

	// Default to paragraph
	block.Type = BlockParagraph
	return block
}

// getContext returns text context from nearby elements
func (tc *TextClassifier) getContext(cluster *TextBlock) []Text {
	if tc.spatialIndex == nil {
		return nil
	}

	// Get nearby text elements within a reasonable distance
	bounds := cluster.Bounds()
	margin := cluster.AvgFontSize * 3.0 // Consider elements within 3x font size distance
	nearbyBounds := Rect{
		Min: Point{X: bounds.Min.X - margin, Y: bounds.Min.Y - margin},
		Max: Point{X: bounds.Max.X + margin, Y: bounds.Max.Y + margin},
	}

	return tc.spatialIndex.Query(nearbyBounds)
}

// isCaptionWithContext checks if the text is a caption based on nearby context
func (tc *TextClassifier) isCaptionWithContext(text string, context []Text) bool {
	if len(context) == 0 {
		return false
	}

	// Check if there's an image or table nearby
	for _, nearbyText := range context {
		if tc.isNearImageOrTable(nearbyText) {
			return true
		}
	}
	return false
}

// isListWithContext checks if the text is a list item with contextual clues
func (tc *TextClassifier) isListWithContext(text string, context []Text) bool {
	for _, nearbyText := range context {
		if tc.listPattern.MatchString(nearbyText.S) {
			return true
		}
	}
	return false
}

// isFootnoteWithContext checks if the text is a footnote based on context
func (tc *TextClassifier) isFootnoteWithContext(cluster *TextBlock, centerY float64, context []Text) bool {
	// Check if there are footnote references nearby in the main content
	for _, nearbyText := range context {
		// Look for footnote reference patterns like [1], ¹, etc.
		if tc.isFootnoteReference(nearbyText.S) {
			return true
		}
	}
	return cluster.AvgFontSize < tc.avgFontSize*0.8 && centerY < tc.pageHeight*0.3
}

// isTitleWithContext checks if the text is a title based on context
func (tc *TextClassifier) isTitleWithContext(cluster *TextBlock, context []Text) bool {
	// Check font styles: bold or larger size often indicates titles
	hasBold := false
	for _, t := range cluster.Texts {
		if t.Bold {
			hasBold = true
			break
		}
	}

	// Check if significantly larger than surrounding text
	avgContextSize := tc.getAverageFontSize(context)
	if cluster.AvgFontSize > avgContextSize*1.2 {
		return true
	}

	// Check if following text is more likely to be paragraph content
	for _, nearbyText := range context {
		center := Point{X: (nearbyText.X + nearbyText.X + nearbyText.W) / 2, Y: nearbyText.Y}

		// If nearby text is below this cluster and likely a paragraph, this might be a title
		if center.Y < cluster.Center().Y && tc.isLikelyParagraph(nearbyText) {
			if cluster.AvgFontSize > tc.avgFontSize || hasBold { // Larger font or bold indicates possible title
				return true
			}
		}
	}
	return false
}

// isParagraphWithContext uses context to determine if this should be classified as a paragraph
func (tc *TextClassifier) isParagraphWithContext(context []Text) bool {
	// If surrounded by other paragraph content, likely a paragraph
	paragraphCount := 0
	for _, nearbyText := range context {
		if tc.isLikelyParagraph(nearbyText) {
			paragraphCount++
		}
	}
	return paragraphCount > len(context)/2
}

// isNearImageOrTable checks if text is near an image or table region
func (tc *TextClassifier) isNearImageOrTable(text Text) bool {
	// This would be enhanced with actual image/table detection
	// For now, using heuristics based on common patterns
	textLower := strings.ToLower(text.S)
	return strings.Contains(textLower, "figure") || strings.Contains(textLower, "table") || strings.Contains(textLower, "fig.")
}

// isFootnoteReference checks if text looks like a footnote reference
func (tc *TextClassifier) isFootnoteReference(text string) bool {
	// Check for patterns like [1], ¹, or superscript numbers
	return regexp.MustCompile(`\[\d+\]|[\x{00B9}\x{00B2}\x{00B3}\x{2070}-\x{2079}\x{2080}-\x{2089}]`).MatchString(text)
}

// isLikelyParagraph checks if text is likely a paragraph based on content
func (tc *TextClassifier) isLikelyParagraph(text Text) bool {
	words := strings.Fields(text.S)
	// A paragraph typically has more than 3 words
	return len(words) >= 3
}

// isHeaderPosition checks if Y coordinate is in header region
func (tc *TextClassifier) isHeaderPosition(y float64) bool {
	if tc.pageHeight == 0 {
		return false
	}
	// Top 10% of page
	return y > tc.pageHeight*0.9
}

// isFooterPosition checks if Y coordinate is in footer region
func (tc *TextClassifier) isFooterPosition(y float64) bool {
	if tc.pageHeight == 0 {
		return false
	}
	// Bottom 10% of page
	return y < tc.pageHeight*0.1
}

// isFootnote checks if cluster is a footnote
func (tc *TextClassifier) isFootnote(cluster *TextBlock, centerY float64) bool {
	// Small font size and in lower part of page
	return cluster.AvgFontSize < tc.avgFontSize*0.8 &&
		centerY < tc.pageHeight*0.3
}

// isTitle checks if cluster is likely a title
func (tc *TextClassifier) isTitle(cluster *TextBlock) bool {
	// Check font styles: bold text is often a title
	hasBold := false
	for _, t := range cluster.Texts {
		if t.Bold {
			hasBold = true
			break
		}
	}

	// Significantly larger than average font or bold
	if cluster.AvgFontSize <= tc.avgFontSize*1.2 && !hasBold {
		return false
	}

	// Check if text looks like a title (short, capitalized)
	text := strings.TrimSpace(cluster.Texts[0].S)
	if len(text) > 100 {
		return false // Too long to be a title
	}

	// Check for title-like patterns
	words := strings.Fields(text)
	if len(words) == 0 {
		return false
	}

	// Count capitalized words
	capitalizedCount := 0
	for _, word := range words {
		if len(word) > 0 && unicode.IsUpper(rune(word[0])) {
			capitalizedCount++
		}
	}

	// Most words should be capitalized for a title, or it's bold
	return float64(capitalizedCount)/float64(len(words)) > 0.6 || hasBold
}

// getTitleLevel determines the hierarchy level based on font size
func (tc *TextClassifier) getTitleLevel(fontSize float64) int {
	// Divide font size range into levels
	if tc.maxFontSize == tc.minFontSize {
		return 1
	}

	ratio := (fontSize - tc.avgFontSize) / (tc.maxFontSize - tc.avgFontSize)

	if ratio > 0.8 {
		return 1 // h1
	} else if ratio > 0.6 {
		return 2 // h2
	} else if ratio > 0.4 {
		return 3 // h3
	} else if ratio > 0.2 {
		return 4 // h4
	} else {
		return 5 // h5
	}
}

// ClassifyTextBlocks is a convenience function that creates a classifier and runs classification
func (p Page) ClassifyTextBlocks() ([]ClassifiedBlock, error) {
	content := p.Content()
	if len(content.Text) == 0 {
		return nil, nil
	}

	// Get page dimensions
	mediaBox := p.V.Key("MediaBox")
	var pageWidth, pageHeight float64
	if mediaBox.Kind() == Array && mediaBox.Len() >= 4 {
		pageWidth = mediaBox.Index(2).Float64()
		pageHeight = mediaBox.Index(3).Float64()
	} else {
		// Default to letter size if no MediaBox
		pageWidth = 612.0  // 8.5 inches * 72 dpi
		pageHeight = 792.0 // 11 inches * 72 dpi
	}

	classifier := NewTextClassifier(content.Text, pageWidth, pageHeight)
	return classifier.ClassifyBlocks(), nil
}

// GetTextByType returns all text blocks of a specific type
func GetTextByType(blocks []ClassifiedBlock, blockType BlockType) []ClassifiedBlock {
	var result []ClassifiedBlock
	for _, block := range blocks {
		if block.Type == blockType {
			result = append(result, block)
		}
	}
	return result
}

// GetTitles returns all title blocks, optionally filtered by level
func GetTitles(blocks []ClassifiedBlock, level int) []ClassifiedBlock {
	var result []ClassifiedBlock
	for _, block := range blocks {
		if block.Type == BlockTitle {
			if level == 0 || block.Level == level {
				result = append(result, block)
			}
		}
	}
	return result
}

// getAverageFontSize calculates average font size of a list of texts
func (tc *TextClassifier) getAverageFontSize(texts []Text) float64 {
	if len(texts) == 0 {
		return tc.avgFontSize
	}
	var total float64
	for _, t := range texts {
		total += t.FontSize
	}
	return total / float64(len(texts))
}

// isNearTitleIndicator checks if nearby text suggests this is a title
func (tc *TextClassifier) isNearTitleIndicator(text Text) bool {
	// Simple heuristic: short text above or below
	return len(text.S) < 50 && (text.Y > tc.pageHeight*0.8 || text.Y < tc.pageHeight*0.2)
}
