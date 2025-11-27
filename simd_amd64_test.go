//go:build amd64
// +build amd64

package pdf

import (
	"testing"
)

func TestHasAVX2(t *testing.T) {
	result := hasAVX2()
	t.Logf("AVX2 support: %v", result)
}

func TestCanMergeCoarseBatchAVX(t *testing.T) {
	if !hasAVX2() {
		t.Skip("AVX2 not supported")
	}

	// Create test data: 4 pairs of blocks
	// Pair 0: overlapping - should pass
	// Pair 1: far apart X - should fail
	// Pair 2: far apart Y - should fail
	// Pair 3: close enough - should pass

	g1MinX := [4]float64{0, 0, 0, 10}
	g1MaxX := [4]float64{10, 10, 10, 20}
	g1MinY := [4]float64{0, 0, 0, 10}
	g1MaxY := [4]float64{10, 10, 10, 20}

	g2MinX := [4]float64{5, 100, 5, 15} // pair 1: X far
	g2MaxX := [4]float64{15, 110, 15, 25}
	g2MinY := [4]float64{5, 5, 100, 15} // pair 2: Y far
	g2MaxY := [4]float64{15, 15, 110, 25}

	threshold := 5.0

	result := canMergeCoarseBatchAVX(
		&g1MinX, &g1MaxX, &g1MinY, &g1MaxY,
		&g2MinX, &g2MaxX, &g2MinY, &g2MaxY,
		threshold,
	)

	t.Logf("Result bitmask: %04b", result)

	// Expected: bit 0 = 1 (pass), bit 1 = 0 (fail), bit 2 = 0 (fail), bit 3 = 1 (pass)
	// So result should be 0b1001 = 9
	expected := uint8(0b1001)
	if result != expected {
		t.Errorf("Expected %04b, got %04b", expected, result)
	}
}

func TestCanMergeCoarseBatchConsistency(t *testing.T) {
	if !hasAVX2() {
		t.Skip("AVX2 not supported")
	}

	// Test that SIMD version matches scalar version for first bounding box checks
	geoms := []blockGeom{
		{minX: 0, maxX: 10, minY: 0, maxY: 10, centerX: 5, centerY: 5, halfW: 5, halfH: 5, avgFont: 12},
		{minX: 8, maxX: 18, minY: 0, maxY: 10, centerX: 13, centerY: 5, halfW: 5, halfH: 5, avgFont: 12},
		{minX: 100, maxX: 110, minY: 0, maxY: 10, centerX: 105, centerY: 5, halfW: 5, halfH: 5, avgFont: 12},
		{minX: 0, maxX: 10, minY: 100, maxY: 110, centerX: 5, centerY: 105, halfW: 5, halfH: 5, avgFont: 12},
		{minX: 15, maxX: 25, minY: 15, maxY: 25, centerX: 20, centerY: 20, halfW: 5, halfH: 5, avgFont: 12},
	}

	threshold := 5.0
	threshold11 := threshold * 1.1
	threshold15 := threshold * 1.5
	threshold08 := threshold * 0.8

	// Test pairs: (0,1), (0,2), (0,3), (0,4)
	indices := [][2]int{{0, 1}, {0, 2}, {0, 3}, {0, 4}}

	simdResult := canMergeCoarseBatch4(geoms, indices, threshold, threshold11)

	// Compare with scalar version
	for k := 0; k < 4; k++ {
		i, j := indices[k][0], indices[k][1]
		scalarResult := canMergeCoarseFast(&geoms[i], &geoms[j], threshold, threshold11, threshold15, threshold08)
		simdPass := (simdResult & (1 << k)) != 0

		// Note: SIMD only does the first 4 bounding box checks, not the full canMergeCoarseFast
		// So we just check the bounding box part matches
		bbPass := boundingBoxOverlapCheck(&geoms[i], &geoms[j], threshold, threshold11)

		if simdPass != bbPass {
			t.Errorf("Pair (%d,%d): SIMD=%v, BB=%v, scalar=%v", i, j, simdPass, bbPass, scalarResult)
		}
	}
}

// boundingBoxOverlapCheck is the first part of canMergeCoarseFast
func boundingBoxOverlapCheck(g1, g2 *blockGeom, threshold, threshold11 float64) bool {
	if g1.maxX+threshold < g2.minX {
		return false
	}
	if g2.maxX+threshold < g1.minX {
		return false
	}
	if g1.maxY+threshold11 < g2.minY {
		return false
	}
	if g2.maxY+threshold11 < g1.minY {
		return false
	}
	return true
}

