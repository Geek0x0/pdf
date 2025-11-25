// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"math"
	"sort"
	"strings"
)

// TextBlock represents a coherent block of text (like a paragraph or column)
type TextBlock struct {
	Texts       []Text
	MinX        float64
	MaxX        float64
	MinY        float64
	MaxY        float64
	AvgFontSize float64
}

// Bounds returns the bounding box of the text block
func (tb *TextBlock) Bounds() Rect {
	return Rect{
		Min: Point{X: tb.MinX, Y: tb.MinY},
		Max: Point{X: tb.MaxX, Y: tb.MaxY},
	}
}

// Center returns the center point of the text block
func (tb *TextBlock) Center() Point {
	return Point{
		X: (tb.MinX + tb.MaxX) / 2,
		Y: (tb.MinY + tb.MaxY) / 2,
	}
}

// Width returns the width of the text block
func (tb *TextBlock) Width() float64 {
	return tb.MaxX - tb.MinX
}

// Height returns the height of the text block
func (tb *TextBlock) Height() float64 {
	return tb.MaxY - tb.MinY
}

// smartTextOrdering implements improved text ordering using clustering
// to handle multi-column layouts and complex reading orders
func smartTextOrdering(texts []Text) []Text {
	if len(texts) == 0 {
		return texts
	}

	// 1. Cluster texts into blocks
	blocks := clusterTextBlocks(texts)

	// 2. Detect reading order (left-to-right columns, etc.)
	orderedBlocks := detectReadingOrder(blocks)

	// 3. Flatten blocks back to texts
	var result []Text
	for _, block := range orderedBlocks {
		// Sort within each block
		sortedTexts := sortWithinBlock(block.Texts)
		result = append(result, sortedTexts...)
	}

	return result
}

// clusterTextBlocks groups nearby texts into coherent blocks
// using a simplified distance-based clustering approach
// P1 optimization: Use KD-Tree to accelerate clustering, optimize from O(n²) to O(n log n)
func clusterTextBlocks(texts []Text) []*TextBlock {
	if len(texts) == 0 {
		return nil
	}

	// For small datasets, use simple method (threshold after tuning: 200)
	if len(texts) < 200 {
		return clusterTextBlocksSimple(texts)
	}

	// For large datasets, use optimized KD-Tree method
	return ClusterTextBlocksOptimized(texts)
}

// clusterTextBlocksSimple Simple clustering method (for small datasets)
func clusterTextBlocksSimple(texts []Text) []*TextBlock {
	if len(texts) == 0 {
		return nil
	}

	// Calculate average font size for distance threshold
	var totalFontSize float64
	for _, t := range texts {
		totalFontSize += t.FontSize
	}
	avgFontSize := totalFontSize / float64(len(texts))

	// Distance threshold: texts within this distance are likely in same block
	distThreshold := avgFontSize * 2.0

	// Start with each text as its own cluster
	clusters := make([]*TextBlock, len(texts))
	for i, t := range texts {
		clusters[i] = &TextBlock{
			Texts:       []Text{t},
			MinX:        t.X,
			MaxX:        t.X + t.W,
			MinY:        t.Y,
			MaxY:        t.Y + t.FontSize,
			AvgFontSize: t.FontSize,
		}
	}

	// Merge nearby clusters iteratively
	merged := true
	for merged {
		merged = false
		for i := 0; i < len(clusters); i++ {
			for j := i + 1; j < len(clusters); j++ {
				if shouldMergeClusters(clusters[i], clusters[j], distThreshold) {
					clusters[i] = mergeClusters(clusters[i], clusters[j])
					clusters = append(clusters[:j], clusters[j+1:]...)
					merged = true
					break
				}
			}
			if merged {
				break
			}
		}
	}

	return clusters
}

