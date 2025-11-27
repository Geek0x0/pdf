package pdf

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// ClusterTextBlocksParallel delegates to ParallelV2 for large inputs.
// This is the main entry point for parallel clustering.
func ClusterTextBlocksParallel(texts []Text) []*TextBlock {
	n := len(texts)
	if n == 0 {
		return nil
	}
	if n < 500 {
		return ClusterTextBlocksV3(texts)
	}
	return ClusterTextBlocksParallelV2(texts)
}

// ClusterTextBlocksV4 automatically selects the best algorithm based on input size.
func ClusterTextBlocksV4(texts []Text) []*TextBlock {
	n := len(texts)
	switch {
	case n == 0:
		return nil
	case n < 50:
		return ClusterTextBlocksOptimizedV2(texts)
	case n < 500:
		return ClusterTextBlocksV3(texts)
	default:
		return ClusterTextBlocksParallelV2(texts)
	}
}

// parallelUnionFind is a lock-free union-find structure for parallel clustering.
// Uses compare-and-swap for thread-safe operations without locks.
type parallelUnionFind struct {
	parent []int32
}

func newParallelUnionFind(n int) *parallelUnionFind {
	uf := &parallelUnionFind{
		parent: make([]int32, n),
	}
	for i := range uf.parent {
		uf.parent[i] = int32(i)
	}
	return uf
}

// Find with path compression using atomic operations (lock-free)
// Optimized to minimize CAS operations by finding root first without compression,
// then doing a single compression pass.
func (uf *parallelUnionFind) Find(x int) int {
	// Phase 1: Find root without any writes (fast path)
	root := int32(x)
	for {
		p := atomic.LoadInt32(&uf.parent[root])
		if p == root {
			break
		}
		root = p
	}

	// Phase 2: Path compression - only compress if path is long (>2 hops)
	if int32(x) != root && atomic.LoadInt32(&uf.parent[x]) != root {
		curr := int32(x)
		for curr != root {
			next := atomic.LoadInt32(&uf.parent[curr])
			if next == root {
				break
			}
			// Best-effort compression - don't spin on failure
			atomic.CompareAndSwapInt32(&uf.parent[curr], next, root)
			curr = next
		}
	}

	return int(root)
}

// Union links two sets together using CAS for thread safety
func (uf *parallelUnionFind) Union(x, y int) bool {
	for {
		px, py := int32(uf.Find(x)), int32(uf.Find(y))
		if px == py {
			return false
		}
		// Always make smaller point to larger for consistency
		if px > py {
			px, py = py, px
		}
		if atomic.CompareAndSwapInt32(&uf.parent[px], px, py) {
			return true
		}
		// CAS failed, retry with fresh roots
	}
}

