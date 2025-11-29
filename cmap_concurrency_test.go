package pdf

import (
	"sync"
	"testing"
	"time"
)

// TestLockFreeCMapConcurrency 测试无锁CMap在高并发场景下的性能和正确性
func TestLockFreeCMapConcurrency(t *testing.T) {
	// 创建一个测试CMap
	cmap := &CMap{
		cidCache:    &sync.Map{},
		decodeCache: &sync.Map{},
	}

	// 预填充一些数据
	for i := 0; i < 1000; i++ {
		cid := uint16(i)
		charCode := uint16(i + 1000)
		cmap.cidCache.Store(cid, charCode)
		cmap.decodeCache.Store(charCode, string(rune(cid)))
	}

	const numGoroutines = 100
	const operationsPerGoroutine = 10000

	var wg sync.WaitGroup
	start := time.Now()

	// 启动多个goroutine并发访问
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for i := 0; i < operationsPerGoroutine; i++ {
				cid := uint16((goroutineID*operationsPerGoroutine + i) % 1000)

				// 测试LookupCID (无锁读取)
				if charCode, ok := cmap.cidCache.Load(cid); ok {
					_ = charCode.(uint16)
				}

				// 测试Decode (无锁读取)
				if decoded, ok := cmap.decodeCache.Load(uint16(cid + 1000)); ok {
					_ = decoded.(string)
				}
			}
		}(g)
	}

	wg.Wait()
	duration := time.Since(start)

	totalOperations := numGoroutines * operationsPerGoroutine * 2 // LookupCID + Decode
	opsPerSecond := float64(totalOperations) / duration.Seconds()

	t.Logf("并发压力测试完成:")
	t.Logf("  Goroutines: %d", numGoroutines)
	t.Logf("  每goroutine操作数: %d", operationsPerGoroutine)
	t.Logf("  总操作数: %d", totalOperations)
	t.Logf("  耗时: %v", duration)
	t.Logf("  每秒操作数: %.0f", opsPerSecond)
	t.Logf("  平均每操作耗时: %.2f ns", float64(duration.Nanoseconds())/float64(totalOperations))

	// 验证数据一致性
	for i := 0; i < 1000; i++ {
		cid := uint16(i)
		if charCode, ok := cmap.cidCache.Load(cid); !ok || charCode.(uint16) != uint16(i+1000) {
			t.Errorf("数据不一致: CID %d", cid)
		}
	}
}

// BenchmarkLockFreeCMapConcurrent 并发基准测试
func BenchmarkLockFreeCMapConcurrent(b *testing.B) {
	cmap := &CMap{
		cidCache:    &sync.Map{},
		decodeCache: &sync.Map{},
	}

	// 预填充数据
	for i := 0; i < 1000; i++ {
		cid := uint16(i)
		charCode := uint16(i + 1000)
		cmap.cidCache.Store(cid, charCode)
		cmap.decodeCache.Store(charCode, string(rune(cid)))
	}

	b.RunParallel(func(pb *testing.PB) {
		localData := make([]uint16, 1000)
		for i := range localData {
			localData[i] = uint16(i)
		}

		for pb.Next() {
			for _, cid := range localData {
				// LookupCID
				if _, ok := cmap.cidCache.Load(cid); !ok {
					b.Fatal("LookupCID failed")
				}

				// Decode
				charCode := uint16(cid + 1000)
				if _, ok := cmap.decodeCache.Load(charCode); !ok {
					b.Fatal("Decode failed")
				}
			}
		}
	})
}