// shouldMergeClusters determines if two text blocks should be merged
func shouldMergeClusters(b1, b2 *TextBlock, threshold float64) bool {
	// Enhanced logic for asymmetric layouts and text-image mixing

	// Check vertical proximity (same line or nearby lines)
	verticalOverlap := math.Min(b1.MaxY, b2.MaxY) - math.Max(b1.MinY, b2.MinY)
	if verticalOverlap < 0 {
		verticalOverlap = 0
	}

	// If there's significant vertical overlap, check horizontal distance
	if verticalOverlap > b1.AvgFontSize*0.3 || verticalOverlap > b2.AvgFontSize*0.3 {
		horizontalGap := math.Max(b1.MinX, b2.MinX) - math.Min(b1.MaxX, b2.MaxX)
		if horizontalGap < 0 {
			horizontalGap = 0
		}
		if horizontalGap < threshold {
			return true
		}
	}

	// Check if vertically stacked and horizontally aligned (same column)
	horizontalOverlap := math.Min(b1.MaxX, b2.MaxX) - math.Max(b1.MinX, b2.MinX)
	if horizontalOverlap > 0 {
		overlapRatio := horizontalOverlap / math.Min(b1.Width(), b2.Width())
		if overlapRatio > 0.6 {
			verticalGap := math.Max(b1.MinY, b2.MinY) - math.Min(b1.MaxY, b2.MaxY)
			if verticalGap < 0 {
				verticalGap = 0
			}
			if verticalGap < threshold*1.5 {
				return true
			}
		}
	}

	// For asymmetric layouts: check if blocks are in different regions
	if isAsymmetricLayout(b1, b2) {
		return false // Don't merge across asymmetric boundaries
	}

	// For text-image mixing: avoid merging text that wraps around images
	if isTextImageMix(b1, b2) {
		return false
	}

	return false
}

// mergeClusters combines two text blocks into one
func mergeClusters(b1, b2 *TextBlock) *TextBlock {
	merged := &TextBlock{
		Texts:       append(b1.Texts, b2.Texts...),
		MinX:        math.Min(b1.MinX, b2.MinX),
		MaxX:        math.Max(b1.MaxX, b2.MaxX),
		MinY:        math.Min(b1.MinY, b2.MinY),
		MaxY:        math.Max(b1.MaxY, b2.MaxY),
		AvgFontSize: (b1.AvgFontSize*float64(len(b1.Texts)) + b2.AvgFontSize*float64(len(b2.Texts))) / float64(len(b1.Texts)+len(b2.Texts)),
	}
	return merged
}

// detectReadingOrder determines the reading order of text blocks
// (left-to-right, top-to-bottom, multi-column, etc.)
func detectReadingOrder(blocks []*TextBlock) []*TextBlock {
	if len(blocks) == 0 {
		return blocks
	}

	// For asymmetric layouts, use the enhanced detection
	return detectReadingOrderForAsymmetricLayout(blocks)
}

// detectColumns identifies column structure in text blocks
func detectColumns(blocks []*TextBlock) [][]*TextBlock {
	if len(blocks) == 0 {
		return nil
	}

	// For asymmetric layouts, use a more sophisticated detection algorithm
	return detectAsymmetricColumns(blocks)
}

// detectAsymmetricColumns identifies complex asymmetric column structures
func detectAsymmetricColumns(blocks []*TextBlock) [][]*TextBlock {
	if len(blocks) == 0 {
		return nil
	}

	// Calculate page dimensions to understand the layout context
	pageBounds := calculatePageBounds(blocks)

	// Use a more sophisticated approach to detect asymmetric layouts
	// This includes overlapping, nested, and irregular columns

	// Group blocks by Y-ranges (horizontal bands) to understand the layout structure
	bands := groupBlocksByYRange(blocks, pageBounds)

	// Process each band separately to detect columns within that band
	var allColumns [][]*TextBlock
	for _, band := range bands {
		bandColumns := detectColumnsInBand(band.Blocks, pageBounds)
		allColumns = append(allColumns, bandColumns...)
	}

	return allColumns
}

// YBand represents a horizontal band of text on a page
type YBand struct {
	MinY, MaxY float64
	Blocks     []*TextBlock
}

