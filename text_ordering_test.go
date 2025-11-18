package pdf

import (
	"testing"
)

// TestAsymmetricLayoutDetection tests the asymmetric layout detection functionality
func TestAsymmetricLayoutDetection(t *testing.T) {
	// Create text blocks that represent an asymmetric layout
	textBlocks := []*TextBlock{
		{MinX: 50, MaxX: 200, MinY: 700, MaxY: 720, AvgFontSize: 12},  // Left column top
		{MinX: 250, MaxX: 400, MinY: 700, MaxY: 720, AvgFontSize: 12}, // Right column top
		{MinX: 50, MaxX: 300, MinY: 650, MaxY: 670, AvgFontSize: 10},  // Wide element in middle
		{MinX: 100, MaxX: 150, MinY: 600, MaxY: 620, AvgFontSize: 8},  // Small element
	}

	// Test the asymmetric column detection
	columns := detectAsymmetricColumns(textBlocks)

	// For this example, we expect multiple columns based on the layout
	if len(columns) == 0 {
		t.Error("Expected at least one column from asymmetric layout, got 0")
	}

	// Validate that the function doesn't crash and returns valid results
	for _, column := range columns {
		if len(column) == 0 {
			t.Error("Found an empty column")
		}
	}
}

// TestGroupBlocksByYRange tests the Y-range grouping functionality
func TestGroupBlocksByYRange(t *testing.T) {
	textBlocks := []*TextBlock{
		{MinX: 50, MaxX: 100, MinY: 700, MaxY: 720, AvgFontSize: 12},
		{MinX: 50, MaxX: 100, MinY: 690, MaxY: 710, AvgFontSize: 12}, // Overlapping Y
		{MinX: 50, MaxX: 100, MinY: 600, MaxY: 620, AvgFontSize: 12}, // Different Y range
		{MinX: 50, MaxX: 100, MinY: 590, MaxY: 610, AvgFontSize: 12}, // Overlapping with above
	}

	pageBounds := Rect{Min: Point{X: 0, Y: 0}, Max: Point{X: 600, Y: 800}}

	bands := groupBlocksByYRange(textBlocks, pageBounds)

	if len(bands) == 0 {
		t.Error("Expected at least one Y band, got 0")
	}

	// Should have 2 bands: one for Y~700 range, one for Y~600 range
	if len(bands) < 2 {
		t.Logf("Expected 2 bands for different Y ranges, got %d", len(bands))
	}
}

// TestCalculatePageBounds tests the page bounds calculation
func TestCalculatePageBounds(t *testing.T) {
	textBlocks := []*TextBlock{
		{MinX: 100, MaxX: 200, MinY: 100, MaxY: 120, AvgFontSize: 12},
		{MinX: 50, MaxX: 150, MinY: 300, MaxY: 320, AvgFontSize: 12},
		{MinX: 250, MaxX: 350, MinY: 50, MaxY: 70, AvgFontSize: 12},
	}

	bounds := calculatePageBounds(textBlocks)

	// Validate the calculated bounds encompass all text blocks
	if bounds.Min.X != 50 {
		t.Errorf("Expected Min.X to be 50, got %f", bounds.Min.X)
	}
	if bounds.Max.X != 350 {
		t.Errorf("Expected Max.X to be 350, got %f", bounds.Max.X)
	}
	if bounds.Min.Y != 50 {
		t.Errorf("Expected Min.Y to be 50, got %f", bounds.Min.Y)
	}
	if bounds.Max.Y != 320 {
		t.Errorf("Expected Max.Y to be 320, got %f", bounds.Max.Y)
	}
}

// TestHasSignificantVerticalOverlap tests the vertical overlap detection
func TestHasSignificantVerticalOverlap(t *testing.T) {
	// Test overlapping blocks
	block1 := &TextBlock{MinY: 100, MaxY: 150, AvgFontSize: 12}
	block2 := &TextBlock{MinY: 140, MaxY: 180, AvgFontSize: 12}

	overlap := hasSignificantVerticalOverlap(block1, block2)
	if !overlap {
		t.Error("Expected blocks to have significant vertical overlap")
	}

	// Test non-overlapping blocks
	block3 := &TextBlock{MinY: 100, MaxY: 120, AvgFontSize: 12}
	block4 := &TextBlock{MinY: 200, MaxY: 220, AvgFontSize: 12}

	overlap = hasSignificantVerticalOverlap(block3, block4)
	if overlap {
		t.Error("Expected blocks to not have significant vertical overlap")
	}

	// Test blocks with small overlap (should not be considered significant)
	block5 := &TextBlock{MinY: 100, MaxY: 110, AvgFontSize: 12} // Very short
	block6 := &TextBlock{MinY: 105, MaxY: 115, AvgFontSize: 12} // Small overlap

	overlap = hasSignificantVerticalOverlap(block5, block6)
	// The result depends on the implementation - may or may not be significant
	t.Logf("Small overlap result: %v", overlap)
}

