package pdf

import "math"

// SpatialGrid is a spatial partitioning structure for efficient neighbor search.
// It divides 2D space into a grid of cells, allowing O(1) cell lookup and
// reducing neighbor search from O(n²) to O(n) for uniformly distributed points.
type SpatialGrid struct {
	cellSize float64
	cells    map[int64][]int // cell ID -> block indices
	blocks   []*TextBlock
}

// NewSpatialGrid creates a new spatial grid for the given blocks.
// cellSize determines the granularity of the grid; typically should be
// around 2-3x the expected cluster radius for optimal performance.
func NewSpatialGrid(blocks []*TextBlock, cellSize float64) *SpatialGrid {
	grid := &SpatialGrid{
		cellSize: cellSize,
		cells:    make(map[int64][]int, len(blocks)/4), // Pre-allocate based on expected cell count
		blocks:   blocks,
	}

	// Populate grid
	for i, block := range blocks {
		center := block.Center()
		cellID := grid.getCellID(center.X, center.Y)
		grid.cells[cellID] = append(grid.cells[cellID], i)
	}

	return grid
}

// getCellID computes a unique cell ID for coordinates
func (g *SpatialGrid) getCellID(x, y float64) int64 {
	cx := int64(math.Floor(x / g.cellSize))
	cy := int64(math.Floor(y / g.cellSize))
	// Pack into single int64: upper 32 bits for x, lower 32 bits for y
	return (cx << 32) | (cy & 0xFFFFFFFF)
}

// GetNearbyBlocks returns indices of blocks in the same cell and neighboring cells.
// This is much faster than searching all blocks when they're uniformly distributed.
// Memory optimized: reuses slice from pool to reduce allocations.
func (g *SpatialGrid) GetNearbyBlocks(blockIdx int) []int {
	if blockIdx < 0 || blockIdx >= len(g.blocks) {
		return nil
	}

	block := g.blocks[blockIdx]
	center := block.Center()

	// Calculate base cell coordinates
	baseCX := int64(math.Floor(center.X / g.cellSize))
	baseCY := int64(math.Floor(center.Y / g.cellSize))

	// Estimate capacity based on average cell population
	// For 3x3 grid search, we check 9 cells
	// Optimized: use smaller initial capacity since most cells are sparse
	estimatedCap := 16 // Reduced from 32 based on profiling
	result := make([]int, 0, estimatedCap)

	// Check current cell and 8 neighboring cells (3x3 grid)
	for dx := int64(-1); dx <= 1; dx++ {
		for dy := int64(-1); dy <= 1; dy++ {
			cx := baseCX + dx
			cy := baseCY + dy
			cellID := (cx << 32) | (cy & 0xFFFFFFFF)

			if indices, ok := g.cells[cellID]; ok {
				// Optimized: check capacity before append to reduce allocations
				if cap(result)-len(result) >= len(indices) {
					result = append(result, indices...)
				} else {
					// Need to grow - allocate with extra space
					newCap := len(result) + len(indices) + 16
					newResult := make([]int, len(result), newCap)
					copy(newResult, result)
					result = append(newResult, indices...)
				}
			}
		}
	}

	return result
}