func BenchmarkCanMergeCoarseBatchAVX(b *testing.B) {
	if !hasAVX2() {
		b.Skip("AVX2 not supported")
	}

	g1MinX := [4]float64{0, 0, 0, 10}
	g1MaxX := [4]float64{10, 10, 10, 20}
	g1MinY := [4]float64{0, 0, 0, 10}
	g1MaxY := [4]float64{10, 10, 10, 20}

	g2MinX := [4]float64{5, 100, 5, 15}
	g2MaxX := [4]float64{15, 110, 15, 25}
	g2MinY := [4]float64{5, 5, 100, 15}
	g2MaxY := [4]float64{15, 15, 110, 25}

	threshold := 5.0

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = canMergeCoarseBatchAVX(
			&g1MinX, &g1MaxX, &g1MinY, &g1MaxY,
			&g2MinX, &g2MaxX, &g2MinY, &g2MaxY,
			threshold,
		)
	}
}

func BenchmarkCanMergeCoarseScalar4x(b *testing.B) {
	geoms := []blockGeom{
		{minX: 0, maxX: 10, minY: 0, maxY: 10, centerX: 5, centerY: 5, halfW: 5, halfH: 5, avgFont: 12},
		{minX: 5, maxX: 15, minY: 5, maxY: 15, centerX: 10, centerY: 10, halfW: 5, halfH: 5, avgFont: 12},
		{minX: 100, maxX: 110, minY: 5, maxY: 15, centerX: 105, centerY: 10, halfW: 5, halfH: 5, avgFont: 12},
		{minX: 5, maxX: 15, minY: 100, maxY: 110, centerX: 10, centerY: 105, halfW: 5, halfH: 5, avgFont: 12},
		{minX: 15, maxX: 25, minY: 15, maxY: 25, centerX: 20, centerY: 20, halfW: 5, halfH: 5, avgFont: 12},
	}

	threshold := 5.0
	threshold11 := threshold * 1.1
	threshold15 := threshold * 1.5
	threshold08 := threshold * 0.8

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = canMergeCoarseFast(&geoms[0], &geoms[1], threshold, threshold11, threshold15, threshold08)
		_ = canMergeCoarseFast(&geoms[0], &geoms[2], threshold, threshold11, threshold15, threshold08)
		_ = canMergeCoarseFast(&geoms[0], &geoms[3], threshold, threshold11, threshold15, threshold08)
		_ = canMergeCoarseFast(&geoms[0], &geoms[4], threshold, threshold11, threshold15, threshold08)
	}
}

func BenchmarkSIMDvsScalarComparison(b *testing.B) {
	if !hasAVX2() {
		b.Skip("AVX2 not supported")
	}

	// Create realistic test data
	geoms := make([]blockGeom, 100)
	for i := range geoms {
		x := float64(i%10) * 50
		y := float64(i/10) * 50
		geoms[i] = blockGeom{
			minX: x, maxX: x + 40,
			minY: y, maxY: y + 12,
			centerX: x + 20, centerY: y + 6,
			halfW: 20, halfH: 6,
			avgFont: 12,
		}
	}

	threshold := 24.0
	threshold11 := threshold * 1.1
	threshold15 := threshold * 1.5
	threshold08 := threshold * 0.8

	b.Run("Scalar", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for j := 0; j < 96; j += 4 {
				_ = canMergeCoarseFast(&geoms[j], &geoms[j+1], threshold, threshold11, threshold15, threshold08)
				_ = canMergeCoarseFast(&geoms[j], &geoms[j+2], threshold, threshold11, threshold15, threshold08)
				_ = canMergeCoarseFast(&geoms[j], &geoms[j+3], threshold, threshold11, threshold15, threshold08)
				_ = canMergeCoarseFast(&geoms[j+1], &geoms[j+2], threshold, threshold11, threshold15, threshold08)
			}
		}
	})

	b.Run("SIMD_Batch", func(b *testing.B) {
		var g1MinX, g1MaxX, g1MinY, g1MaxY [4]float64
		var g2MinX, g2MaxX, g2MinY, g2MaxY [4]float64

		for i := 0; i < b.N; i++ {
			for j := 0; j < 96; j += 4 {
				// Prepare batch
				pairs := [][2]int{{j, j + 1}, {j, j + 2}, {j, j + 3}, {j + 1, j + 2}}
				for k := 0; k < 4; k++ {
					p1, p2 := pairs[k][0], pairs[k][1]
					g1MinX[k] = geoms[p1].minX
					g1MaxX[k] = geoms[p1].maxX
					g1MinY[k] = geoms[p1].minY
					g1MaxY[k] = geoms[p1].maxY
					g2MinX[k] = geoms[p2].minX
					g2MaxX[k] = geoms[p2].maxX
					g2MinY[k] = geoms[p2].minY
					g2MaxY[k] = geoms[p2].maxY
				}
				_ = canMergeCoarseBatchAVX(&g1MinX, &g1MaxX, &g1MinY, &g1MaxY, &g2MinX, &g2MaxX, &g2MinY, &g2MaxY, threshold)
			}
		}
	})
}
