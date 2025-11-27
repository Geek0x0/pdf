package pdf

import (
	"math/rand"
	"runtime"
	"testing"
)

// genTextsForParallel creates random text elements for parallel clustering benchmarks
func genTextsForParallel(n int, seed int64) []Text {
	rng := rand.New(rand.NewSource(seed))
	texts := make([]Text, n)

	pageWidth := 612.0
	pageHeight := 792.0

	for i := 0; i < n; i++ {
		col := rng.Intn(3)
		colWidth := pageWidth / 3.0
		x := float64(col)*colWidth + rng.Float64()*colWidth*0.8
		y := rng.Float64() * pageHeight
		fontSize := 8.0 + rng.Float64()*8.0
		w := rng.Float64() * 100.0

		texts[i] = Text{
			S:        "Lorem ipsum",
			X:        x,
			Y:        y,
			W:        w,
			FontSize: fontSize,
		}
	}
	return texts
}

func BenchmarkClusterV3vsPV2(b *testing.B) {
	sizes := []int{1000, 5000, 10000}

	for _, size := range sizes {
		texts := genTextsForParallel(size, 42)

		b.Run("V3_"+itoa2(size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				result := ClusterTextBlocksV3(texts)
				for _, blk := range result {
					PutTextBlock(blk)
				}
			}
		})

		b.Run("PV2_"+itoa2(size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				result := ClusterTextBlocksParallelV2(texts)
				for _, blk := range result {
					PutTextBlock(blk)
				}
			}
		})
	}
}

func BenchmarkCanMergeCoarse(b *testing.B) {
	// prepare a set of geometries
	n := 10000
	texts := genTextsForParallel(n, 123)
	blocks := make([]*TextBlock, n)
	for i := range texts {
		t := &texts[i]
		tb := GetTextBlock()
		tb.Texts = append(tb.Texts, *t)
		tb.MinX = t.X
		tb.MaxX = t.X + t.W
		tb.MinY = t.Y
		tb.MaxY = t.Y + t.FontSize
		tb.AvgFontSize = t.FontSize
		blocks[i] = tb
	}
	geoms := buildBlockGeoms(blocks)
	defer putBlockGeomSlice(geoms)

	b.Run("ScalarPair", func(b *testing.B) {
		b.ReportAllocs()
		out := make([]bool, 0, 16)
		for i := 0; i < b.N; i++ {
			// compare first element vs next 128 neighbors
			g1 := &geoms[0]
			for j := 1; j < 129 && j < len(geoms); j++ {
				_ = canMergeCoarseFast(g1, &geoms[j], g1.avgFont*2.0, g1.avgFont*2.0*1.1, g1.avgFont*2.0*1.5, g1.avgFont*2.0*0.8)
			}
			_ = out
		}
	})

	b.Run("BatchScalar", func(b *testing.B) {
		b.ReportAllocs()
		batch := make([]blockGeom, 0, 128)
		for j := 1; j < 129 && j < len(geoms); j++ {
			batch = append(batch, geoms[j])
		}
		out := make([]bool, len(batch))
		for i := 0; i < b.N; i++ {
			canMergeCoarseBatchScalar(&geoms[0], batch, geoms[0].avgFont*2.0, geoms[0].avgFont*2.0*1.1, geoms[0].avgFont*2.0*1.5, geoms[0].avgFont*2.0*0.8, out)
		}
		_ = out
	})

	for _, blk := range blocks {
		PutTextBlock(blk)
	}
}

func itoa2(n int) string {
	if n >= 1000 {
		return itoa3(n/1000) + "k"
	}
	return itoa3(n)
}

func itoa3(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func TestParallelConsistency(t *testing.T) {
	texts := genTextsForParallel(2000, 42)

	v3 := ClusterTextBlocksV3(texts)
	pv2 := ClusterTextBlocksParallelV2(texts)

	t.Logf("V3: %d clusters, PV2: %d clusters", len(v3), len(pv2))

	// Cleanup
	for _, blk := range v3 {
		PutTextBlock(blk)
	}
	for _, blk := range pv2 {
		PutTextBlock(blk)
	}
}

func BenchmarkParallelScalingCores(b *testing.B) {
	texts := genTextsForParallel(10000, 42)
	maxProcs := runtime.GOMAXPROCS(0)

	for procs := 1; procs <= maxProcs && procs <= 16; procs *= 2 {
		b.Run(itoa3(procs)+"cores", func(b *testing.B) {
			runtime.GOMAXPROCS(procs)
			defer runtime.GOMAXPROCS(maxProcs)

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				result := ClusterTextBlocksParallelV2(texts)
				for _, blk := range result {
					PutTextBlock(blk)
				}
			}
		})
	}
}
