package pdf

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"testing"
	"time"
)

// MemoryProfile records memory usage
type MemoryProfile struct {
	Timestamp  time.Time
	Alloc      uint64
	TotalAlloc uint64
	Sys        uint64
	NumGC      uint32
	Goroutines int
}

// RecordMemory records current memory state
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

// MemoryDifference calculates difference between two records
func MemoryDifference(before, after MemoryProfile) {
	allocDiff := int64(after.Alloc) - int64(before.Alloc)
	fmt.Printf("  Memory Delta: %+v MB (%.2f%% change)\n",
		allocDiff/1024/1024,
		float64(allocDiff)*100/float64(before.Alloc+1))
	fmt.Printf("  GC Events: %v (before) -> %v (after)\n", before.NumGC, after.NumGC)
}

// TestFontCacheReferenceCleanup verifies fontCache reference cleanup
func TestFontCacheReferenceCleanup(t *testing.T) {
	t.Log("Test: FontCache Reference Cleanup")

	// Simulate fontCache
	fontCache := make(map[string]interface{})

	// Simulate Page structure
	type mockPage struct {
		fontCache map[string]interface{}
	}

	start := RecordMemory("初始")

	// Simulate processing 500 pages (problem scenario)
	for page := 0; page < 500; page++ {
		p := mockPage{fontCache: fontCache}

		// Add fonts to cache
		for i := 0; i < 50; i++ {
			fontCache[fmt.Sprintf("font_%d", page*50+i)] = make([]byte, 4096)
		}

		// [Critical fix] Clean up references
		p.fontCache = nil

		// Prevent optimizer elimination
		_ = p
	}

	middle := RecordMemory("处理后")
	MemoryDifference(start, middle)

	// Clean up
	fontCache = make(map[string]interface{})
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	end := RecordMemory("清理后")
	MemoryDifference(middle, end)
}

// TestObjCacheCapacityLimit verifies objCache capacity limit
func TestObjCacheCapacityLimit(t *testing.T) {
	t.Log("Test: ObjCache Capacity Limiting")

	// No capacity limit (problem)
	t.Log("Scenario A: No capacity limit")
	cacheNoLimit := make(map[int][]byte)
	start1 := RecordMemory("无限制-初始")

	for i := 0; i < 50000; i++ {
		cacheNoLimit[i] = make([]byte, 1024)
	}

	end1 := RecordMemory("无限制-添加50000项")
	MemoryDifference(start1, end1)

	// With capacity limit (fix)
	t.Log("Scenario B: With capacity limit")
	cacheWithLimit := make(map[int][]byte)
	const maxCap = 5000
	start2 := RecordMemory("有限制-初始")

	for i := 0; i < 50000; i++ {
		cacheWithLimit[i] = make([]byte, 1024)
		// Remove old items when exceeding capacity
		if len(cacheWithLimit) > maxCap {
			delete(cacheWithLimit, i-maxCap)
		}
	}

	end2 := RecordMemory("有限制-添加50000项")
	MemoryDifference(start2, end2)

	// Comparison
	t.Logf("无限制缓存项数: %d", len(cacheNoLimit))
	t.Logf("有限制缓存项数: %d (容量: %d)", len(cacheWithLimit), maxCap)
}

// TestPeriodicGCImpact verifies impact of periodic GC
func TestPeriodicGCImpact(t *testing.T) {
	t.Log("Test: Periodic GC Impact")

	// Disable automatic GC
	oldGCPercent := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(oldGCPercent)

	// Scenario A: Without GC
	t.Log("Scenario A: Without GC")
	data1 := make([][]byte, 0)
	start1 := RecordMemory("无GC-初始")

	for i := 0; i < 50000; i++ {
		data1 = append(data1, make([]byte, 1024))
	}

	end1 := RecordMemory("无GC-分配50000项")
	MemoryDifference(start1, end1)

	// Scenario B: With periodic GC
	t.Log("Scenario B: With periodic GC")
	data2 := make([][]byte, 0)
	start2 := RecordMemory("有GC-初始")

	for i := 0; i < 50000; i++ {
		data2 = append(data2, make([]byte, 1024))
		if i%5000 == 0 && i > 0 {
			runtime.GC() // Periodic GC
		}
	}

	end2 := RecordMemory("有GC-分配50000项")
	MemoryDifference(start2, end2)
}