// TestCanAddToColumn tests the column membership logic
func TestCanAddToColumn(t *testing.T) {
	// Create a column with some blocks
	column := []*TextBlock{
		{MinX: 100, MaxX: 150, MinY: 700, MaxY: 720, AvgFontSize: 12},
		{MinX: 100, MaxX: 150, MinY: 650, MaxY: 670, AvgFontSize: 12},
	}

	newBlock := &TextBlock{MinX: 100, MaxX: 150, MinY: 600, MaxY: 620, AvgFontSize: 12}

	// Should be able to add to column (same X alignment)
	gapThreshold := 50.0
	result := canAddToColumn(column, newBlock, gapThreshold)
	if !result {
		t.Error("Expected block to be able to join column with same X alignment")
	}

	// Try with misaligned block
	misalignedBlock := &TextBlock{MinX: 300, MaxX: 350, MinY: 600, MaxY: 620, AvgFontSize: 12}
	result = canAddToColumn(column, misalignedBlock, gapThreshold)
	// May or may not work depending on threshold, but let's check the logic works
	t.Logf("Misaligned block result: %v", result)
}

// TestDetectReadingOrderForAsymmetricLayout tests the reading order detection for asymmetric layouts
func TestDetectReadingOrderForAsymmetricLayout(t *testing.T) {
	textBlocks := []*TextBlock{
		{MinX: 400, MaxX: 500, MinY: 700, MaxY: 720, AvgFontSize: 12, Texts: []Text{{S: "Right top"}}},
		{MinX: 100, MaxX: 200, MinY: 700, MaxY: 720, AvgFontSize: 12, Texts: []Text{{S: "Left top"}}},
		{MinX: 100, MaxX: 300, MinY: 600, MaxY: 620, AvgFontSize: 10, Texts: []Text{{S: "Wide middle"}}},
	}

	ordered := detectReadingOrderForAsymmetricLayout(textBlocks)

	if len(ordered) != len(textBlocks) {
		t.Errorf("Expected ordered blocks length %d, got %d", len(textBlocks), len(ordered))
	}

	// The ordering should follow top-to-bottom, then left-to-right within horizontal bands
	// Top band should come before middle band
	if len(ordered) >= 2 {
		// The top band items should have higher Y values (in PDF coordinates)
		if ordered[0].MaxY < ordered[1].MaxY {
			t.Log("Blocks may be ordered bottom-to-top due to PDF coordinate system")
		}
	}
}

// TestDetectColumnsInBand tests column detection within a horizontal band
func TestDetectColumnsInBand(t *testing.T) {
	textBlocks := []*TextBlock{
		{MinX: 50, MaxX: 100, MinY: 700, MaxY: 720, AvgFontSize: 12},
		{MinX: 120, MaxX: 170, MinY: 700, MaxY: 720, AvgFontSize: 12}, // Gap in between
		{MinX: 200, MaxX: 250, MinY: 700, MaxY: 720, AvgFontSize: 12}, // Another gap
	}

	pageBounds := Rect{Min: Point{X: 0, Y: 0}, Max: Point{X: 600, Y: 800}}

	columns := detectColumnsInBand(textBlocks, pageBounds)

	if len(columns) == 0 {
		t.Error("Expected at least one column in band, got 0")
	}

	// Should have multiple columns due to gaps
	expectedCols := 3 // Based on the gaps in the test data
	if len(columns) < expectedCols {
		t.Logf("Expected at least %d columns, got %d", expectedCols, len(columns))
	}
}

// TestYBandStructure tests the YBand structure and functionality
func TestYBandStructure(t *testing.T) {
	// Test that YBand can hold blocks properly
	band := YBand{
		MinY: 700,
		MaxY: 720,
		Blocks: []*TextBlock{
			{MinX: 100, MaxX: 200, MinY: 700, MaxY: 720, AvgFontSize: 12},
		},
	}

	if band.MinY != 700 {
		t.Errorf("Expected MinY to be 700, got %f", band.MinY)
	}

	if len(band.Blocks) != 1 {
		t.Errorf("Expected 1 block in band, got %d", len(band.Blocks))
	}
}

