// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"math"
)

// SpatialIndex provides spatial indexing for efficient text location queries
// This is a simple implementation using a grid-based approach; for production use,
// consider a more sophisticated structure like R-tree
type SpatialIndex struct {
	grid         map[GridKey][]Text
	cellSize     float64
	bounds       Rect
	texts        []Text
}

// GridKey represents a grid cell identifier
type GridKey struct {
	X, Y int
}

// NewSpatialIndex creates a new spatial index from text elements
func NewSpatialIndex(texts []Text) *SpatialIndex {
	if len(texts) == 0 {
		return &SpatialIndex{
			grid: make(map[GridKey][]Text),
			texts: texts,
		}
	}

	// Calculate bounds of all text elements
	var minX, minY, maxX, maxY float64
	for i, t := range texts {
		x := t.X
		y := t.Y
		w := t.W

		if i == 0 {
			minX, minY = x, y
			maxX, maxY = x+w, y+t.FontSize
		} else {
			minX = math.Min(minX, x)
			minY = math.Min(minY, y)
			maxX = math.Max(maxX, x+w)
			maxY = math.Max(maxY, y+t.FontSize)
		}
	}

	bounds := Rect{Min: Point{X: minX, Y: minY}, Max: Point{X: maxX, Y: maxY}}
	
	// Use average font size as cell size for reasonable granularity
	totalSize := 0.0
	for _, t := range texts {
		totalSize += t.FontSize
	}
	avgFontSize := totalSize / float64(len(texts))
	cellSize := avgFontSize * 2.0 // Make cells slightly larger than average font size

	si := &SpatialIndex{
		grid:     make(map[GridKey][]Text),
		cellSize: cellSize,
		bounds:   bounds,
		texts:    texts,
	}

	// Insert all text elements into grid
	for _, t := range texts {
		si.insert(t)
	}

	return si
}

// insert adds a text element to the spatial index
func (si *SpatialIndex) insert(t Text) {
	// Calculate which grid cells this text element overlaps
	minX, minY := si.worldToGrid(t.X, t.Y)
	maxX, maxY := si.worldToGrid(t.X+t.W, t.Y+t.FontSize)

	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			key := GridKey{X: x, Y: y}
			si.grid[key] = append(si.grid[key], t)
		}
	}
}

// worldToGrid converts world coordinates to grid coordinates
func (si *SpatialIndex) worldToGrid(x, y float64) (int, int) {
	gridX := int((x - si.bounds.Min.X) / si.cellSize)
	gridY := int((y - si.bounds.Min.Y) / si.cellSize)
	return gridX, gridY
}

// Query returns all text elements that potentially intersect with the given bounds
func (si *SpatialIndex) Query(bounds Rect) []Text {
	minX, minY := si.worldToGrid(bounds.Min.X, bounds.Min.Y)
	maxX, maxY := si.worldToGrid(bounds.Max.X, bounds.Max.Y)

	// Use a map to avoid duplicate results
	uniqueResults := make(map[string]Text)

	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			key := GridKey{X: x, Y: y}
			if cellTexts, exists := si.grid[key]; exists {
				for _, t := range cellTexts {
					// Additional check to ensure actual intersection
					if si.intersects(bounds, t) {
						// Use text content and position as unique identifier
						key := t.S + string(rune(int(t.X*100))) + string(rune(int(t.Y*100)))
						uniqueResults[key] = t
					}
				}
			}
		}
	}

	// Convert map back to slice
	results := make([]Text, 0, len(uniqueResults))
	for _, t := range uniqueResults {
		results = append(results, t)
	}

	return results
}

// intersects checks if a text element intersects with the given bounds
func (si *SpatialIndex) intersects(bounds Rect, t Text) bool {
	textBounds := Rect{
		Min: Point{X: t.X, Y: t.Y},
		Max: Point{X: t.X + t.W, Y: t.Y + t.FontSize},
	}
	
	return !(textBounds.Max.X < bounds.Min.X || 
	         textBounds.Min.X > bounds.Max.X || 
	         textBounds.Max.Y < bounds.Min.Y || 
	         textBounds.Min.Y > bounds.Max.Y)
}

// RTreeSpatialIndex provides a more sophisticated spatial index using a proper R-tree implementation
type RTreeSpatialIndex struct {
	root       *RTreeNode
	texts      []Text
	maxEntries int // Max entries per node
	minEntries int // Min entries per node (for rebalancing)
}

// RTreeNode represents a node in the R-tree
type RTreeNode struct {
	bounds   Rect
	children []*RTreeNode
	leaf     bool
	texts    []Text // Only used in leaf nodes
	level    int    // Level in the tree (0 for leaves)
}