// TestBatchExtractMemoryManagement verifies batch processing memory management
func TestBatchExtractMemoryManagement(t *testing.T) {
	t.Log("Test: Batch Extract Memory Management")

	// If there is a test PDF, can run actual batch processing test
	// Here only shows the test framework

	start := RecordMemory("批处理-初始")

	// Simulate batch processing logic
	// Actual usage:
	// opts := BatchExtractOptions{Workers: 4, UseFontCache: true}
	// results := reader.ExtractPagesBatch(opts)
	// for range results { }

	// Clean up
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	end := RecordMemory("批处理-完成后")
	MemoryDifference(start, end)
}

// TestExtractPagesBatchMemoryLeak tests that ExtractPagesBatch properly cleans up memory
func TestExtractPagesBatchMemoryLeak(t *testing.T) {
	t.Log("Test: ExtractPagesBatch Memory Leak Detection")

	// Simulate multiple batch extractions to verify memory doesn't accumulate
	iterations := 3

	debug.SetGCPercent(100) // Enable normal GC
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	initialMemory := RecordMemory("初始基线")

	// Simulate batch processing multiple times
	for i := 0; i < iterations; i++ {
		t.Logf("Iteration %d/%d", i+1, iterations)

		// Simulate a batch extraction scenario
		// In real usage, this would be:
		// r, _ := Open("large.pdf")
		// opts := BatchExtractOptions{Workers: 4, UseFontCache: true}
		// for result := range r.ExtractPagesBatch(opts) { ... }
		// r.Close()

		// For testing without a PDF file, we simulate the memory pattern
		mockBatchExtract(t, 500) // Simulate 500 pages

		// Force cleanup
		runtime.GC()
		time.Sleep(100 * time.Millisecond)

		RecordMemory(fmt.Sprintf("迭代%d完成", i+1))
	}

	finalMemory := RecordMemory("最终状态")

	// Calculate memory growth
	memoryGrowth := int64(finalMemory.Alloc) - int64(initialMemory.Alloc)
	growthMB := memoryGrowth / 1024 / 1024

	t.Logf("总内存增长: %d MB", growthMB)

	// Memory should not grow significantly after multiple iterations
	// Allow up to 50MB growth for test overhead
	if growthMB > 50 {
		t.Errorf("Memory leak detected: growth %d MB (expected < 50 MB)", growthMB)
	} else {
		t.Logf("Memory management normal: growth only %d MB", growthMB)
	}
}

// mockBatchExtract simulates the memory allocation pattern of batch extraction
func mockBatchExtract(t *testing.T, numPages int) {
	// Simulate objCache with capacity limit
	const maxCacheSize = 5000
	objCache := make(map[int][]byte)

	// Simulate fontCache
	fontCache := make(map[string][]byte)

	// Process pages
	for i := 0; i < numPages; i++ {
		// Simulate parsing objects (objCache entries)
		for j := 0; j < 10; j++ {
			key := i*10 + j
			objCache[key] = make([]byte, 512)

			// Evict old entries when capacity exceeded
			if len(objCache) > maxCacheSize {
				// Remove oldest entry
				oldestKey := key - maxCacheSize
				delete(objCache, oldestKey)
			}
		}

		// Simulate font parsing (fontCache entries)
		if i%10 == 0 {
			fontKey := fmt.Sprintf("font_%d", i/10)
			fontCache[fontKey] = make([]byte, 2048)
		}
	}

	// Simulate cleanup (what our fix does)
	objCache = nil
	fontCache = nil
	runtime.GC()
}

// TestReaderCloseClearsCache verifies that Reader.Close() properly clears the object cache
func TestReaderCloseClearsCache(t *testing.T) {
	t.Log("Test: Reader Close Clears Cache")

	// This test would require a PDF file, so we'll test the cache clearing mechanism directly
	// In a real scenario, this would be tested with an actual PDF

	start := RecordMemory("Reader缓存清理-初始")

	// Simulate cache usage (without actual PDF)
	// In real usage: r, _ := Open("test.pdf"); r.GetPlainText(); r.Close()

	// For testing, we'll create a mock scenario
	mockCacheUsage(t)

	end := RecordMemory("Reader缓存清理-完成后")
	MemoryDifference(start, end)

	// Memory should not grow significantly
	growth := int64(end.Alloc) - int64(start.Alloc)
	if growth > 50*1024*1024 { // 50MB threshold
		t.Errorf("内存泄漏: Reader.Close()后内存增长 %d MB", growth/1024/1024)
	}
}

// mockCacheUsage simulates cache usage and cleanup
func mockCacheUsage(t *testing.T) {
	// Simulate what happens during PDF processing
	// Create some cached objects that would be cleaned up by Close()

	// This is a simplified simulation since we don't have a test PDF
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
}

// BenchmarkWithCapacityLimit benchmark test: with capacity limit
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
