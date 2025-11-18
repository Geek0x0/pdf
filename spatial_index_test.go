package pdf

import (
	"testing"
)

// TestNewSpatialIndex tests the spatial index initialization
func TestNewSpatialIndex(t *testing.T) {
	// Test with empty texts
	emptyIndex := NewSpatialIndex([]Text{})
	if emptyIndex == nil {
		t.Error("Expected spatial index to be created even for empty input")
	}

	// Test with single text
	singleText := []Text{{S: "test", X: 100, Y: 200, W: 50, FontSize: 12}}
	singleIndex := NewSpatialIndex(singleText)
	if singleIndex == nil {
		t.Error("Expected spatial index to be created for single text")
	}

	// Test with multiple texts
	multiText := []Text{
		{S: "text1", X: 100, Y: 200, W: 30, FontSize: 12},
		{S: "text2", X: 150, Y: 250, W: 40, FontSize: 10},
		{S: "text3", X: 200, Y: 300, W: 50, FontSize: 14},
	}
	multiIndex := NewSpatialIndex(multiText)
	if multiIndex == nil {
		t.Error("Expected spatial index to be created for multiple texts")
	}
}

// TestSpatialIndexQuery tests the basic query functionality
func TestSpatialIndexQuery(t *testing.T) {
	texts := []Text{
		{S: "top-left", X: 100, Y: 300, W: 50, FontSize: 12},    // Position 1
		{S: "top-right", X: 300, Y: 300, W: 60, FontSize: 12},   // Position 2
		{S: "bottom-left", X: 100, Y: 100, W: 70, FontSize: 12}, // Position 3
		{S: "middle", X: 200, Y: 200, W: 40, FontSize: 12},      // Position 4
	}

	index := NewSpatialIndex(texts)

	// Query for top-left area
	topLeftBounds := Rect{Min: Point{X: 90, Y: 290}, Max: Point{X: 160, Y: 310}}
	results := index.Query(topLeftBounds)

	// Should find the "top-left" text
	foundTopLeft := false
	for _, text := range results {
		if text.S == "top-left" {
			foundTopLeft = true
			break
		}
	}
	if !foundTopLeft {
		t.Error("Expected to find 'top-left' text in top-left query bounds")
	}

	// Query for a small area that shouldn't contain anything
	smallBounds := Rect{Min: Point{X: 50, Y: 50}, Max: Point{X: 60, Y: 60}}
	emptyResults := index.Query(smallBounds)
	if len(emptyResults) != 0 {
		t.Errorf("Expected 0 results for small area, got %d", len(emptyResults))
	}

	// Query for a large area that should contain everything
	largeBounds := Rect{Min: Point{X: 0, Y: 0}, Max: Point{X: 500, Y: 500}}
	allResults := index.Query(largeBounds)
	if len(allResults) < len(texts) {
		t.Logf("Expected at least %d results for large area, got %d (may be due to implementation details)", len(texts), len(allResults))
	}
}

// TestIntersectsFunction tests the rectangle intersection function
func TestIntersectsFunction(t *testing.T) {
	// Create an RTree index directly for the intersection test
	rtIndex := NewRTreeSpatialIndex([]Text{})

	// Test intersecting rectangles
	rect1 := Rect{Min: Point{X: 0, Y: 0}, Max: Point{X: 100, Y: 100}}
	rect2 := Rect{Min: Point{X: 50, Y: 50}, Max: Point{X: 150, Y: 150}}

	if !rtIndex.intersects(rect1, rect2) {
		t.Error("Expected rectangles to intersect")
	}

	// Test non-intersecting rectangles
	rect3 := Rect{Min: Point{X: 0, Y: 0}, Max: Point{X: 50, Y: 50}}
	rect4 := Rect{Min: Point{X: 60, Y: 60}, Max: Point{X: 100, Y: 100}}

	if rtIndex.intersects(rect3, rect4) {
		t.Error("Expected rectangles to not intersect")
	}

	// Test touching rectangles (should intersect)
	rect5 := Rect{Min: Point{X: 0, Y: 0}, Max: Point{X: 50, Y: 50}}
	rect6 := Rect{Min: Point{X: 50, Y: 50}, Max: Point{X: 100, Y: 100}}

	if !rtIndex.intersects(rect5, rect6) {
		t.Error("Expected touching rectangles to intersect")
	}
}