// groupBlocksByYRange groups text blocks into horizontal bands
func groupBlocksByYRange(blocks []*TextBlock, pageBounds Rect) []YBand {
	if len(blocks) == 0 {
		return nil
	}

	// Sort blocks by Y position
	sortedBlocks := make([]*TextBlock, len(blocks))
	copy(sortedBlocks, blocks)
	sort.Slice(sortedBlocks, func(i, j int) bool {
		return sortedBlocks[i].Center().Y > sortedBlocks[j].Center().Y // Top to bottom
	})

	var bands []YBand
	if len(sortedBlocks) == 0 {
		return bands
	}

	// Start with the first block
	currentBand := YBand{
		MinY:   sortedBlocks[0].MinY,
		MaxY:   sortedBlocks[0].MaxY,
		Blocks: []*TextBlock{sortedBlocks[0]},
	}

	const yTolerance = 10.0 // Tolerance for considering blocks in same band

	for _, block := range sortedBlocks[1:] {
		// Check if this block overlaps vertically with the current band
		overlap := math.Min(currentBand.MaxY, block.MaxY) - math.Max(currentBand.MinY, block.MinY)

		if overlap >= -yTolerance { // Allow slight gaps
			// Extend the band to include this block
			currentBand.MinY = math.Min(currentBand.MinY, block.MinY)
			currentBand.MaxY = math.Max(currentBand.MaxY, block.MaxY)
			currentBand.Blocks = append(currentBand.Blocks, block)
		} else {
			// Start a new band
			bands = append(bands, currentBand)
			currentBand = YBand{
				MinY:   block.MinY,
				MaxY:   block.MaxY,
				Blocks: []*TextBlock{block},
			}
		}
	}

	// Add the last band
	if len(currentBand.Blocks) > 0 {
		bands = append(bands, currentBand)
	}

	return bands
}

// calculatePageBounds calculates the overall bounds of all text blocks
func calculatePageBounds(blocks []*TextBlock) Rect {
	if len(blocks) == 0 {
		return Rect{}
	}

	minX, minY := blocks[0].MinX, blocks[0].MinY
	maxX, maxY := blocks[0].MaxX, blocks[0].MaxY

	for _, block := range blocks[1:] {
		minX = math.Min(minX, block.MinX)
		minY = math.Min(minY, block.MinY)
		maxX = math.Max(maxX, block.MaxX)
		maxY = math.Max(maxY, block.MaxY)
	}

	return Rect{
		Min: Point{X: minX, Y: minY},
		Max: Point{X: maxX, Y: maxY},
	}
}

// detectColumnsInBand detects columns within a horizontal band
func detectColumnsInBand(blocks []*TextBlock, pageBounds Rect) [][]*TextBlock {
	if len(blocks) == 0 {
		return [][]*TextBlock{}
	}

	// For each band, detect potential columns based on X positions and gaps
	// Sort blocks by X position
	sorted := make([]*TextBlock, len(blocks))
	copy(sorted, blocks)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].MinX < sorted[j].MinX
	})

	var columns [][]*TextBlock

	// Use a more sophisticated algorithm that considers:
	// 1. Gaps between text blocks
	// 2. Overlapping regions
	// 3. Asymmetric layouts

	// Calculate gap threshold based on page width
	pageWidth := pageBounds.Max.X - pageBounds.Min.X
	gapThreshold := pageWidth * 0.05 // 5% of page width as minimum gap for separate columns

	// Group blocks based on proximity and gaps
	currentColumn := []*TextBlock{sorted[0]}

	for _, block := range sorted[1:] {
		// Calculate gap to previous block in current column
		lastBlock := currentColumn[len(currentColumn)-1]
		gap := block.MinX - lastBlock.MaxX

		// Check if there's a significant gap or if blocks overlap significantly
		if gap > gapThreshold {
			// Significant gap - start new column
			columns = append(columns, currentColumn)
			currentColumn = []*TextBlock{block}
		} else if hasSignificantVerticalOverlap(lastBlock, block) {
			// Blocks overlap vertically - likely same column
			currentColumn = append(currentColumn, block)
		} else {
			// Check if block belongs to an existing column based on alignment
			placed := false
			for i, col := range columns {
				if canAddToColumn(col, block, gapThreshold) {
					columns[i] = append(col, block)
					placed = true
					break
				}
			}

			if !placed {
				// Add to current column
				currentColumn = append(currentColumn, block)
			}
		}
	}

	// Add the last column
	if len(currentColumn) > 0 {
		columns = append(columns, currentColumn)
	}

	return columns
}

