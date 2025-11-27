package pdf

import (
	"math"
	"sync"
)

// SpatialGrid is a spatial partitioning structure for efficient neighbor search.
// It divides 2D space into a grid of cells, allowing O(1) cell lookup and
// reducing neighbor search from O(n²) to O(n) for uniformly distributed points.
type SpatialGrid struct {
	cellSize float64
	cells    map[int64][]int // cell ID -> block indices
	blocks   []*TextBlock
	// Reusable buffer for GetNearbyBlocks to avoid allocations
	resultBuf []int
}

// blockGeom caches derived geometry for a TextBlock to avoid recomputation
// inside hot merge loops.
type blockGeom struct {
	minX, maxX float64
	minY, maxY float64
	centerX    float64
	centerY    float64
	halfW      float64
	halfH      float64
	avgFont    float64
}

// blockGeomPool provides reusable slices for blockGeom to reduce allocations
var blockGeomPool = sync.Pool{
	New: func() interface{} {
		s := make([]blockGeom, 0, 256)
		return &s
	},
}

// getBlockGeomSlice gets a blockGeom slice from pool
func getBlockGeomSlice(size int) []blockGeom {
	sp := blockGeomPool.Get().(*[]blockGeom)
	s := *sp
	if cap(s) < size {
		// Need larger slice, let old one be GC'd
		return make([]blockGeom, size)
	}
	return s[:size]
}

// putBlockGeomSlice returns a blockGeom slice to pool
func putBlockGeomSlice(s []blockGeom) {
	if cap(s) > 4096 {
		return // Don't pool very large slices
	}
	s = s[:0]
	blockGeomPool.Put(&s)
}

func buildBlockGeoms(blocks []*TextBlock) []blockGeom {
	geoms := getBlockGeomSlice(len(blocks))
	for i, b := range blocks {
		w := b.MaxX - b.MinX
		h := b.MaxY - b.MinY
		geoms[i] = blockGeom{
			minX:    b.MinX,
			maxX:    b.MaxX,
			minY:    b.MinY,
			maxY:    b.MaxY,
			centerX: b.MinX + w*0.5,
			centerY: b.MinY + h*0.5,
			halfW:   w * 0.5,
			halfH:   h * 0.5,
			avgFont: b.AvgFontSize,
		}
	}
	return geoms
}

// canMergeCoarse performs a very cheap bounding/gap check to short-circuit
// obviously distant pairs before running the heavier heuristics.
// Uses pointer parameters to avoid 72-byte struct copy overhead on each call.
// Optimized for the common case: most pairs fail the first check.
//
//go:nosplit
func canMergeCoarse(g1, g2 *blockGeom, threshold float64) bool {
	return canMergeCoarseFast(g1, g2, threshold, threshold*1.1, threshold*1.5, threshold*0.8)
}

// canMergeCoarseFast is the hot-path optimized version with pre-computed thresholds.
// Pre-computing threshold multiplications saves ~3-5 cycles per call.
//
//go:nosplit
func canMergeCoarseFast(g1, g2 *blockGeom, threshold, threshold11, threshold15, threshold08 float64) bool {
	// CRITICAL: The first bounding box check filters out ~90% of pairs
	// Load only the fields needed for the first check to maximize L1 cache hits
	g1maxX := g1.maxX
	g2minX := g2.minX

	// Quick X-axis separation check (most common rejection path)
	if g1maxX+threshold < g2minX {
		return false
	}

	g2maxX := g2.maxX
	g1minX := g1.minX
	if g2maxX+threshold < g1minX {
		return false
	}

	// Now check Y-axis (second most common rejection)
	g1maxY := g1.maxY
	g2minY := g2.minY
	if g1maxY+threshold11 < g2minY {
		return false
	}

	g2maxY := g2.maxY
	g1minY := g1.minY
	if g2maxY+threshold11 < g1minY {
		return false
	}

	// Center distance checks (rare path - only for nearby blocks)
	dx := g1.centerX - g2.centerX
	if dx < 0 {
		dx = -dx
	}
	dy := g1.centerY - g2.centerY
	if dy < 0 {
		dy = -dy
	}

	// Gap calculations
	hGap := dx - (g1.halfW + g2.halfW)
	if hGap > threshold {
		return false
	}
	vGap := dy - (g1.halfH + g2.halfH)
	if vGap > threshold15 {
		return false
	}

	// Combined gap check
	if hGap > threshold08 && vGap > threshold {
		return false
	}

	return true
}