// NewRTreeSpatialIndex creates a new R-tree based spatial index
func NewRTreeSpatialIndex(texts []Text) *RTreeSpatialIndex {
	rt := &RTreeSpatialIndex{
		maxEntries: 10, // Default max entries per node
		minEntries: 4,  // Default min entries per node
	}

	if len(texts) == 0 {
		rt.root = nil
		rt.texts = texts
		return rt
	}

	rt.texts = texts
	rt.root = rt.buildTree(texts)

	return rt
}

// buildTree builds the R-tree from a set of text elements
func (rt *RTreeSpatialIndex) buildTree(texts []Text) *RTreeNode {
	if len(texts) == 0 {
		return nil
	}

	// If the number of texts is less than or equal to maxEntries, create a leaf node
	if len(texts) <= rt.maxEntries {
		leaf := &RTreeNode{
			leaf:  true,
			texts: texts,
			level: 0,
		}
		leaf.bounds = rt.calculateBounds(texts)
		return leaf
	}

	// Otherwise, subdivide the texts into groups and create internal nodes
	// For this implementation, we'll use a simple approach by grouping by spatial proximity

	// Create root node
	root := &RTreeNode{
		leaf:  false,
		level: 1,
	}

	// Partition texts into groups and build child nodes
	groups := rt.partitionTexts(texts, rt.maxEntries)

	for _, group := range groups {
		child := rt.buildTree(group)
		if child != nil {
			root.children = append(root.children, child)
		}
	}

	root.bounds = rt.calculateNodeBounds(root)

	return root
}

// partitionTexts divides texts into groups based on spatial proximity
func (rt *RTreeSpatialIndex) partitionTexts(texts []Text, maxGroupSize int) [][]Text {
	if len(texts) <= maxGroupSize {
		return [][]Text{texts}
	}

	var groups [][]Text

	// For this simple implementation, we'll sort by X coordinate and group sequentially
	sorted := make([]Text, len(texts))
	copy(sorted, texts)

	// Sort by X position (left to right)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i].X > sorted[j].X {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Group texts sequentially
	for i := 0; i < len(sorted); i += maxGroupSize {
		end := i + maxGroupSize
		if end > len(sorted) {
			end = len(sorted)
		}
		groups = append(groups, sorted[i:end])
	}

	return groups
}

// calculateBounds calculates the bounding box for a slice of texts
func (rt *RTreeSpatialIndex) calculateBounds(texts []Text) Rect {
	if len(texts) == 0 {
		return Rect{}
	}

	minX, minY := texts[0].X, texts[0].Y
	maxX, maxY := texts[0].X+texts[0].W, texts[0].Y+texts[0].FontSize

	for _, t := range texts[1:] {
		x, y := t.X, t.Y
		w, h := t.W, t.FontSize

		minX = math.Min(minX, x)
		minY = math.Min(minY, y)
		maxX = math.Max(maxX, x+w)
		maxY = math.Max(maxY, y+h)
	}

	return Rect{
		Min: Point{X: minX, Y: minY},
		Max: Point{X: maxX, Y: maxY},
	}
}

// calculateNodeBounds calculates the bounding box for a node
func (rt *RTreeSpatialIndex) calculateNodeBounds(node *RTreeNode) Rect {
	if node.leaf {
		return rt.calculateBounds(node.texts)
	}

	if len(node.children) == 0 {
		return Rect{}
	}

	bounds := node.children[0].bounds
	for _, child := range node.children[1:] {
		bounds = rt.expandBounds(bounds, child.bounds)
	}

	return bounds
}

// expandBounds expands the first rectangle to include the second
func (rt *RTreeSpatialIndex) expandBounds(r1, r2 Rect) Rect {
	return Rect{
		Min: Point{
			X: math.Min(r1.Min.X, r2.Min.X),
			Y: math.Min(r1.Min.Y, r2.Min.Y),
		},
		Max: Point{
			X: math.Max(r1.Max.X, r2.Max.X),
			Y: math.Max(r1.Max.Y, r2.Max.Y),
		},
	}
}

// Insert adds a text element to the R-tree
func (rt *RTreeSpatialIndex) Insert(text Text) {
	if rt.root == nil {
		rt.root = &RTreeNode{
			leaf:  true,
			texts: []Text{text},
			level: 0,
			bounds: Rect{
				Min: Point{X: text.X, Y: text.Y},
				Max: Point{X: text.X + text.W, Y: text.Y + text.FontSize},
			},
		}
		rt.texts = append(rt.texts, text)
		return
	}

	// Insert and potentially split nodes if they become overfull
	splitRoot, newRoot := rt.insertNode(rt.root, text, 0)

	if splitRoot != nil {
		// Create a new root node
		rt.root = &RTreeNode{
			bounds:   rt.expandBounds(splitRoot.bounds, newRoot.bounds),
			children: []*RTreeNode{splitRoot, newRoot},
			leaf:     false,
			level:    splitRoot.level + 1,
		}
	}

	rt.texts = append(rt.texts, text)
}