// hasSignificantVerticalOverlap checks if two blocks have significant vertical overlap
func hasSignificantVerticalOverlap(b1, b2 *TextBlock) bool {
	verticalOverlap := math.Min(b1.MaxY, b2.MaxY) - math.Max(b1.MinY, b2.MinY)
	if verticalOverlap <= 0 {
		return false
	}

	// Consider significant if overlap covers at least 20% of the smaller block's height.
	height1 := b1.MaxY - b1.MinY
	height2 := b2.MaxY - b2.MinY
	minHeight := math.Min(height1, height2)
	if minHeight <= 0 {
		return false
	}

	return verticalOverlap >= minHeight*0.2
}

// canAddToColumn checks if a block can be added to an existing column
func canAddToColumn(column []*TextBlock, newBlock *TextBlock, gapThreshold float64) bool {
	if len(column) == 0 {
		return true
	}

	// Check if the new block aligns with the column
	avgLeft := 0.0
	avgRight := 0.0
	for _, block := range column {
		avgLeft += block.MinX
		avgRight += block.MaxX
	}
	avgLeft /= float64(len(column))
	avgRight /= float64(len(column))

	leftDiff := math.Abs(newBlock.MinX - avgLeft)
	rightDiff := math.Abs(newBlock.MaxX - avgRight)

	// Block can join the column if it aligns reasonably well
	return leftDiff < gapThreshold && rightDiff < gapThreshold
}

// detectReadingOrderForAsymmetricLayout determines the reading order for asymmetric layouts
func detectReadingOrderForAsymmetricLayout(blocks []*TextBlock) []*TextBlock {
	if len(blocks) == 0 {
		return blocks
	}

	// Group into horizontal bands first
	bands := groupBlocksByYRange(blocks, calculatePageBounds(blocks))

	// Process each band: sort blocks within the band, then order bands
	var result []*TextBlock

	// Sort bands from top to bottom (Y coordinates are typically larger at the top in PDFs)
	sort.Slice(bands, func(i, j int) bool {
		return bands[i].MaxY > bands[j].MaxY // Higher Y values are at the top
	})

	for _, band := range bands {
		// Sort blocks within each band based on reading order
		sortedBandBlocks := sortBlocksInBand(band.Blocks)
		result = append(result, sortedBandBlocks...)
	}

	return result
}

// sortBlocksInBand sorts blocks within a horizontal band
func sortBlocksInBand(blocks []*TextBlock) []*TextBlock {
	if len(blocks) <= 1 {
		return blocks
	}

	// For asymmetric layouts, use a flow-based approach
	// Consider text flow, not just column positions

	// First, identify the main flow direction in this band
	// This could be left-to-right columns, but also more complex flows

	// Simple approach: sort by X position (left to right)
	sorted := make([]*TextBlock, len(blocks))
	copy(sorted, blocks)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].MinX < sorted[j].MinX
	})

	return sorted
}

// sortWithinBlock sorts texts within a single block in reading order
func sortWithinBlock(texts []Text) []Text {
	if len(texts) == 0 {
		return texts
	}

	// Group into lines
	type line struct {
		y     float64
		texts []Text
	}

	const lineTolerance = 3.0
	// Pre-allocate lines capacity, estimate number of lines as 1/10 of text count
	lines := make([]line, 0, len(texts)/10+1)

	// Sort directly on original slice, avoid copying
	// Sort by Y first (top to bottom)
	// Optimization: Use cached Y value comparison to reduce floating point operations
	sort.Slice(texts, func(i, j int) bool {
		deltaY := texts[i].Y - texts[j].Y
		if deltaY > lineTolerance || deltaY < -lineTolerance {
			return texts[i].Y > texts[j].Y
		}
		return texts[i].X < texts[j].X
	})

	// Group into lines
	for _, t := range texts {
		placed := false
		for i := range lines {
			if math.Abs(t.Y-lines[i].y) <= lineTolerance {
				lines[i].texts = append(lines[i].texts, t)
				placed = true
				break
			}
		}
		if !placed {
			lines = append(lines, line{y: t.Y, texts: []Text{t}})
		}
	}

	// Sort each line left-to-right and flatten
	// Pre-allocate result, reuse texts slice to avoid new allocation
	result := texts[:0]
	for _, l := range lines {
		sort.Slice(l.texts, func(i, j int) bool {
			return l.texts[i].X < l.texts[j].X
		})
		result = append(result, l.texts...)
	}

	return result
}