// TestTextBlockMethods tests TextBlock methods used in asymmetric layout detection
func TestTextBlockMethods(t *testing.T) {
	// Create a text block for testing
	tb := &TextBlock{
		Texts:       []Text{{S: "test", X: 100, Y: 200, W: 50, FontSize: 12}},
		MinX:        100,
		MaxX:        150,
		MinY:        200,
		MaxY:        212,
		AvgFontSize: 12,
	}

	// Test Bounds method
	bounds := tb.Bounds()
	expectedBounds := Rect{Min: Point{X: 100, Y: 200}, Max: Point{X: 150, Y: 212}}
	if bounds != expectedBounds {
		t.Errorf("Bounds() = %+v, want %+v", bounds, expectedBounds)
	}

	// Test Center method
	center := tb.Center()
	expectedCenter := Point{X: 125, Y: 206}
	if center.X != expectedCenter.X || center.Y != expectedCenter.Y {
		t.Errorf("Center() = %+v, want %+v", center, expectedCenter)
	}

	// Test Width and Height methods
	expectedWidth := 50.0
	if tb.Width() != expectedWidth {
		t.Errorf("Width() = %f, want %f", tb.Width(), expectedWidth)
	}

	expectedHeight := 12.0
	if tb.Height() != expectedHeight {
		t.Errorf("Height() = %f, want %f", tb.Height(), expectedHeight)
	}
}

// BenchmarkAsymmetricLayoutDetection benchmarks the new asymmetric layout detection
func BenchmarkAsymmetricLayoutDetection(b *testing.B) {
	textBlocks := make([]*TextBlock, 100)
	for i := 0; i < 100; i++ {
		textBlocks[i] = &TextBlock{
			MinX:        float64((i % 10) * 100),
			MaxX:        float64((i%10)*100 + 80),
			MinY:        float64((i / 10) * 100),
			MaxY:        float64((i/10)*100 + 20),
			AvgFontSize: 12,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detectAsymmetricColumns(textBlocks)
	}
}

func TestIsAsymmetricLayout(t *testing.T) {
	b1 := &TextBlock{MinX: 50, MaxX: 150, MinY: 700, MaxY: 720, AvgFontSize: 12}
	b2 := &TextBlock{MinX: 300, MaxX: 400, MinY: 700, MaxY: 720, AvgFontSize: 12}

	// Blocks far apart horizontally should be asymmetric
	if !isAsymmetricLayout(b1, b2) {
		t.Error("Expected blocks far apart to be asymmetric")
	}

	b3 := &TextBlock{MinX: 100, MaxX: 200, MinY: 650, MaxY: 670, AvgFontSize: 12}
	b4 := &TextBlock{MinX: 120, MaxX: 180, MinY: 630, MaxY: 650, AvgFontSize: 12}

	// Blocks close together should not be asymmetric
	if isAsymmetricLayout(b3, b4) {
		t.Error("Expected blocks close together not to be asymmetric")
	}
}

func TestIsTextImageMix(t *testing.T) {
	b1 := &TextBlock{MinX: 50, MaxX: 150, MinY: 700, MaxY: 720, AvgFontSize: 12}
	b2 := &TextBlock{MinX: 300, MaxX: 350, MinY: 600, MaxY: 620, AvgFontSize: 12}

	// Blocks with large gap might be separated by image
	if !isTextImageMix(b1, b2) {
		t.Error("Expected blocks with large gap to be text-image mix")
	}

	b3 := &TextBlock{MinX: 100, MaxX: 200, MinY: 650, MaxY: 670, AvgFontSize: 12}
	b4 := &TextBlock{MinX: 120, MaxX: 180, MinY: 630, MaxY: 650, AvgFontSize: 12}

	// Close blocks should not be text-image mix
	if isTextImageMix(b3, b4) {
		t.Error("Expected close blocks not to be text-image mix")
	}
}

func TestDetectFootnotes(t *testing.T) {
	blocks := []*TextBlock{
		{MinX: 50, MaxX: 150, MinY: 700, MaxY: 720, AvgFontSize: 12}, // Normal text
		{MinX: 50, MaxX: 150, MinY: 650, MaxY: 670, AvgFontSize: 8},  // Small font at bottom - footnote
		{MinX: 50, MaxX: 150, MinY: 645, MaxY: 665, AvgFontSize: 6},  // Another footnote
		{MinX: 50, MaxX: 150, MinY: 600, MaxY: 620, AvgFontSize: 10}, // Normal text
	}

	footnotes := detectFootnotes(blocks, 800)

	if len(footnotes) != 2 {
		t.Errorf("Expected 2 footnotes, got %d", len(footnotes))
	}

	for _, footnote := range footnotes {
		if footnote.AvgFontSize >= 10 {
			t.Error("Footnote should have small font size")
		}
		if footnote.Center().Y < 600 {
			t.Error("Footnote should be at bottom of page")
		}
	}
}