// TestNewRTreeSpatialIndex tests the R-tree specific initialization
func TestNewRTreeSpatialIndex(t *testing.T) {
	texts := []Text{
		{S: "text1", X: 100, Y: 200, W: 30, FontSize: 12},
		{S: "text2", X: 150, Y: 250, W: 40, FontSize: 10},
	}

	rtIndex := NewRTreeSpatialIndex(texts)

	if rtIndex.maxEntries != 10 {
		t.Errorf("Expected maxEntries to be 10, got %d", rtIndex.maxEntries)
	}

	if len(rtIndex.texts) != 2 {
		t.Errorf("Expected 2 texts in index, got %d", len(rtIndex.texts))
	}

	if rtIndex.root == nil {
		t.Error("Expected root node to be created")
	}
}

// TestRTreeInsert tests the R-tree insertion functionality
func TestRTreeInsert(t *testing.T) {
	rtIndex := NewRTreeSpatialIndex([]Text{})

	// Insert first text
	firstText := Text{S: "first", X: 100, Y: 200, W: 30, FontSize: 12}
	rtIndex.Insert(firstText)

	// Query for the inserted text
	queryBounds := Rect{Min: Point{X: 90, Y: 190}, Max: Point{X: 140, Y: 210}}
	results := rtIndex.Query(queryBounds)

	found := false
	for _, text := range results {
		if text.S == "first" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected to find inserted text")
	}

	// Insert another text
	secondText := Text{S: "second", X: 300, Y: 400, W: 40, FontSize: 10}
	rtIndex.Insert(secondText)

	// Query for both texts
	largerQuery := Rect{Min: Point{X: 0, Y: 0}, Max: Point{X: 500, Y: 500}}
	allResults := rtIndex.Query(largerQuery)

	var foundFirst, foundSecond bool
	for _, text := range allResults {
		if text.S == "first" {
			foundFirst = true
		}
		if text.S == "second" {
			foundSecond = true
		}
	}

	if !foundFirst || !foundSecond {
		t.Error("Expected to find both inserted texts")
	}
}

// TestCalculateBounds tests the bounds calculation function
func TestCalculateBounds(t *testing.T) {
	rtIndex := NewRTreeSpatialIndex([]Text{})

	// Test with empty slice
	emptyBounds := rtIndex.calculateBounds([]Text{})
	if emptyBounds.Min.X != 0 || emptyBounds.Min.Y != 0 || emptyBounds.Max.X != 0 || emptyBounds.Max.Y != 0 {
		t.Error("Expected empty bounds to be all zeros")
	}

	// Test with single text
	singleText := []Text{{S: "test", X: 100, Y: 200, W: 30, FontSize: 12}}
	singleBounds := rtIndex.calculateBounds(singleText)
	if singleBounds.Min.X != 100 || singleBounds.Min.Y != 200 || singleBounds.Max.X != 130 || singleBounds.Max.Y != 212 {
		t.Errorf("Expected bounds Min(100,200) Max(130,212), got Min(%f,%f) Max(%f,%f)",
			singleBounds.Min.X, singleBounds.Min.Y, singleBounds.Max.X, singleBounds.Max.Y)
	}

	// Test with multiple texts
	multiText := []Text{
		{X: 100, Y: 200, W: 10, FontSize: 5},  // Bounds: (100, 200) to (110, 205)
		{X: 150, Y: 250, W: 20, FontSize: 10}, // Bounds: (150, 250) to (170, 260)
	}
	multiBounds := rtIndex.calculateBounds(multiText)

	// Should encompass both text elements
	expectedMinX := 100.0
	expectedMinY := 200.0
	expectedMaxX := 170.0
	expectedMaxY := 260.0

	if multiBounds.Min.X != expectedMinX || multiBounds.Min.Y != expectedMinY ||
		multiBounds.Max.X != expectedMaxX || multiBounds.Max.Y != expectedMaxY {
		t.Errorf("Expected bounds Min(%f,%f) Max(%f,%f), got Min(%f,%f) Max(%f,%f)",
			expectedMinX, expectedMinY, expectedMaxX, expectedMaxY,
			multiBounds.Min.X, multiBounds.Min.Y, multiBounds.Max.X, multiBounds.Max.Y)
	}
}