// ClusterTextBlocksParallelV2 uses a work-partitioning strategy for parallel clustering.
// Each worker processes a chunk of blocks independently with local edge collection,
// then edges are merged sequentially. This avoids all lock contention.
func ClusterTextBlocksParallelV2(texts []Text) []*TextBlock {
	n := len(texts)
	if n == 0 {
		return nil
	}
	if n < 1000 {
		return ClusterTextBlocksV3(texts)
	}

	// Calculate threshold
	var totalFontSize float64
	for i := range texts {
		totalFontSize += texts[i].FontSize
	}
	avgFontSize := totalFontSize / float64(n)
	eps := avgFontSize * 2.0

	// Initialize blocks - optimize memory allocation
	blocks := make([]*TextBlock, n)
	for i := range texts {
		t := &texts[i]
		tb := GetTextBlock()
		// Pre-allocate with capacity 1 to avoid append allocation
		// The pool will reuse this capacity across calls
		if cap(tb.Texts) < 1 {
			tb.Texts = make([]Text, 1, 4) // Start with small capacity
		} else {
			tb.Texts = tb.Texts[:1]
		}
		tb.Texts[0] = *t
		tb.MinX = t.X
		tb.MaxX = t.X + t.W
		tb.MinY = t.Y
		tb.MaxY = t.Y + t.FontSize
		tb.AvgFontSize = t.FontSize
		blocks[i] = tb
	}

	// Build spatial grid and geometry cache
	grid := NewSpatialGrid(blocks, eps*2.0)
	geoms := buildBlockGeoms(blocks)
	defer putBlockGeomSlice(geoms)

	// Pre-compute thresholds
	eps11 := eps * 1.1
	eps15 := eps * 1.5
	eps08 := eps * 0.8

	// Phase 1: Parallel edge discovery with local collection (no channels)
	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers > 16 {
		numWorkers = 16
	}

	chunkSize := (n + numWorkers - 1) / numWorkers

	// Each worker stores its edges in a local slice (no synchronization)
	type edgePair struct{ i, j int32 }
	edgeSlices := make([][]edgePair, numWorkers)

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for w := 0; w < numWorkers; w++ {
		go func(workerID int) {
			defer wg.Done()

			start := workerID * chunkSize
			end := start + chunkSize
			if end > n {
				end = n
			}
			if start >= n {
				return
			}

			// Pre-allocate local edge buffer (estimate: each block has ~2 edges on average)
			localEdges := make([]edgePair, 0, (end-start)*2)

			// Each worker needs its own result buffer for grid queries
			localResultBuf := make([]int, 0, 256)

			// Reusable batch buffers to avoid allocations inside hot loop
			batchIdx := make([]int, 0, 16)
			batchGeoms := make([]blockGeom, 0, 16)
			out := make([]bool, 16)

			for i := start; i < end; i++ {
				gi := &geoms[i]

				// Inline GetNearbyBlocks to avoid shared state
				localResultBuf = localResultBuf[:0]
				minCX := int((gi.minX - grid.cellSize) / grid.cellSize)
				maxCX := int((gi.maxX + grid.cellSize) / grid.cellSize)
				minCY := int((gi.minY - grid.cellSize) / grid.cellSize)
				maxCY := int((gi.maxY + grid.cellSize) / grid.cellSize)

				for cy := minCY; cy <= maxCY; cy++ {
					for cx := minCX; cx <= maxCX; cx++ {
						key := int64(cx)<<32 | int64(uint32(cy))
						if cell, ok := grid.cells[key]; ok {
							localResultBuf = append(localResultBuf, cell...)
						}
					}
				}

				// collect up to 16 neighbors into batchIdx
				batchIdx = batchIdx[:0]
				for _, jj := range localResultBuf {
					if jj <= i {
						continue
					}
					batchIdx = append(batchIdx, jj)
					if len(batchIdx) >= cap(batchIdx) {
						break
					}
				}

				if len(batchIdx) == 0 {
					continue
				}

				// prepare geoms batch (resize preserving capacity)
				batchGeoms = batchGeoms[:len(batchIdx)]
				for k := range batchIdx {
					batchGeoms[k] = geoms[batchIdx[k]]
				}

				// ensure out slice length equals batch
				out = out[:len(batchGeoms)]

				// coarse filter using AVX2 or scalar batch
				canMergeCoarseBatchAuto(gi, batchGeoms, eps, eps11, eps15, eps08, out)

				for k, ok := range out {
					if !ok {
						continue
					}
					j := batchIdx[k]
					gj := &geoms[j]
					if shouldMergeClustersGeomFast(gi, gj, eps, eps15) {
						localEdges = append(localEdges, edgePair{int32(i), int32(j)})
					}
				}
			}
			edgeSlices[workerID] = localEdges
		}(w)
	}

	wg.Wait()

	// Phase 2: Build union-find sequentially from collected edges (very fast, no contention)
	parent := make([]int32, n)
	for i := range parent {
		parent[i] = int32(i)
	}

	// Iterative find with path compression
	find := func(x int32) int32 {
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

	// Process all edges from all workers
	for w := 0; w < numWorkers; w++ {
		for _, edge := range edgeSlices[w] {
			px, py := find(edge.i), find(edge.j)
			if px != py {
				parent[px] = py
			}
		}
	}

	// Group blocks by cluster
	rootCounts := make(map[int32]int, n/2)
	for i := 0; i < n; i++ {
		root := find(int32(i))
		rootCounts[root]++
	}

	clusters := make(map[int32][]*TextBlock, len(rootCounts))
	for i := 0; i < n; i++ {
		root := find(int32(i))
		if clusters[root] == nil {
			clusters[root] = make([]*TextBlock, 0, rootCounts[root])
		}
		clusters[root] = append(clusters[root], blocks[i])
	}

	// Build result
	result := make([]*TextBlock, 0, len(clusters))
	for _, cluster := range clusters {
		if len(cluster) == 0 {
			continue
		}
		if len(cluster) == 1 {
			result = append(result, cluster[0])
			continue
		}

		merged := mergeTextBlocksOptimized(cluster)
		for i := 1; i < len(cluster); i++ {
			PutTextBlock(cluster[i])
		}
		result = append(result, merged)
	}

	return result
}