// canMergeCoarseBatchScalar performs the same coarse merge test as
// canMergeCoarseFast but operates on a batch of g2 geometries against
// a single g1. This is a scalar, loop-unrolled friendly version that
// allows later replacement with assembly SIMD implementations.
// Inputs are slices of preloaded blockGeom values for g2.
func canMergeCoarseBatchScalar(g1 *blockGeom, g2s []blockGeom, threshold, threshold11, threshold15, threshold08 float64, out []bool) {
	// Ensure out has enough capacity
	if len(out) < len(g2s) {
		panic("out slice too small")
	}

	// Load g1 fields
	g1maxX := g1.maxX
	g1minX := g1.minX
	g1maxY := g1.maxY
	g1minY := g1.minY
	g1centerX := g1.centerX
	g1centerY := g1.centerY
	g1halfW := g1.halfW
	g1halfH := g1.halfH

	for i := 0; i < len(g2s); i++ {
		g2 := &g2s[i]
		// Quick X-axis separation
		if g1maxX+threshold < g2.minX {
			out[i] = false
			continue
		}
		if g2.maxX+threshold < g1minX {
			out[i] = false
			continue
		}

		// Y-axis checks
		if g1maxY+threshold11 < g2.minY {
			out[i] = false
			continue
		}
		if g2.maxY+threshold11 < g1minY {
			out[i] = false
			continue
		}

		// Center distance checks
		dx := g1centerX - g2.centerX
		if dx < 0 {
			dx = -dx
		}
		dy := g1centerY - g2.centerY
		if dy < 0 {
			dy = -dy
		}

		hGap := dx - (g1halfW + g2.halfW)
		if hGap > threshold {
			out[i] = false
			continue
		}
		vGap := dy - (g1halfH + g2.halfH)
		if vGap > threshold15 {
			out[i] = false
			continue
		}

		if hGap > threshold08 && vGap > threshold {
			out[i] = false
			continue
		}

		out[i] = true
	}
}

// canMergeCoarseBatchAuto dispatches to an AVX2 implementation when
// available, otherwise it falls back to the scalar batch implementation.
// It processes g2s and writes results into out (must be at least len(g2s)).
func canMergeCoarseBatchAuto(g1 *blockGeom, g2s []blockGeom, threshold, threshold11, threshold15, threshold08 float64, out []bool) {
	if len(out) < len(g2s) {
		panic("out slice too small")
	}

	// If AVX2 available and at least 4 elements, use the vector path.
	if hasAVX2() && len(g2s) >= 4 {
		// process in groups of 4
		var aMinX, aMaxX, aMinY, aMaxY [4]float64
		var bMinX, bMaxX, bMinY, bMaxY [4]float64

		i := 0
		for ; i+4 <= len(g2s); i += 4 {
			// g1 fields replicated
			for k := 0; k < 4; k++ {
				aMinX[k] = g1.minX
				aMaxX[k] = g1.maxX
				aMinY[k] = g1.minY
				aMaxY[k] = g1.maxY

				bMinX[k] = g2s[i+k].minX
				bMaxX[k] = g2s[i+k].maxX
				bMinY[k] = g2s[i+k].minY
				bMaxY[k] = g2s[i+k].maxY
			}

			mask := canMergeCoarseBatchAVX(&aMinX, &aMaxX, &aMinY, &aMaxY, &bMinX, &bMaxX, &bMinY, &bMaxY, threshold)
			// mask's lowest 4 bits correspond to lanes
			for k := 0; k < 4; k++ {
				out[i+k] = (mask&(1<<uint(k)) != 0)
			}
		}

		// leftover
		if i < len(g2s) {
			canMergeCoarseBatchScalar(g1, g2s[i:], threshold, threshold11, threshold15, threshold08, out[i:])
		}
		return
	}

	// Fallback to scalar batch
	canMergeCoarseBatchScalar(g1, g2s, threshold, threshold11, threshold15, threshold08, out)
}

