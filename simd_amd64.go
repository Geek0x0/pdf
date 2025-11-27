//go:build amd64
// +build amd64

package pdf

// SIMD-optimized batch operations for clustering.
// Uses AVX2 instructions for parallel float64 comparisons.

// canMergeCoarseBatchAVX checks multiple block pairs simultaneously using AVX2.
// Returns a bitmask where bit i is set if pair i can merge.
// Requires AVX2 support.
//
// Parameters are arrays of 4 pairs (AVX2 processes 4 float64s at once):
//
//	g1MinX, g1MaxX, g1MinY, g1MaxY: bounds of first block in each pair
//	g2MinX, g2MaxX, g2MinY, g2MaxY: bounds of second block in each pair
//	threshold: distance threshold (same for all pairs)
//
//go:noescape
func canMergeCoarseBatchAVX(
	g1MinX, g1MaxX, g1MinY, g1MaxY *[4]float64,
	g2MinX, g2MaxX, g2MinY, g2MaxY *[4]float64,
	threshold float64,
) uint8

// Internal batch processing wrapper
func canMergeCoarseBatch4(
	geoms []blockGeom,
	indices [][2]int, // pairs of indices to check
	threshold, threshold11 float64,
) uint8 {
	if len(indices) != 4 {
		return 0
	}

	var g1MinX, g1MaxX, g1MinY, g1MaxY [4]float64
	var g2MinX, g2MaxX, g2MinY, g2MaxY [4]float64

	for k := 0; k < 4; k++ {
		i, j := indices[k][0], indices[k][1]
		g1 := &geoms[i]
		g2 := &geoms[j]

		g1MinX[k] = g1.minX
		g1MaxX[k] = g1.maxX
		g1MinY[k] = g1.minY
		g1MaxY[k] = g1.maxY

		g2MinX[k] = g2.minX
		g2MaxX[k] = g2.maxX
		g2MinY[k] = g2.minY
		g2MaxY[k] = g2.maxY
	}

	return canMergeCoarseBatchAVX(
		&g1MinX, &g1MaxX, &g1MinY, &g1MaxY,
		&g2MinX, &g2MaxX, &g2MinY, &g2MaxY,
		threshold,
	)
}
