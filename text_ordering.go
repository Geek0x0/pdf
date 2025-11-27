// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"math"
	"sort"
)

// TextBlock represents a coherent block of text (like a paragraph or column)
type TextBlock struct {
	Texts       []Text
	MinX        float64
	MaxX        float64
	MinY        float64
	MaxY        float64
	AvgFontSize float64
	clusterIdx  int // Internal index for clustering, avoids map lookups
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
	result := make([]Text, 0, len(texts))
	for _, block := range orderedBlocks {
		// Sort within each block
		sortedTexts := sortWithinBlockOptimized(block.Texts)
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

	// For small datasets, use simple method (reduced threshold to use V3 more)
	if len(texts) < 50 {
		return clusterTextBlocksSimple(texts)
	}

	// For large datasets, use optimized spatial grid algorithm (V3)
	return ClusterTextBlocksV3(texts)
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
// Optimized: inline min/max, cache width calculations, early returns
func shouldMergeClusters(b1, b2 *TextBlock, threshold float64) bool {
	// Inline min/max for performance (avoid function call overhead)
	var minMaxY, maxMinY float64
	if b1.MaxY < b2.MaxY {
		minMaxY = b1.MaxY
	} else {
		minMaxY = b2.MaxY
	}
	if b1.MinY > b2.MinY {
		maxMinY = b1.MinY
	} else {
		maxMinY = b2.MinY
	}
	verticalOverlap := minMaxY - maxMinY

	// Early return for vertically overlapping blocks
	if verticalOverlap > 0 && (verticalOverlap > b1.AvgFontSize*0.3 || verticalOverlap > b2.AvgFontSize*0.3) {
		var maxMinX, minMaxX float64
		if b1.MinX > b2.MinX {
			maxMinX = b1.MinX
		} else {
			maxMinX = b2.MinX
		}
		if b1.MaxX < b2.MaxX {
			minMaxX = b1.MaxX
		} else {
			minMaxX = b2.MaxX
		}
		horizontalGap := maxMinX - minMaxX
		if horizontalGap < threshold {
			return true
		}
	}

	// Cache width calculations
	w1 := b1.MaxX - b1.MinX
	w2 := b2.MaxX - b2.MinX

	// Check if vertically stacked and horizontally aligned
	var minMaxX, maxMinX float64
	if b1.MaxX < b2.MaxX {
		minMaxX = b1.MaxX
	} else {
		minMaxX = b2.MaxX
	}
	if b1.MinX > b2.MinX {
		maxMinX = b1.MinX
	} else {
		maxMinX = b2.MinX
	}
	horizontalOverlap := minMaxX - maxMinX

	if horizontalOverlap > 0 {
		minWidth := w1
		if w2 < minWidth {
			minWidth = w2
		}
		if minWidth <= 0 {
			return false
		}
		overlapRatio := horizontalOverlap / minWidth
		if overlapRatio > 0.6 {
			var maxMinY2, minMaxY2 float64
			if b1.MinY > b2.MinY {
				maxMinY2 = b1.MinY
			} else {
				maxMinY2 = b2.MinY
			}
			if b1.MaxY < b2.MaxY {
				minMaxY2 = b1.MaxY
			} else {
				minMaxY2 = b2.MaxY
			}
			verticalGap := maxMinY2 - minMaxY2
			if verticalGap >= 0 && verticalGap < threshold*1.5 {
				return true
			}
		}
	}

	// Inline asymmetric layout check (avoid function call)
	c1x := (b1.MinX + b1.MaxX) * 0.5
	c1y := (b1.MinY + b1.MaxY) * 0.5
	c2x := (b2.MinX + b2.MaxX) * 0.5
	c2y := (b2.MinY + b2.MaxY) * 0.5

	horizontalDistance := c1x - c2x
	if horizontalDistance < 0 {
		horizontalDistance = -horizontalDistance
	}
	verticalDistance := c1y - c2y
	if verticalDistance < 0 {
		verticalDistance = -verticalDistance
	}

	// Different columns check
	if horizontalDistance > verticalDistance*2 {
		return false
	}

	// Text-image mix check (inline)
	avgSize := (w1 + (b1.MaxY - b1.MinY) + w2 + (b2.MaxY - b2.MinY)) * 0.25
	if horizontalDistance > avgSize*2 || verticalDistance > avgSize*2 {
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

	// Optimized: Build output directly without intermediate line grouping
	return buildPlainTextOptimized(ordered)
}

// buildPlainTextOptimized builds plain text directly from ordered texts
// avoiding intermediate slice allocations for line grouping
func buildPlainTextOptimized(texts []Text) string {
	if len(texts) == 0 {
		return ""
	}

	// Use pooled string builder for better memory reuse
	builder := GetSizedStringBuilder(0)
	defer PutSizedStringBuilder(builder, 0)

	const lineTolerance = 3.0
	var prevY float64
	var prevX float64
	var prevW float64

	for i, t := range texts {
		if i > 0 {
			// Inline abs calculation
			dy := t.Y - prevY
			if dy < 0 {
				dy = -dy
			}
			if dy > lineTolerance {
				// New line
				builder.WriteByte('\n')
			} else {
				// Same line - check if space needed
				gap := t.X - (prevX + prevW)
				if gap > t.FontSize*0.3 {
					builder.WriteByte(' ')
				}
			}
		}
		builder.WriteString(t.S)
		prevY = t.Y
		prevX = t.X
		prevW = t.W
	}

	return builder.String()
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

// sortWithinBlockOptimized is an optimized version that reduces allocations
func sortWithinBlockOptimized(texts []Text) []Text {
	if len(texts) == 0 {
		return texts
	}

	const lineTolerance = 3.0

	// Sort by Y first (top to bottom), then X (left to right)
	sort.Slice(texts, func(i, j int) bool {
		deltaY := texts[i].Y - texts[j].Y
		if deltaY > lineTolerance || deltaY < -lineTolerance {
			return texts[i].Y > texts[j].Y
		}
		return texts[i].X < texts[j].X
	})

	// Process in-place without creating line structures
	// Find line boundaries and sort within each line
	n := len(texts)
	lineStart := 0

	for i := 1; i <= n; i++ {
		// Check if this is end of a line (or end of texts)
		isLineEnd := i == n || math.Abs(texts[i].Y-texts[lineStart].Y) > lineTolerance

		if isLineEnd && i > lineStart+1 {
			// Sort this line segment by X
			lineTexts := texts[lineStart:i]
			sort.Slice(lineTexts, func(a, b int) bool {
				return lineTexts[a].X < lineTexts[b].X
			})
		}

		if isLineEnd && i < n {
			lineStart = i
		}
	}

	return texts
}