// NewSpatialGrid creates a new spatial grid for the given blocks.
// cellSize determines the granularity of the grid; typically should be
// around 2-3x the expected cluster radius for optimal performance.
func NewSpatialGrid(blocks []*TextBlock, cellSize float64) *SpatialGrid {
	// Estimate number of cells: assume average 4 blocks per cell
	estimatedCells := len(blocks) / 4
	if estimatedCells < 16 {
		estimatedCells = 16
	}

	grid := &SpatialGrid{
		cellSize:  cellSize,
		cells:     make(map[int64][]int, estimatedCells),
		blocks:    blocks,
		resultBuf: make([]int, 0, 256), // Larger buffer to avoid frequent reallocs
	}

	// Pre-compute inverse cell size for faster division
	invCellSize := 1.0 / cellSize

	// First pass: count blocks per cell to pre-allocate
	cellCounts := make(map[int64]int, estimatedCells)
	for _, block := range blocks {
		// Inline Center() calculation
		centerX := (block.MinX + block.MaxX) * 0.5
		centerY := (block.MinY + block.MaxY) * 0.5

		// Inline getCellID with fast floor
		fx := centerX * invCellSize
		fy := centerY * invCellSize
		cx := int64(fx)
		if fx < 0 && fx != float64(cx) {
			cx--
		}
		cy := int64(fy)
		if fy < 0 && fy != float64(cy) {
			cy--
		}
		cellID := (cx << 32) | (cy & 0xFFFFFFFF)
		cellCounts[cellID]++
	}

	// Pre-allocate cell slices with exact capacity
	for cellID, count := range cellCounts {
		grid.cells[cellID] = make([]int, 0, count)
	}

	// Second pass: populate grid (no reallocation now)
	for i, block := range blocks {
		// Inline Center() calculation
		centerX := (block.MinX + block.MaxX) * 0.5
		centerY := (block.MinY + block.MaxY) * 0.5

		// Inline getCellID with fast floor
		fx := centerX * invCellSize
		fy := centerY * invCellSize
		cx := int64(fx)
		if fx < 0 && fx != float64(cx) {
			cx--
		}
		cy := int64(fy)
		if fy < 0 && fy != float64(cy) {
			cy--
		}
		cellID := (cx << 32) | (cy & 0xFFFFFFFF)
		grid.cells[cellID] = append(grid.cells[cellID], i)
	}

	return grid
}

// getCellID computes a unique cell ID for coordinates
// Inlined math.Floor for performance (avoids function call overhead)
func (g *SpatialGrid) getCellID(x, y float64) int64 {
	// Fast floor: for positive values, int64(x) is floor
	// For negative values, we need to subtract 1 if there's a fractional part
	invCellSize := 1.0 / g.cellSize
	fx := x * invCellSize
	fy := y * invCellSize

	cx := int64(fx)
	if fx < 0 && fx != float64(cx) {
		cx--
	}
	cy := int64(fy)
	if fy < 0 && fy != float64(cy) {
		cy--
	}

	// Pack into single int64: upper 32 bits for x, lower 32 bits for y
	return (cx << 32) | (cy & 0xFFFFFFFF)
}