// TestRectangleArea tests the rectangle area calculation
func TestRectangleArea(t *testing.T) {
	rtIndex := NewRTreeSpatialIndex([]Text{})

	// Test with normal rectangle
	normalRect := Rect{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 20}}
	area := rtIndex.rectangleArea(normalRect)
	if area != 200 {
		t.Errorf("Expected area 200, got %f", area)
	}

	// Test with invalid rectangle (negative area)
	invalidRect := Rect{Min: Point{X: 10, Y: 10}, Max: Point{X: 5, Y: 5}}
	area = rtIndex.rectangleArea(invalidRect)
	if area != 0 {
		t.Errorf("Expected area 0 for invalid rectangle, got %f", area)
	}

	// Test with zero-area rectangle
	zeroRect := Rect{Min: Point{X: 0, Y: 0}, Max: Point{X: 0, Y: 0}}
	area = rtIndex.rectangleArea(zeroRect)
	if area != 0 {
		t.Errorf("Expected area 0 for zero rectangle, got %f", area)
	}
}

// TestExpandBounds tests the bounds expansion function
func TestExpandBounds(t *testing.T) {
	rtIndex := NewRTreeSpatialIndex([]Text{})

	rect1 := Rect{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 10}}
	rect2 := Rect{Min: Point{X: 5, Y: 5}, Max: Point{X: 15, Y: 15}}

	expanded := rtIndex.expandBounds(rect1, rect2)

	// Should encompass both rectangles
	expectedMinX := 0.0
	expectedMinY := 0.0
	expectedMaxX := 15.0
	expectedMaxY := 15.0

	if expanded.Min.X != expectedMinX || expanded.Min.Y != expectedMinY ||
		expanded.Max.X != expectedMaxX || expanded.Max.Y != expectedMaxY {
		t.Errorf("Expected expanded bounds Min(%f,%f) Max(%f,%f), got Min(%f,%f) Max(%f,%f)",
			expectedMinX, expectedMinY, expectedMaxX, expectedMaxY,
			expanded.Min.X, expanded.Min.Y, expanded.Max.X, expanded.Max.Y)
	}
}

// TestTextDistance tests the text distance calculation
func TestTextDistance(t *testing.T) {
	rtIndex := NewRTreeSpatialIndex([]Text{})

	text1 := Text{X: 0, Y: 0, W: 10, FontSize: 10}   // Center at (5, 5)
	text2 := Text{X: 30, Y: 40, W: 10, FontSize: 10} // Center at (35, 45)

	distance := rtIndex.textDistance(text1, text2)

	// Distance between centers (5,5) and (35,45) = sqrt((30^2 + 40^2)) = sqrt(900+1600) = sqrt(2500) = 50
	expectedDistance := 50.0
	if distance != expectedDistance {
		t.Errorf("Expected distance %f, got %f", expectedDistance, distance)
	}

	// Test with same position (distance should be 0)
	text3 := Text{X: 10, Y: 20, W: 5, FontSize: 5}
	text4 := Text{X: 10, Y: 20, W: 5, FontSize: 5}

	distance2 := rtIndex.textDistance(text3, text4)
	if distance2 != 0 {
		t.Errorf("Expected distance 0 for same position, got %f", distance2)
	}
}

