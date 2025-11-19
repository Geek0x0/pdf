// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"testing"
	"time"
)

// ==================== 自适应容量估算器测试 ====================

func TestAdaptiveCapacityEstimator(t *testing.T) {
	estimator := NewAdaptiveCapacityEstimator(10)

	// 测试无历史数据时的估算
	estimated := estimator.Estimate(100)
	if estimated < 100 {
		t.Errorf("估算值应大于提示值，got %d", estimated)
	}

	// 记录一些历史数据
	for i := 0; i < 20; i++ {
		estimator.Record(100 + i*10)
	}

	// 测试基于历史数据的估算
	estimated = estimator.Estimate(50)
	t.Logf("基于历史数据估算: %d", estimated)
}

func BenchmarkAdaptiveCapacityEstimator(b *testing.B) {
	estimator := NewAdaptiveCapacityEstimator(100)
	for i := 0; i < 50; i++ {
		estimator.Record(100 + i*5)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		estimator.Estimate(100)
	}
}

// ==================== 批量字符串构建器测试 ====================

func TestBatchStringBuilder(t *testing.T) {
	texts := []Text{
		{S: "Hello"},
		{S: "World"},
		{S: "Test"},
	}

	builder := NewBatchStringBuilder(texts)
	result := builder.AppendTexts(texts)

	if len(result) == 0 {
		t.Error("结果不应为空")
	}
	t.Logf("构建结果: %s", result)
}

func BenchmarkBatchStringBuilder(b *testing.B) {
	texts := make([]Text, 100)
	for i := range texts {
		texts[i] = Text{S: "Sample text content for testing"}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		builder := NewBatchStringBuilder(texts)
		_ = builder.AppendTexts(texts)
	}
}

func BenchmarkBatchStringBuilderVsTraditional(b *testing.B) {
	texts := make([]Text, 100)
	for i := range texts {
		texts[i] = Text{S: "Sample text "}
	}

	b.Run("BatchStringBuilder", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			builder := NewBatchStringBuilder(texts)
			_ = builder.AppendTexts(texts)
		}
	})

	b.Run("TraditionalConcat", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			result := ""
			for j := range texts {
				result += texts[j].S
				if j < len(texts)-1 {
					result += " "
				}
			}
			_ = result
		}
	})

	b.Run("StringsBuilder", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			builder := GetBuilder()
			for j := range texts {
				builder.WriteString(texts[j].S)
				if j < len(texts)-1 {
					builder.WriteByte(' ')
				}
			}
			_ = builder.String()
			PutBuilder(builder)
		}
	})
}

// ==================== KD树测试 ====================

func TestKDTreeBasic(t *testing.T) {
	// 创建测试文本块
	blocks := []*TextBlock{
		{MinX: 0, MaxX: 10, MinY: 0, MaxY: 10},
		{MinX: 20, MaxX: 30, MinY: 20, MaxY: 30},
		{MinX: 5, MaxX: 15, MinY: 5, MaxY: 15},
	}

	// 构建KD树
	tree := BuildKDTree(blocks)
	if tree.root == nil {
		t.Error("KD树构建失败")
	}
}

func TestKDTreeRangeSearch(t *testing.T) {
	// 创建测试数据
	blocks := []*TextBlock{
		{MinX: 0, MaxX: 10, MinY: 0, MaxY: 10},
		{MinX: 100, MaxX: 110, MinY: 100, MaxY: 110},
		{MinX: 5, MaxX: 15, MinY: 5, MaxY: 15},
		{MinX: 200, MaxX: 210, MinY: 200, MaxY: 210},
	}

	tree := BuildKDTree(blocks)

	// 在(7.5, 7.5)附近搜索，半径100
	results := tree.RangeSearch([]float64{7.5, 7.5}, 100)

	t.Logf("找到 %d 个近邻块", len(results))
	if len(results) < 2 {
		t.Error("应该找到至少2个近邻块")
	}
}

func BenchmarkKDTreeBuild(b *testing.B) {
	// 生成1000个随机文本块
	blocks := make([]*TextBlock, 1000)
	for i := range blocks {
		x := float64(i * 10)
		y := float64(i * 5)
		blocks[i] = &TextBlock{
			MinX: x,
			MaxX: x + 50,
			MinY: y,
			MaxY: y + 20,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildKDTree(blocks)
	}
}

func BenchmarkKDTreeRangeSearch(b *testing.B) {
	// 构建包含1000个块的KD树
	blocks := make([]*TextBlock, 1000)
	for i := range blocks {
		x := float64(i * 10)
		y := float64(i * 5)
		blocks[i] = &TextBlock{
			MinX: x,
			MaxX: x + 50,
			MinY: y,
			MaxY: y + 20,
		}
	}
	tree := BuildKDTree(blocks)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.RangeSearch([]float64{500, 250}, 10000)
	}
}

// ==================== 聚类优化对比测试 ====================

func BenchmarkClusteringComparison(b *testing.B) {
	// 生成测试数据
	texts := make([]Text, 500)
	for i := range texts {
		texts[i] = Text{
			S:        "Sample text",
			X:        float64(i % 50 * 10),
			Y:        float64(i / 50 * 20),
			W:        50,
			FontSize: 12,
		}
	}

	b.Run("OriginalClustering", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			clusterTextBlocks(texts)
		}
	})

	b.Run("OptimizedClustering", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ClusterTextBlocksOptimized(texts)
		}
	})
}

// ==================== 工作窃取调度器测试 ====================

type testTask struct {
	id     int
	result *int
}

func (t *testTask) Execute() error {
	*t.result = t.id * 2
	time.Sleep(time.Microsecond) // 模拟工作
	return nil
}

