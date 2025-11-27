// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"runtime"
	"sync"
)

// ClusterTextBlocksUltraOptimized - 极致性能优化版本
// 目标：最小化内存分配和GC压力，同时保持并行性能
func ClusterTextBlocksUltraOptimized(texts []Text) []*TextBlock {
	n := len(texts)
	if n == 0 {
		return nil
	}

	// 小数据集使用简单算法避免并行开销
	if n < 1000 {
		return ClusterTextBlocksV3(texts)
	}

	// 计算阈值 - 使用快速求和
	var totalFontSize float64
	for i := range texts {
		totalFontSize += texts[i].FontSize
	}
	avgFontSize := totalFontSize / float64(n)
	eps := avgFontSize * 2.0

	// 初始化 blocks - 优化：直接设置值而不是 append
	blocks := make([]*TextBlock, n)
	for i := range texts {
		t := &texts[i]
		tb := GetTextBlock()

		// 优化：直接设置 slice 长度避免 append
		if cap(tb.Texts) == 0 {
			tb.Texts = make([]Text, 1, 8) // 小的初始容量
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

	// 构建空间网格和几何缓存
	grid := NewSpatialGrid(blocks, eps*2.0)
	geoms := buildBlockGeoms(blocks)
	defer putBlockGeomSlice(geoms)

	// 预计算阈值
	eps11 := eps * 1.1
	eps15 := eps * 1.5
	eps08 := eps * 0.8

	// 阶段1：并行边发现 - 使用本地收集避免锁竞争
	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers > 16 {
		numWorkers = 16
	}

	chunkSize := (n + numWorkers - 1) / numWorkers

	// 每个 worker 在本地 slice 中存储边（无同步）
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

			// 预分配本地边缓冲区
			localEdges := make([]edgePair, 0, (end-start)*2)

			// 每个 worker 需要自己的结果缓冲区用于网格查询
			localResultBuf := make([]int, 0, 256)

			// 可重用的批处理缓冲区避免在热循环内分配
			batchIdx := make([]int, 0, 16)
			batchGeoms := make([]blockGeom, 0, 16)
			out := make([]bool, 16)

			for i := start; i < end; i++ {
				gi := &geoms[i]

				// 内联 GetNearbyBlocks 避免共享状态
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

				// 收集最多16个邻居到 batchIdx
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

				// 准备几何批次（调整大小保留容量）
				batchGeoms = batchGeoms[:len(batchIdx)]
				for k := range batchIdx {
					batchGeoms[k] = geoms[batchIdx[k]]
				}

				// 确保 out slice 长度等于批次
				out = out[:len(batchGeoms)]

				// 使用 AVX2 或标量批次进行粗过滤
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

	// 阶段2：从收集的边顺序构建并查集（非常快，无竞争）
	parent := make([]int32, n)
	for i := range parent {
		parent[i] = int32(i)
	}

	// Union-find with path compression
	var find func(int32) int32
	find = func(x int32) int32 {
		if parent[x] != x {
			parent[x] = find(parent[x]) // 路径压缩
		}
		return parent[x]
	}

	union := func(x, y int32) {
		px, py := find(x), find(y)
		if px != py {
			parent[px] = py
		}
	}

	// 应用所有边
	for _, edges := range edgeSlices {
		for _, e := range edges {
			union(e.i, e.j)
		}
	}

	// 阶段3：合并属于同一集群的块
	clusterMap := make(map[int32]*TextBlock)
	for i := range blocks {
		root := find(int32(i))
		if merged, exists := clusterMap[root]; exists {
			// 合并到现有块 - 优化：直接操作 slice
			oldLen := len(merged.Texts)
			newLen := oldLen + len(blocks[i].Texts)

			// 确保容量足够
			if cap(merged.Texts) < newLen {
				// 扩容策略：增加25%额外容量
				newCap := newLen + newLen/4
				newTexts := make([]Text, newLen, newCap)
				copy(newTexts, merged.Texts)
				merged.Texts = newTexts
			} else {
				merged.Texts = merged.Texts[:newLen]
			}

			// 复制新的文本
			copy(merged.Texts[oldLen:], blocks[i].Texts)

			// 更新边界
			if blocks[i].MinX < merged.MinX {
				merged.MinX = blocks[i].MinX
			}
			if blocks[i].MaxX > merged.MaxX {
				merged.MaxX = blocks[i].MaxX
			}
			if blocks[i].MinY < merged.MinY {
				merged.MinY = blocks[i].MinY
			}
			if blocks[i].MaxY > merged.MaxY {
				merged.MaxY = blocks[i].MaxY
			}

			// 更新平均字体大小
			totalSize := merged.AvgFontSize*float64(oldLen) + blocks[i].AvgFontSize*float64(len(blocks[i].Texts))
			merged.AvgFontSize = totalSize / float64(newLen)

			// 归还未使用的块到池
			PutTextBlock(blocks[i])
		} else {
			clusterMap[root] = blocks[i]
		}
	}

	// 转换为结果 slice
	result := make([]*TextBlock, 0, len(clusterMap))
	for _, block := range clusterMap {
		result = append(result, block)
	}

	return result
}