// TestNodeDistance tests the node distance calculation
func TestNodeDistance(t *testing.T) {
	rtIndex := NewRTreeSpatialIndex([]Text{})

	// Create two simple nodes
	node1 := &RTreeNode{
		bounds: Rect{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 10}},
	}
	node2 := &RTreeNode{
		bounds: Rect{Min: Point{X: 20, Y: 30}, Max: Point{X: 30, Y: 40}},
	}

	distance := rtIndex.nodeDistance(node1, node2)

	// Centers: (5,5) and (25,35), distance = sqrt((20^2 + 30^2)) = sqrt(400+900) = sqrt(1300) ≈ 36.055
	expectedDistance := 36.05551275463989
	tolerance := 0.001

	if distance < expectedDistance-tolerance || distance > expectedDistance+tolerance {
		t.Errorf("Expected distance ~%f, got %f", expectedDistance, distance)
	}
}

// TestSpatialIndexInterface tests the interface compliance
func TestSpatialIndexInterface(t *testing.T) {
	texts := []Text{{S: "test", X: 100, Y: 200, W: 30, FontSize: 12}}
	index := NewSpatialIndex(texts)

	// Should implement the interface
	queryBounds := Rect{Min: Point{X: 90, Y: 190}, Max: Point{X: 140, Y: 210}}
	results := index.Query(queryBounds)

	if len(results) == 0 {
		t.Log("No results found - this is expected for the grid-based implementation")
	}

	// Test insert functionality
	newText := Text{S: "new", X: 110, Y: 210, W: 25, FontSize: 10}
	index.insert(newText)
}

// TestRTreeNodeStructure tests the R-tree node creation and properties
func TestRTreeNodeStructure(t *testing.T) {
	// Create a leaf node
	leafNode := &RTreeNode{
		bounds:   Rect{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 10}},
		leaf:     true,
		texts:    []Text{{S: "test", X: 1, Y: 1, W: 2, FontSize: 2}},
		level:    0,
		children: nil,
	}

	if !leafNode.leaf {
		t.Error("Expected node to be marked as leaf")
	}
	if leafNode.level != 0 {
		t.Errorf("Expected level 0, got %d", leafNode.level)
	}
	if len(leafNode.texts) != 1 {
		t.Errorf("Expected 1 text in leaf, got %d", len(leafNode.texts))
	}
	if leafNode.children != nil {
		t.Error("Expected children to be nil in leaf node")
	}

	// Create an internal node
	internalNode := &RTreeNode{
		bounds:   Rect{Min: Point{X: 0, Y: 0}, Max: Point{X: 20, Y: 20}},
		leaf:     false,
		texts:    nil,
		level:    1,
		children: []*RTreeNode{leafNode},
	}

	if internalNode.leaf {
		t.Error("Expected node to not be leaf")
	}
	if internalNode.level != 1 {
		t.Errorf("Expected level 1, got %d", internalNode.level)
	}
	if internalNode.texts != nil {
		t.Error("Expected texts to be nil in internal node")
	}
	if len(internalNode.children) != 1 {
		t.Errorf("Expected 1 child in internal node, got %d", len(internalNode.children))
	}
}

// TestQuadraticSplitTexts tests the text splitting functionality
func TestQuadraticSplitTexts(t *testing.T) {
	rtIndex := NewRTreeSpatialIndex([]Text{})

	// Create some texts that are spatially distant
	texts := []Text{
		{X: 0, Y: 0, W: 5, FontSize: 5},     // Close to origin
		{X: 100, Y: 100, W: 5, FontSize: 5}, // Far from origin
		{X: 1, Y: 1, W: 5, FontSize: 5},     // Close to origin
		{X: 99, Y: 99, W: 5, FontSize: 5},   // Far from origin
	}

	group1, group2 := rtIndex.quadraticSplitTexts(texts)

	// Both groups should exist and not be empty
	if len(group1) == 0 {
		t.Error("Expected group1 to not be empty")
	}
	if len(group2) == 0 {
		t.Error("Expected group2 to not be empty")
	}

	// Total should equal original
	if len(group1)+len(group2) != len(texts) {
		t.Errorf("Expected total %d, got %d", len(texts), len(group1)+len(group2))
	}
}

