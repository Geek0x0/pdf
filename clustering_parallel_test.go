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
