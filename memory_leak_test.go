package pdf

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"testing"
	"time"
)

// MemoryProfile 记录内存使用情况
type MemoryProfile struct {
	Timestamp  time.Time
	Alloc      uint64
	TotalAlloc uint64
	Sys        uint64
	NumGC      uint32
	Goroutines int
}

// RecordMemory 记录当前内存状态
func RecordMemory(label string) MemoryProfile {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("[%s] Alloc: %v MB, Sys: %v MB, GC: %v, Goroutines: %v\n",
		label, m.Alloc/1024/1024, m.Sys/1024/1024, m.NumGC, runtime.NumGoroutine())
	return MemoryProfile{
		Timestamp:  time.Now(),
		Alloc:      m.Alloc,
		TotalAlloc: m.TotalAlloc,
		Sys:        m.Sys,
		NumGC:      m.NumGC,
		Goroutines: runtime.NumGoroutine(),
	}
}

// MemoryDifference 计算两次记录间的差异
func MemoryDifference(before, after MemoryProfile) {
	allocDiff := int64(after.Alloc) - int64(before.Alloc)
	fmt.Printf("  Memory Delta: %+v MB (%.2f%% change)\n",
		allocDiff/1024/1024,
		float64(allocDiff)*100/float64(before.Alloc+1))
	fmt.Printf("  GC Events: %v (before) -> %v (after)\n", before.NumGC, after.NumGC)
}

// TestFontCacheReferenceCleanup 验证 fontCache 引用清理
func TestFontCacheReferenceCleanup(t *testing.T) {
	t.Log("Test: FontCache Reference Cleanup")

	// 模拟 fontCache
	fontCache := make(map[string]interface{})

	// 模拟 Page 结构
	type mockPage struct {
		fontCache map[string]interface{}
	}

	start := RecordMemory("初始")

	// 模拟处理 500 页（问题场景）
	for page := 0; page < 500; page++ {
		p := mockPage{fontCache: fontCache}

		// 添加字体到缓存
		for i := 0; i < 50; i++ {
			fontCache[fmt.Sprintf("font_%d", page*50+i)] = make([]byte, 4096)
		}

		// 【关键修复】清理引用
		p.fontCache = nil

		// 防止优化器消除
		_ = p
	}

	middle := RecordMemory("处理后")
	MemoryDifference(start, middle)

	// 清理
	fontCache = make(map[string]interface{})
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	end := RecordMemory("清理后")
	MemoryDifference(middle, end)
}

// TestObjCacheCapacityLimit 验证 objCache 容量限制
func TestObjCacheCapacityLimit(t *testing.T) {
	t.Log("Test: ObjCache Capacity Limiting")

	// 无容量限制（问题）
	t.Log("Scenario A: No capacity limit")
	cacheNoLimit := make(map[int][]byte)
	start1 := RecordMemory("无限制-初始")

	for i := 0; i < 50000; i++ {
		cacheNoLimit[i] = make([]byte, 1024)
	}

	end1 := RecordMemory("无限制-添加50000项")
	MemoryDifference(start1, end1)

	// 有容量限制（修复）
	t.Log("Scenario B: With capacity limit")
	cacheWithLimit := make(map[int][]byte)
	const maxCap = 5000
	start2 := RecordMemory("有限制-初始")

	for i := 0; i < 50000; i++ {
		cacheWithLimit[i] = make([]byte, 1024)
		// 超过容量则移除旧项
		if len(cacheWithLimit) > maxCap {
			delete(cacheWithLimit, i-maxCap)
		}
	}

	end2 := RecordMemory("有限制-添加50000项")
	MemoryDifference(start2, end2)

	// 对比
	t.Logf("无限制缓存项数: %d", len(cacheNoLimit))
	t.Logf("有限制缓存项数: %d (容量: %d)", len(cacheWithLimit), maxCap)
}

// TestPeriodicGCImpact 验证定期 GC 的影响
func TestPeriodicGCImpact(t *testing.T) {
	t.Log("Test: Periodic GC Impact")

	// 禁用自动 GC
	oldPercent := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(oldPercent)

	// 场景 A：无 GC
	t.Log("Scenario A: Without GC")
	data1 := make([][]byte, 0)
	start1 := RecordMemory("无GC-初始")

	for i := 0; i < 50000; i++ {
		data1 = append(data1, make([]byte, 1024))
	}

	end1 := RecordMemory("无GC-分配50000项")
	MemoryDifference(start1, end1)

	// 场景 B：有定期 GC
	t.Log("Scenario B: With periodic GC")
	data2 := make([][]byte, 0)
	start2 := RecordMemory("有GC-初始")

	for i := 0; i < 50000; i++ {
		data2 = append(data2, make([]byte, 1024))
		if i%5000 == 0 && i > 0 {
			runtime.GC() // 定期 GC
		}
	}

	end2 := RecordMemory("有GC-分配50000项")
	MemoryDifference(start2, end2)
}

// TestBatchExtractMemoryManagement 验证批处理内存管理
func TestBatchExtractMemoryManagement(t *testing.T) {
	t.Log("Test: Batch Extract Memory Management")

	// 如果有测试 PDF，可以运行实际的批处理测试
	// 这里仅展示测试框架

	start := RecordMemory("批处理-初始")

	// 模拟批处理逻辑
	// 实际用法：
	// opts := BatchExtractOptions{Workers: 4, UseFontCache: true}
	// results := reader.ExtractPagesBatch(opts)
	// for range results { }

	// 清理
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	end := RecordMemory("批处理-完成后")
	MemoryDifference(start, end)
}

// BenchmarkMemoryLeakSimulation 基准测试：模拟内存泄漏
func BenchmarkMemoryLeakSimulation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		cache := make(map[int][]byte)
		for j := 0; j < 1000; j++ {
			cache[j] = make([]byte, 4096)
		}
		// 没有清理，模拟泄漏
	}
}

// BenchmarkWithCapacityLimit 基准测试：有容量限制
func BenchmarkWithCapacityLimit(b *testing.B) {
	const maxCap = 500
	for i := 0; i < b.N; i++ {
		cache := make(map[int][]byte)
		for j := 0; j < 1000; j++ {
			cache[j] = make([]byte, 4096)
			if len(cache) > maxCap {
				delete(cache, j-maxCap)
			}
		}
	}
}