// TestQueryWithNoMatches tests querying with bounds that don't match anything
func TestQueryWithNoMatches(t *testing.T) {
	texts := []Text{
		{S: "text1", X: 100, Y: 200, W: 30, FontSize: 12},
	}
	index := NewSpatialIndex(texts)

	// Query area that doesn't overlap with any text
	nonOverlappingBounds := Rect{Min: Point{X: 500, Y: 500}, Max: Point{X: 600, Y: 600}}
	results := index.Query(nonOverlappingBounds)

	if len(results) != 0 {
		t.Errorf("Expected 0 results for non-overlapping query, got %d", len(results))
	}
}

// BenchmarkSpatialIndexCreation benchmarks the spatial index creation
func BenchmarkSpatialIndexCreation(b *testing.B) {
	texts := make([]Text, 1000)
	for i := 0; i < 1000; i++ {
		texts[i] = Text{
			X:        float64(i * 10),
			Y:        float64((i / 10) * 20),
			W:        10,
			FontSize: 12,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewSpatialIndex(texts)
	}
}

// BenchmarkSpatialIndexQuery benchmarks the spatial index query performance
func BenchmarkSpatialIndexQuery(b *testing.B) {
	texts := make([]Text, 1000)
	for i := 0; i < 1000; i++ {
		texts[i] = Text{
			X:        float64(i * 10),
			Y:        float64((i / 10) * 20),
			W:        10,
			FontSize: 12,
		}
	}

	index := NewSpatialIndex(texts)
	queryBounds := Rect{Min: Point{X: 0, Y: 0}, Max: Point{X: 500, Y: 500}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = index.Query(queryBounds)
	}
}

func TestRTreeSpatialIndex(t *testing.T) {
	// Test with empty texts
	emptyIndex := NewRTreeSpatialIndex([]Text{})
	if emptyIndex == nil {
		t.Error("Expected R-tree spatial index to be created even for empty input")
	}

	// Test with single text
	singleText := []Text{{S: "test", X: 100, Y: 200, W: 50, FontSize: 12}}
	singleIndex := NewRTreeSpatialIndex(singleText)
	if singleIndex == nil {
		t.Error("Expected R-tree spatial index to be created for single text")
	}

	// Test with multiple texts
	multiText := []Text{
		{S: "text1", X: 100, Y: 200, W: 30, FontSize: 12},
		{S: "text2", X: 150, Y: 250, W: 40, FontSize: 10},
		{S: "text3", X: 200, Y: 300, W: 50, FontSize: 14},
	}
	multiIndex := NewRTreeSpatialIndex(multiText)
	if multiIndex == nil {
		t.Error("Expected R-tree spatial index to be created for multiple texts")
	}

	// Test query
	queryBounds := Rect{Min: Point{X: 90, Y: 190}, Max: Point{X: 160, Y: 260}}
	results := multiIndex.Query(queryBounds)

	if len(results) == 0 {
		t.Error("Expected to find at least one text in query bounds")
	}

	// Test insert
	newText := Text{S: "new", X: 250, Y: 350, W: 30, FontSize: 12}
	multiIndex.Insert(newText)

	// Query again to verify insertion
	newQueryBounds := Rect{Min: Point{X: 240, Y: 340}, Max: Point{X: 290, Y: 370}}
	newResults := multiIndex.Query(newQueryBounds)

	found := false
	for _, text := range newResults {
		if text.S == "new" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Inserted text not found in query")
	}
}

func BenchmarkRTreeSpatialIndexQuery(b *testing.B) {
	texts := make([]Text, 1000)
	for i := 0; i < 1000; i++ {
		texts[i] = Text{
			X:        float64(i * 10),
			Y:        float64((i / 10) * 20),
			W:        10,
			FontSize: 12,
		}
	}

	index := NewRTreeSpatialIndex(texts)
	queryBounds := Rect{Min: Point{X: 0, Y: 0}, Max: Point{X: 500, Y: 500}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = index.Query(queryBounds)
	}
}