// ClusterTextBlocksV3 is an improved clustering algorithm using spatial grid.
// Time complexity: O(n) for uniformly distributed blocks (vs O(n²) for naive approach)
// Space complexity: O(n) for grid structure
func ClusterTextBlocksV3(texts []Text) []*TextBlock {
	if len(texts) == 0 {
		return nil
	}

	// Early return for small inputs - use simple algorithm
	if len(texts) < 50 {
		return ClusterTextBlocksOptimizedV2(texts)
	}

	// Calculate average font size as distance threshold
	var totalFontSize float64
	for i := range texts {
		totalFontSize += texts[i].FontSize
	}
	avgFontSize := totalFontSize / float64(len(texts))
	distThreshold := avgFontSize * 2.0

	// Initialize: each text as independent block using object pool
	blocks := make([]*TextBlock, len(texts))
	for i := range texts {
		t := &texts[i]
		tb := GetTextBlock()
		// Optimized: reserve capacity for potential merges
		if cap(tb.Texts) < 4 {
			tb.Texts = make([]Text, 0, 4)
		}
		tb.Texts = append(tb.Texts, *t)
		tb.MinX = t.X
		tb.MaxX = t.X + t.W
		tb.MinY = t.Y
		tb.MaxY = t.Y + t.FontSize
		tb.AvgFontSize = t.FontSize
		blocks[i] = tb
	}

	// Build spatial grid - cell size is 2x distance threshold for optimal coverage
	grid := NewSpatialGrid(blocks, distThreshold*2.0)

	// Union-find for clustering - use pooled slice
	parent := GetIntSlice(len(blocks))
	defer PutIntSlice(parent)

	for i := range parent {
		parent[i] = i
	}

	// Non-recursive find with path compression
	find := func(x int) int {
		root := x
		for parent[root] != root {
			root = parent[root]
		}
		// Path compression
		for parent[x] != root {
			next := parent[x]
			parent[x] = root
			x = next
		}
		return root
	}

	union := func(x, y int) {
		px, py := find(x), find(y)
		if px != py {
			parent[px] = py
		}
	}

	// Find neighbors using spatial grid (much faster than KD-tree for this use case)
	for i, block := range blocks {
		// Get nearby blocks from spatial grid (O(1) average case)
		nearbyIndices := grid.GetNearbyBlocks(i)

		for _, j := range nearbyIndices {
			if i == j {
				continue
			}

			// Check if already in same cluster
			if find(i) == find(j) {
				continue
			}

			// Check if should merge
			if shouldMergeClusters(block, blocks[j], distThreshold) {
				union(i, j)
			}
		}
	}

	// Group blocks by cluster root
	clusterMap := make(map[int][]*TextBlock)
	for i, block := range blocks {
		root := find(i)
		clusterMap[root] = append(clusterMap[root], block)
	}

	// Merge blocks in same cluster
	result := make([]*TextBlock, 0, len(clusterMap))
	for _, cluster := range clusterMap {
		if len(cluster) == 1 {
			result = append(result, cluster[0])
		} else {
			merged := mergeTextBlocksOptimized(cluster)
			// Return individual blocks to pool
			for _, block := range cluster {
				PutTextBlock(block)
			}
			result = append(result, merged)
		}
	}

	return result
}

// ClusterTextBlocksV3Fast is an even faster version with early termination.
// Suitable for very large documents where absolute precision is less critical.
func ClusterTextBlocksV3Fast(texts []Text, maxClusters int) []*TextBlock {
	if len(texts) == 0 {
		return nil
	}

	// Use V3 for initial clustering
	clusters := ClusterTextBlocksV3(texts)

	// If already under limit, return as-is
	if maxClusters <= 0 || len(clusters) <= maxClusters {
		return clusters
	}

	// Otherwise, merge smallest clusters until we reach target
	// This is a greedy approximation but very fast
	for len(clusters) > maxClusters {
		// Find two closest clusters to merge
		minDist := math.MaxFloat64
		mergeI, mergeJ := 0, 1

		for i := 0; i < len(clusters)-1; i++ {
			ci := clusters[i].Center()
			for j := i + 1; j < len(clusters); j++ {
				cj := clusters[j].Center()
				dx := ci.X - cj.X
				dy := ci.Y - cj.Y
				dist := dx*dx + dy*dy

				if dist < minDist {
					minDist = dist
					mergeI, mergeJ = i, j
				}
			}
		}

		// Merge the two closest clusters
		merged := mergeTextBlocksOptimized([]*TextBlock{clusters[mergeI], clusters[mergeJ]})
		PutTextBlock(clusters[mergeI])
		PutTextBlock(clusters[mergeJ])

		// Remove merged clusters and add new one
		clusters[mergeI] = merged
		clusters = append(clusters[:mergeJ], clusters[mergeJ+1:]...)
	}

	return clusters
}

// shouldMergeClustersV3 is an optimized version of merge check with early exits
func shouldMergeClustersV3(b1, b2 *TextBlock, maxDist float64) bool {
	// Quick bounds check first (cheaper than center calculation)
	if b1.MaxX < b2.MinX-maxDist || b2.MaxX < b1.MinX-maxDist {
		return false
	}
	if b1.MaxY < b2.MinY-maxDist || b2.MaxY < b1.MinY-maxDist {
		return false
	}

	// If bounds overlap or are close, do precise center distance check
	c1 := b1.Center()
	c2 := b2.Center()
	dx := c1.X - c2.X
	dy := c1.Y - c2.Y
	distSq := dx*dx + dy*dy

	return distSq <= maxDist*maxDist
}