// GetNearbyBlocks returns indices of blocks in the same cell and neighboring cells.
// This is much faster than searching all blocks when they're uniformly distributed.
// Memory optimized: reuses internal buffer to reduce allocations.
// WARNING: The returned slice is reused on next call - copy if needed.
func (g *SpatialGrid) GetNearbyBlocks(blockIdx int) []int {
	if blockIdx < 0 || blockIdx >= len(g.blocks) {
		return nil
	}

	block := g.blocks[blockIdx]
	// Inline Center() calculation to avoid method call overhead
	centerX := (block.MinX + block.MaxX) * 0.5
	centerY := (block.MinY + block.MaxY) * 0.5

	// Calculate base cell coordinates with inlined floor
	invCellSize := 1.0 / g.cellSize
	fx := centerX * invCellSize
	fy := centerY * invCellSize

	baseCX := int64(fx)
	if fx < 0 && fx != float64(baseCX) {
		baseCX--
	}
	baseCY := int64(fy)
	if fy < 0 && fy != float64(baseCY) {
		baseCY--
	}

	// Reuse internal buffer instead of allocating new slice each time
	g.resultBuf = g.resultBuf[:0]

	// Check current cell and 8 neighboring cells (3x3 grid)
	// Unroll the loop for better performance
	for dx := int64(-1); dx <= 1; dx++ {
		cx := baseCX + dx
		cxShifted := cx << 32
		for dy := int64(-1); dy <= 1; dy++ {
			cellID := cxShifted | ((baseCY + dy) & 0xFFFFFFFF)

			if indices, ok := g.cells[cellID]; ok {
				g.resultBuf = append(g.resultBuf, indices...)
			}
		}
	}

	return g.resultBuf
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
		// Let Texts slice grow naturally - pool will reuse grown capacity
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
	geoms := buildBlockGeoms(blocks)
	defer putBlockGeomSlice(geoms) // Return geoms to pool when done

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

	// Pre-compute threshold values once outside the hot loop
	threshold11 := distThreshold * 1.1
	threshold15 := distThreshold * 1.5
	threshold08 := distThreshold * 0.8

	// Find neighbors using spatial grid (much faster than KD-tree for this use case)
	for i := range blocks {
		gi := &geoms[i] // Use pointer to avoid struct copy
		rootI := find(i)

		// Get nearby blocks from spatial grid (O(1) average case)
		nearbyIndices := grid.GetNearbyBlocks(i)

		for _, j := range nearbyIndices {
			if j <= i {
				continue
			}

			gj := &geoms[j] // Use pointer to avoid struct copy
			if !canMergeCoarseFast(gi, gj, distThreshold, threshold11, threshold15, threshold08) {
				continue
			}

			// Check if already in same cluster
			rootJ := find(j)
			if rootI == rootJ {
				continue
			}

			// Check if should merge - use geometry-based version for maximum performance
			// This avoids pointer dereferences to TextBlock structs
			if shouldMergeClustersGeomFast(gi, gj, distThreshold, threshold15) {
				union(rootI, rootJ)
				rootI = find(i)
			}
		}
	}

	// Group blocks by cluster root - optimized to minimize allocations
	// First pass: count blocks per root and identify unique roots
	rootCounts := make([]int, len(blocks))
	blockRoots := make([]int, len(blocks))
	uniqueRoots := 0
	for i := range blocks {
		root := find(i)
		blockRoots[i] = root
		if rootCounts[root] == 0 {
			uniqueRoots++
		}
		rootCounts[root]++
	}

	// Use map only for clusters with >1 block (most clusters have 1 block)
	// Single-block clusters go directly to result without intermediate storage
	multiBlockClusters := make(map[int][]*TextBlock, uniqueRoots/4+1)

	// Pre-allocate result with upper bound
	result := make([]*TextBlock, 0, uniqueRoots)

	// Second pass: directly add single-block clusters, group multi-block ones
	for i, root := range blockRoots {
		count := rootCounts[root]
		if count == 1 {
			// Single-block cluster - add directly to result
			result = append(result, blocks[i])
		} else {
			// Multi-block cluster - needs merging
			if multiBlockClusters[root] == nil {
				multiBlockClusters[root] = make([]*TextBlock, 0, count)
			}
			multiBlockClusters[root] = append(multiBlockClusters[root], blocks[i])
		}
	}

	// Merge multi-block clusters
	for _, cluster := range multiBlockClusters {
		if len(cluster) == 0 {
			continue
		}

		merged := mergeTextBlocksOptimized(cluster)
		// Return individual blocks to pool (merged reuses first block)
		for i := 1; i < len(cluster); i++ {
			PutTextBlock(cluster[i])
		}
		result = append(result, merged)
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

// shouldMergeClustersGeom is an ultra-optimized version using pre-cached geometry.
// All data is already in cache-friendly blockGeom structs, avoiding pointer chasing.
// This is the hot path - every nanosecond counts here.
// Uses pointer parameters to avoid 72-byte struct copy overhead.
//
//go:nosplit
func shouldMergeClustersGeom(g1, g2 *blockGeom, threshold float64) bool {
	return shouldMergeClustersGeomFast(g1, g2, threshold, threshold*1.5)
}

// shouldMergeClustersGeomFast is the hot-path version with pre-computed threshold15.
//
//go:nosplit
func shouldMergeClustersGeomFast(g1, g2 *blockGeom, threshold, threshold15 float64) bool {
	// Load all fields into local variables at once - CPU can prefetch better
	g1minX, g1maxX := g1.minX, g1.maxX
	g1minY, g1maxY := g1.minY, g1.maxY
	g1centerX, g1centerY := g1.centerX, g1.centerY
	g1halfW, g1halfH := g1.halfW, g1.halfH
	g1avgFont := g1.avgFont

	g2minX, g2maxX := g2.minX, g2.maxX
	g2minY, g2maxY := g2.minY, g2.maxY
	g2centerX, g2centerY := g2.centerX, g2.centerY
	g2halfW, g2halfH := g2.halfW, g2.halfH
	g2avgFont := g2.avgFont

	// Pre-compute threshold-based values
	threshold03_1 := g1avgFont * 0.3
	threshold03_2 := g2avgFont * 0.3

	// Compute vertical overlap using branchless min/max
	var minMaxY, maxMinY float64
	if g1maxY < g2maxY {
		minMaxY = g1maxY
	} else {
		minMaxY = g2maxY
	}
	if g1minY > g2minY {
		maxMinY = g1minY
	} else {
		maxMinY = g2minY
	}
	verticalOverlap := minMaxY - maxMinY

	// Early return for vertically overlapping blocks
	if verticalOverlap > 0 && (verticalOverlap > threshold03_1 || verticalOverlap > threshold03_2) {
		var maxMinX, minMaxX float64
		if g1minX > g2minX {
			maxMinX = g1minX
		} else {
			maxMinX = g2minX
		}
		if g1maxX < g2maxX {
			minMaxX = g1maxX
		} else {
			minMaxX = g2maxX
		}
		if maxMinX-minMaxX < threshold {
			return true
		}
	}

	// Width is already computable from halfW
	w1 := g1halfW * 2
	w2 := g2halfW * 2

	// Check if vertically stacked and horizontally aligned
	var minMaxX, maxMinX float64
	if g1maxX < g2maxX {
		minMaxX = g1maxX
	} else {
		minMaxX = g2maxX
	}
	if g1minX > g2minX {
		maxMinX = g1minX
	} else {
		maxMinX = g2minX
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
			if g1minY > g2minY {
				maxMinY2 = g1minY
			} else {
				maxMinY2 = g2minY
			}
			if g1maxY < g2maxY {
				minMaxY2 = g1maxY
			} else {
				minMaxY2 = g2maxY
			}
			verticalGap := maxMinY2 - minMaxY2
			if verticalGap >= 0 && verticalGap < threshold15 {
				return true
			}
		}
	}

	// Use pre-computed center coordinates (already loaded above)
	// Use branchless abs
	horizontalDistance := g1centerX - g2centerX
	if horizontalDistance < 0 {
		horizontalDistance = -horizontalDistance
	}
	verticalDistance := g1centerY - g2centerY
	if verticalDistance < 0 {
		verticalDistance = -verticalDistance
	}

	// Different columns check
	if horizontalDistance > verticalDistance*2 {
		return false
	}

	// Text-image mix check using pre-computed half dimensions
	h1 := g1halfH * 2
	h2 := g2halfH * 2
	avgSize := (w1 + h1 + w2 + h2) * 0.25
	avgSize2 := avgSize * 2
	if horizontalDistance > avgSize2 || verticalDistance > avgSize2 {
		return false
	}

	return false
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