// insertNode inserts a text into a node and returns any split nodes
func (rt *RTreeSpatialIndex) insertNode(node *RTreeNode, text Text, level int) (splitNode, newNode *RTreeNode) {
	textBounds := Rect{
		Min: Point{X: text.X, Y: text.Y},
		Max: Point{X: text.X + text.W, Y: text.Y + text.FontSize},
	}

	if node.level == level {
		if node.leaf {
			// Add text to leaf node
			node.texts = append(node.texts, text)
			node.bounds = rt.expandBounds(node.bounds, textBounds)

			// Check if node needs to be split
			if len(node.texts) > rt.maxEntries {
				return rt.splitNode(node)
			}
		} else {
			// Add to internal node - find the best child
			bestChild := rt.chooseBestSubtree(node, textBounds)
			splitChild, newChild := rt.insertNode(bestChild, text, level)

			if newChild != nil {
				// Child was split, update parent
				node.children = append(node.children[:0], node.children[0:]...) // copy
				// Replace the split child with the new children
				newChildren := make([]*RTreeNode, 0, len(node.children))
				for _, child := range node.children {
					if child == bestChild {
						newChildren = append(newChildren, splitChild, newChild)
					} else {
						newChildren = append(newChildren, child)
					}
				}
				node.children = newChildren

				// Update node bounds
				node.bounds = rt.calculateNodeBounds(node)

				// Check if parent needs to be split
				if len(node.children) > rt.maxEntries {
					return rt.splitNode(node)
				}
			} else {
				// Update node bounds to include the new text
				node.bounds = rt.expandBounds(node.bounds, textBounds)
			}
		}
		return nil, nil
	}

	// Go deeper into the tree
	return node, nil
}

// chooseBestSubtree finds the best child node for a given text
func (rt *RTreeSpatialIndex) chooseBestSubtree(node *RTreeNode, bounds Rect) *RTreeNode {
	var bestChild *RTreeNode
	minIncrease := math.MaxFloat64

	for _, child := range node.children {
		// Calculate the area increase if we add the bounds to this child
		currentArea := rt.rectangleArea(child.bounds)
		unionBounds := rt.expandBounds(child.bounds, bounds)
		unionArea := rt.rectangleArea(unionBounds)
		increase := unionArea - currentArea

		if increase < minIncrease || (increase == minIncrease && rt.rectangleArea(child.bounds) < rt.rectangleArea(bestChild.bounds)) {
			minIncrease = increase
			bestChild = child
		}
	}

	return bestChild
}

// rectangleArea calculates the area of a rectangle
func (rt *RTreeSpatialIndex) rectangleArea(r Rect) float64 {
	width := r.Max.X - r.Min.X
	height := r.Max.Y - r.Min.Y
	if width <= 0 || height <= 0 {
		return 0
	}
	return width * height
}

// splitNode splits an overfull node
func (rt *RTreeSpatialIndex) splitNode(node *RTreeNode) (*RTreeNode, *RTreeNode) {
	// Use a simple quadratic split algorithm
	if node.leaf {
		// Split leaf node's texts
		group1, group2 := rt.quadraticSplitTexts(node.texts)

		newNode1 := &RTreeNode{
			leaf:  true,
			texts: group1,
			level: node.level,
		}
		newNode1.bounds = rt.calculateBounds(group1)

		newNode2 := &RTreeNode{
			leaf:  true,
			texts: group2,
			level: node.level,
		}
		newNode2.bounds = rt.calculateBounds(group2)

		return newNode1, newNode2
	} else {
		// Split internal node's children
		group1, group2 := rt.quadraticSplitNodes(node.children)

		newNode1 := &RTreeNode{
			children: group1,
			leaf:     false,
			level:    node.level,
		}
		newNode1.bounds = rt.calculateNodeBounds(newNode1)

		newNode2 := &RTreeNode{
			children: group2,
			leaf:     false,
			level:    node.level,
		}
		newNode2.bounds = rt.calculateNodeBounds(newNode2)

		return newNode1, newNode2
	}
}

// quadraticSplitTexts performs a quadratic split of text elements
func (rt *RTreeSpatialIndex) quadraticSplitTexts(texts []Text) ([]Text, []Text) {
	if len(texts) <= 1 {
		return texts, []Text{}
	}

	// Find the two most distant texts
	maxDistance := -1.0
	var idx1, idx2 int

	for i := 0; i < len(texts); i++ {
		for j := i + 1; j < len(texts); j++ {
			dist := rt.textDistance(texts[i], texts[j])
			if dist > maxDistance {
				maxDistance = dist
				idx1, idx2 = i, j
			}
		}
	}

	// Distribute the remaining texts to the closest group
	group1 := []Text{texts[idx1]}
	group2 := []Text{texts[idx2]}

	for i, text := range texts {
		if i == idx1 || i == idx2 {
			continue
		}

		dist1 := rt.textDistance(text, texts[idx1])
		dist2 := rt.textDistance(text, texts[idx2])

		if dist1 < dist2 {
			group1 = append(group1, text)
		} else {
			group2 = append(group2, text)
		}
	}

	return group1, group2
}