// SmartTextRunsToPlain converts text runs to plain text using improved ordering
func SmartTextRunsToPlain(texts []Text) string {
	if len(texts) == 0 {
		return ""
	}

	// Use smart ordering algorithm
	ordered := smartTextOrdering(texts)

	// Group into lines for formatting
	const lineTolerance = 3.0
	// Pre-allocate lines capacity, estimate number of lines as 1/10 of text count
	lines := make([][]Text, 0, len(ordered)/10+1)
	// Pre-allocate currentLine capacity
	currentLine := make([]Text, 0, 10)
	var currentY float64

	for i, t := range ordered {
		if i == 0 {
			currentLine = append(currentLine, t)
			currentY = t.Y
			continue
		}

		if math.Abs(t.Y-currentY) <= lineTolerance {
			currentLine = append(currentLine, t)
		} else {
			if len(currentLine) > 0 {
				lines = append(lines, currentLine)
			}
			currentLine = make([]Text, 0, 10)
			currentLine = append(currentLine, t)
			currentY = t.Y
		}
	}
	if len(currentLine) > 0 {
		lines = append(lines, currentLine)
	}

	// Build output with proper spacing
	// Improved capacity estimation: count actual text length instead of fixed estimation
	estimatedLen := 0
	for _, line := range lines {
		for _, t := range line {
			estimatedLen += len(t.S) + 1 // text length + space
		}
		estimatedLen += 1 // newline
	}
	var builder strings.Builder
	builder.Grow(estimatedLen)

	for i, line := range lines {
		appendLine(&builder, line)
		if i < len(lines)-1 {
			builder.WriteByte('\n')
		}
	}

	return strings.TrimRight(builder.String(), "\n")
}

// isAsymmetricLayout checks if two blocks are in different asymmetric layout regions
func isAsymmetricLayout(b1, b2 *TextBlock) bool {
	// Simple heuristic: if blocks are far apart horizontally and vertically,
	// they might be in different layout regions
	horizontalDistance := math.Abs(b1.Center().X - b2.Center().X)
	verticalDistance := math.Abs(b1.Center().Y - b2.Center().Y)

	// If horizontal distance is much larger than vertical, likely different columns
	if horizontalDistance > verticalDistance*2 {
		return true
	}

	return false
}

// isTextImageMix checks if text blocks are separated by potential image regions
func isTextImageMix(b1, b2 *TextBlock) bool {
	// Heuristic: if there's a large gap between blocks, might be image
	gapX := math.Abs(b1.Center().X - b2.Center().X)
	gapY := math.Abs(b1.Center().Y - b2.Center().Y)

	// If gap is larger than average block size, might be image
	avgSize := (b1.Width() + b1.Height() + b2.Width() + b2.Height()) / 4
	if gapX > avgSize*2 || gapY > avgSize*2 {
		return true
	}

	return false
}

// detectFootnotes identifies potential footnote blocks
func detectFootnotes(blocks []*TextBlock, pageHeight float64) []*TextBlock {
	var footnotes []*TextBlock

	for _, block := range blocks {
		// Footnotes are typically at the bottom of the page, small font
		if block.Center().Y > pageHeight*0.8 && block.AvgFontSize < 10 {
			footnotes = append(footnotes, block)
		}
	}

	return footnotes
}