func TestWorkStealingScheduler(t *testing.T) {
	scheduler := NewWorkStealingScheduler(4)
	scheduler.Start()
	defer scheduler.Stop()

	// 提交100个任务
	results := make([]int, 100)
	for i := 0; i < 100; i++ {
		task := &testTask{id: i, result: &results[i]}
		scheduler.Submit(task)
	}

	scheduler.Wait()

	// 验证结果
	for i, r := range results {
		if r != i*2 {
			t.Errorf("任务 %d 结果错误: expected %d, got %d", i, i*2, r)
		}
	}
}

func BenchmarkWorkStealingScheduler(b *testing.B) {
	scheduler := NewWorkStealingScheduler(4)
	scheduler.Start()
	defer scheduler.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := 0
		task := &testTask{id: i, result: &result}
		scheduler.Submit(task)
	}
	scheduler.Wait()
}

func BenchmarkSchedulerComparison(b *testing.B) {
	b.Run("WorkStealingScheduler", func(b *testing.B) {
		scheduler := NewWorkStealingScheduler(4)
		scheduler.Start()
		defer scheduler.Stop()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			result := 0
			task := &testTask{id: i, result: &result}
			scheduler.Submit(task)
		}
		scheduler.Wait()
	})

	b.Run("DirectGoroutines", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			result := 0
			task := &testTask{id: i, result: &result}
			task.Execute()
		}
	})
}

// ==================== 多级缓存测试 ====================

func TestMultiLevelCache(t *testing.T) {
	cache := NewMultiLevelCache()

	// 测试存储和检索
	cache.Put("key1", "value1")

	val, ok := cache.Get("key1")
	if !ok {
		t.Error("应该能找到 key1")
	}
	if val.(string) != "value1" {
		t.Errorf("值不匹配: expected 'value1', got '%v'", val)
	}

	// 测试缓存未命中
	_, ok = cache.Get("nonexistent")
	if ok {
		t.Error("不应该找到不存在的key")
	}
}

func TestMultiLevelCacheStats(t *testing.T) {
	cache := NewMultiLevelCache()

	// 写入一些数据
	for i := 0; i < 10; i++ {
		cache.Put(string(rune('a'+i)), i)
	}

	// 读取数据（应该命中L1）
	for i := 0; i < 10; i++ {
		cache.Get(string(rune('a' + i)))
	}

	// 读取不存在的数据
	cache.Get("nonexistent")

	stats := cache.Stats()
	t.Logf("缓存统计: %+v", stats)

	if stats["l1_hits"] == 0 {
		t.Error("L1命中数应该大于0")
	}
}

func BenchmarkMultiLevelCache(b *testing.B) {
	cache := NewMultiLevelCache()

	// 预填充缓存
	for i := 0; i < 1000; i++ {
		cache.Put(string(rune(i)), i)
	}

	b.ResetTimer()
	b.Run("Get", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache.Get(string(rune(i % 1000)))
		}
	})

	b.Run("Put", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache.Put(string(rune(i)), i)
		}
	})
}

func BenchmarkCacheComparison(b *testing.B) {
	b.Run("MultiLevelCache", func(b *testing.B) {
		cache := NewMultiLevelCache()
		for i := 0; i < 100; i++ {
			cache.Put(string(rune(i)), i)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cache.Get(string(rune(i % 100)))
		}
	})

	b.Run("SingleLevelCache", func(b *testing.B) {
		cache := NewResultCache(10*1024*1024, 5*time.Minute, "LRU")
		for i := 0; i < 100; i++ {
			cache.Put(string(rune(i)), i)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cache.Get(string(rune(i % 100)))
		}
	})
}

// ==================== 性能指标测试 ====================

func TestPerformanceMetrics(t *testing.T) {
	metrics := &PerformanceMetrics{}

	// 记录一些指标
	metrics.RecordExtractDuration(100 * time.Millisecond)
	metrics.RecordAllocation(1024)
	metrics.RecordAllocation(2048)

	// 获取指标
	m := metrics.GetMetrics()
	t.Logf("性能指标: %+v", m)

	if m["extract_duration_ms"].(float64) != 100.0 {
		t.Errorf("提取耗时不正确")
	}

	if m["total_allocs"].(uint64) != 2 {
		t.Errorf("分配次数不正确")
	}

	if m["bytes_allocated"].(uint64) != 3072 {
		t.Errorf("分配字节数不正确")
	}
}

// ==================== 集成性能对比测试 ====================

func BenchmarkOptimizationImpact(b *testing.B) {
	// 这个基准测试对比优化前后的性能

	// 生成测试数据
	texts := make([]Text, 1000)
	for i := range texts {
		texts[i] = Text{
			S:        "This is a sample text for performance testing purposes.",
			X:        float64(i % 100),
			Y:        float64(i / 100),
			W:        100,
			FontSize: 12,
			Font:     "Arial",
		}
	}

	b.Run("WithOptimizations", func(b *testing.B) {
		// 使用优化版本
		estimator := NewAdaptiveCapacityEstimator(50)
		cache := NewMultiLevelCache()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// 模拟优化的文本提取流程
			cap := estimator.Estimate(len(texts))
			result := make([]Text, 0, cap)

			for j := range texts {
				key := string(rune(j))
				if val, ok := cache.Get(key); ok {
					result = append(result, val.(Text))
				} else {
					result = append(result, texts[j])
					cache.Put(key, texts[j])
				}
			}

			estimator.Record(len(result))
		}
	})

	b.Run("WithoutOptimizations", func(b *testing.B) {
		// 不使用优化
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			result := make([]Text, 0) // 不预分配
			for j := range texts {
				result = append(result, texts[j])
			}
		}
	})
}