// quadraticSplitNodes performs a quadratic split of child nodes
func (rt *RTreeSpatialIndex) quadraticSplitNodes(nodes []*RTreeNode) ([]*RTreeNode, []*RTreeNode) {
	if len(nodes) <= 1 {
		return nodes, []*RTreeNode{}
	}

	// Find the two nodes with the greatest distance
	maxDistance := -1.0
	var idx1, idx2 int

	for i := 0; i < len(nodes); i++ {
		for j := i + 1; j < len(nodes); j++ {
			dist := rt.nodeDistance(nodes[i], nodes[j])
			if dist > maxDistance {
				maxDistance = dist
				idx1, idx2 = i, j
			}
		}
	}

	// Distribute remaining nodes to the closest group
	group1 := []*RTreeNode{nodes[idx1]}
	group2 := []*RTreeNode{nodes[idx2]}

	for i, node := range nodes {
		if i == idx1 || i == idx2 {
			continue
		}

		dist1 := rt.nodeDistance(node, nodes[idx1])
		dist2 := rt.nodeDistance(node, nodes[idx2])

		if dist1 < dist2 {
			group1 = append(group1, node)
		} else {
			group2 = append(group2, node)
		}
	}

	return group1, group2
}

// textDistance calculates distance between text elements
func (rt *RTreeSpatialIndex) textDistance(t1, t2 Text) float64 {
	center1 := Point{X: t1.X + t1.W/2, Y: t1.Y + t1.FontSize/2}
	center2 := Point{X: t2.X + t2.W/2, Y: t2.Y + t2.FontSize/2}

	dx := center1.X - center2.X
	dy := center1.Y - center2.Y
	return math.Sqrt(dx*dx + dy*dy)
}

// nodeDistance calculates distance between nodes
func (rt *RTreeSpatialIndex) nodeDistance(n1, n2 *RTreeNode) float64 {
	center1 := Point{
		X: (n1.bounds.Min.X + n1.bounds.Max.X) / 2,
		Y: (n1.bounds.Min.Y + n1.bounds.Max.Y) / 2,
	}
	center2 := Point{
		X: (n2.bounds.Min.X + n2.bounds.Max.X) / 2,
		Y: (n2.bounds.Min.Y + n2.bounds.Max.Y) / 2,
	}

	dx := center1.X - center2.X
	dy := center1.Y - center2.Y
	return math.Sqrt(dx*dx + dy*dy)
}

// Query returns all text elements that intersect with the given bounds
func (rt *RTreeSpatialIndex) Query(bounds Rect) []Text {
	if rt.root == nil {
		return []Text{}
	}

	return rt.queryNode(rt.root, bounds)
}

// queryNode recursively queries nodes in the R-tree
func (rt *RTreeSpatialIndex) queryNode(node *RTreeNode, bounds Rect) []Text {
	// Check if bounds intersect
	if !rt.intersects(node.bounds, bounds) {
		return []Text{}
	}

	if node.leaf {
		// For leaf nodes, check each text element
		var results []Text
		for _, t := range node.texts {
			textBounds := Rect{
				Min: Point{X: t.X, Y: t.Y},
				Max: Point{X: t.X + t.W, Y: t.Y + t.FontSize},
			}
			if rt.intersects(textBounds, bounds) {
				results = append(results, t)
			}
		}
		return results
	}

	// For internal nodes, query children
	var results []Text
	for _, child := range node.children {
		childResults := rt.queryNode(child, bounds)
		results = append(results, childResults...)
	}
	return results
}

// intersects checks if two rectangles intersect
func (rt *RTreeSpatialIndex) intersects(rect1, rect2 Rect) bool {
	return !(rect1.Max.X < rect2.Min.X ||
	         rect1.Min.X > rect2.Max.X ||
	         rect1.Max.Y < rect2.Min.Y ||
	         rect1.Min.Y > rect2.Max.Y)
}

// SpatialIndex interface to allow using either grid or R-tree implementation
type SpatialIndexInterface interface {
	Query(bounds Rect) []Text
	Insert(text Text)
}

// NewSpatialIndexInterface creates a spatial index interface (can be switched between implementations)
func NewSpatialIndexInterface(texts []Text) SpatialIndexInterface {
	// For now, return the R-tree implementation
	return NewRTreeSpatialIndex(texts)
}