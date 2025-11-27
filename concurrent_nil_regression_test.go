package pdf

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestBuildPlainTextConcurrentStress 针对 buildPlainTextOptimized 的并发压力测试
// 复现原始问题：多个 goroutine 并发调用 buildPlainTextOptimized
func TestBuildPlainTextConcurrentStress(t *testing.T) {
	const (
		goroutines = 200 // 模拟高并发
		iterations = 100 // 每个 goroutine 的迭代次数
	)

	// 准备测试数据 - 模拟真实的 PDF 文本提取场景
	testCases := [][]Text{
		// 短文本
		{
			{X: 10, Y: 10, W: 5, FontSize: 12, S: "Hello"},
			{X: 20, Y: 10, W: 5, FontSize: 12, S: "World"},
		},
		// 中等长度文本
		generateConcurrentTestTexts(100),
		// 长文本
		generateConcurrentTestTexts(500),
		// 空文本
		{},
		// 单个文本
		{{X: 0, Y: 0, W: 1, FontSize: 10, S: "A"}},
	}

	var (
		wg         sync.WaitGroup
		panics     int32
		nilResults int32
	)

	wg.Add(goroutines)
	startTime := time.Now()

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					atomic.AddInt32(&panics, 1)
					t.Errorf("Goroutine %d panicked: %v", id, r)
				}
			}()

			for j := 0; j < iterations; j++ {
				// 使用不同的测试用例
				texts := testCases[j%len(testCases)]

				// 这是问题代码路径：buildPlainTextOptimized
				result := buildPlainTextOptimized(texts)

				// 验证结果
				if len(texts) > 0 && len(result) == 0 {
					atomic.AddInt32(&nilResults, 1)
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	// 报告结果
	t.Logf("Concurrent Stress Test Results:")
	t.Logf("  Goroutines: %d", goroutines)
	t.Logf("  Iterations per goroutine: %d", iterations)
	t.Logf("  Total calls: %d", goroutines*iterations)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Throughput: %.0f calls/sec", float64(goroutines*iterations)/duration.Seconds())
	t.Logf("  Panics: %d", panics)
	t.Logf("  Nil results: %d", nilResults)

	if panics > 0 {
		t.Fatalf("FAIL: Detected %d panics during concurrent execution", panics)
	}
}

// TestSmartTextRunsToPlainConcurrent 测试 SmartTextRunsToPlain 的并发安全性
// 主要测试不会panic，使用简化的数据避免竞态检测误报
func TestSmartTextRunsToPlainConcurrent(t *testing.T) {
	const goroutines = 100

	var wg sync.WaitGroup
	var panics int32

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					atomic.AddInt32(&panics, 1)
					t.Errorf("Panic in goroutine %d: %v", id, r)
				}
			}()

			// 使用简单的测试数据
			texts := []Text{
				{X: 10.0, Y: 10.0, W: 20.0, FontSize: 12.0, S: "Hello"},
				{X: 35.0, Y: 10.0, W: 20.0, FontSize: 12.0, S: "World"},
				{X: 10.0, Y: 30.0, W: 15.0, FontSize: 12.0, S: "Test"},
			}

			// 调用真实的 API - 主要测试不panic
			_ = SmartTextRunsToPlain(texts)
		}(i)
	}

	wg.Wait()

	if panics > 0 {
		t.Fatalf("FAIL: %d panics occurred", panics)
	}
}

// TestFastStringBuilderDirectAllocation 验证直接分配的安全性
func TestFastStringBuilderDirectAllocation(t *testing.T) {
	const goroutines = 500
	const iterations = 1000

	var wg sync.WaitGroup
	var panics int32

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					atomic.AddInt32(&panics, 1)
					t.Errorf("Panic: %v", r)
				}
			}()

			for j := 0; j < iterations; j++ {
				// 模拟 buildPlainTextOptimized 中的模式
				size := (j % 100) * 21
				if size < 256 {
					size = 256
				}

				builder := NewFastStringBuilder(size)

				// 执行操作
				for k := 0; k < 10; k++ {
					builder.WriteString("test ")
					builder.WriteByte('\n')
				}

				result := builder.String()
				if len(result) == 0 {
					t.Errorf("Empty result from builder")
				}
			}
		}(i)
	}

	wg.Wait()

	if panics > 0 {
		t.Fatalf("FAIL: Direct allocation caused %d panics", panics)
	}
}

// TestFastStringBuilderNilPointerDefense 专门测试 nil 指针防御
func TestFastStringBuilderNilPointerDefense(t *testing.T) {
	tests := []struct {
		name    string
		builder *FastStringBuilder
		op      func(*FastStringBuilder)
		wantErr bool
	}{
		{
			name:    "nil builder WriteString",
			builder: nil,
			op:      func(b *FastStringBuilder) { b.WriteString("test") },
			wantErr: false, // Should not panic
		},
		{
			name:    "nil builder WriteByte",
			builder: nil,
			op:      func(b *FastStringBuilder) { b.WriteByte('a') },
			wantErr: false,
		},
		{
			name:    "nil builder String",
			builder: nil,
			op:      func(b *FastStringBuilder) { _ = b.String() },
			wantErr: false,
		},
		{
			name:    "nil builder Len",
			builder: nil,
			op:      func(b *FastStringBuilder) { _ = b.Len() },
			wantErr: false,
		},
		{
			name:    "nil builder Reset",
			builder: nil,
			op:      func(b *FastStringBuilder) { b.Reset() },
			wantErr: false,
		},
		{
			name:    "nil buf WriteString",
			builder: &FastStringBuilder{buf: nil},
			op:      func(b *FastStringBuilder) { b.WriteString("test") },
			wantErr: false,
		},
		{
			name:    "nil buf WriteByte",
			builder: &FastStringBuilder{buf: nil},
			op:      func(b *FastStringBuilder) { b.WriteByte('a') },
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					if !tt.wantErr {
						t.Errorf("Unexpected panic: %v", r)
					}
				}
			}()

			tt.op(tt.builder)
		})
	}
}

// TestBatchExtractWorkerConcurrent 模拟批处理场景的并发测试
func TestBatchExtractWorkerConcurrent(t *testing.T) {
	// 跳过如果没有测试 PDF 文件
	if testing.Short() {
		t.Skip("Skipping batch extract test in short mode")
	}

	// 这个测试需要实际的 PDF 文件，这里只测试并发调用模式
	const workers = 50
	const jobsPerWorker = 20

	var wg sync.WaitGroup
	var panics int32

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					atomic.AddInt32(&panics, 1)
					t.Logf("Worker %d panicked: %v", id, r)
				}
			}()

			for j := 0; j < jobsPerWorker; j++ {
				// 模拟文本构建操作
				texts := generateConcurrentTestTexts(50 + j*10)
				_ = buildPlainTextOptimized(texts)
			}
		}(i)
	}

	wg.Wait()

	if panics > 0 {
		t.Errorf("FAIL: %d workers panicked", panics)
	}
}

// TestMemoryLeakPrevention 验证没有内存泄漏
func TestMemoryLeakPrevention(t *testing.T) {
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	const iterations = 10000
	for i := 0; i < iterations; i++ {
		texts := generateConcurrentTestTexts(100)
		_ = buildPlainTextOptimized(texts)
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	allocPerOp := float64(m2.TotalAlloc-m1.TotalAlloc) / float64(iterations)

	t.Logf("Memory Leak Check:")
	t.Logf("  Iterations: %d", iterations)
	t.Logf("  Total allocated: %.2f MB", float64(m2.TotalAlloc-m1.TotalAlloc)/1024/1024)
	t.Logf("  Avg per operation: %.2f KB", allocPerOp/1024)
	t.Logf("  Current in-use: %.2f MB", float64(m2.Alloc)/1024/1024)
	t.Logf("  GC runs: %d", m2.NumGC-m1.NumGC)

	// 验证没有明显的内存泄漏（当前使用应该很低）
	if m2.Alloc > 100*1024*1024 { // 100MB
		t.Errorf("Possible memory leak: current allocation is %.2f MB", float64(m2.Alloc)/1024/1024)
	}
}

// TestRaceConditionDetection 使用 race detector 检测竞态条件
func TestRaceConditionDetection(t *testing.T) {
	const goroutines = 100
	const iterations = 100

	var wg sync.WaitGroup

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// 每个迭代创建自己的数据，避免共享导致的竞态
				texts := generateConcurrentTestTexts(50)
				_ = buildPlainTextOptimized(texts)
			}
		}()
	}

	wg.Wait()
}

// TestEdgeCases 测试边界情况
func TestEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		texts []Text
	}{
		{"Empty", []Text{}},
		{"Single char", []Text{{X: 0, Y: 0, W: 1, FontSize: 10, S: "A"}}},
		{"Very long text", []Text{{X: 0, Y: 0, W: 1000, FontSize: 10, S: string(make([]byte, 10000))}}},
		{"Many small texts", generateConcurrentTestTexts(1000)},
		{"Unicode", []Text{
			{X: 0, Y: 0, W: 5, FontSize: 12, S: "你好"},
			{X: 10, Y: 0, W: 5, FontSize: 12, S: "世界"},
		}},
		{"Special chars", []Text{
			{X: 0, Y: 0, W: 5, FontSize: 12, S: "\n\t\r"},
			{X: 10, Y: 0, W: 5, FontSize: 12, S: "test"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Panic on edge case %s: %v", tt.name, r)
				}
			}()

			result := buildPlainTextOptimized(tt.texts)
			// 验证结果是有效字符串（可能为空）
			_ = len(result)
		})
	}
}

// TestConcurrentWithGC 并发执行时触发 GC
func TestConcurrentWithGC(t *testing.T) {
	const goroutines = 50
	var wg sync.WaitGroup

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < 100; j++ {
				texts := generateConcurrentTestTexts(100)
				_ = buildPlainTextOptimized(texts)

				// 定期触发 GC
				if j%10 == 0 {
					runtime.GC()
				}
			}
		}(i)
	}

	wg.Wait()
}

// Helper function
func generateConcurrentTestTexts(count int) []Text {
	texts := make([]Text, count)
	for i := 0; i < count; i++ {
		texts[i] = Text{
			X:        float64(i * 10),
			Y:        float64(i / 50 * 20),
			W:        5.0,
			FontSize: 12.0,
			S:        fmt.Sprintf("Text_%d", i),
		}
	}
	return texts
}

// Benchmark for performance regression detection
func BenchmarkBuildPlainTextOptimized(b *testing.B) {
	texts := generateConcurrentTestTexts(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildPlainTextOptimized(texts)
	}
}

func BenchmarkBuildPlainTextOptimizedParallel(b *testing.B) {
	texts := generateConcurrentTestTexts(100)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = buildPlainTextOptimized(texts)
		}
	})
}

// TestNoPoolSharingBetweenGoroutines 验证没有跨 goroutine 的对象共享
func TestNoPoolSharingBetweenGoroutines(t *testing.T) {
	const goroutines = 100
	var wg sync.WaitGroup

	// 用于检测是否有相同的 builder 被多个 goroutine 使用
	builderAddrs := sync.Map{}
	conflicts := int32(0)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()

			texts := generateConcurrentTestTexts(50)

			// 在执行期间，理论上不应该有其他 goroutine 使用相同的 builder
			// 因为我们现在使用直接分配而不是池
			builder := NewFastStringBuilder(len(texts) * 21)
			addr := fmt.Sprintf("%p", builder)

			// 检查这个地址是否被其他 goroutine 同时使用
			if _, loaded := builderAddrs.LoadOrStore(addr, id); loaded {
				atomic.AddInt32(&conflicts, 1)
				t.Errorf("Builder %s is being used by multiple goroutines", addr)
			}

			// 模拟使用
			for _, text := range texts {
				builder.WriteString(text.S)
			}
			_ = builder.String()

			// 使用完毕，移除
			builderAddrs.Delete(addr)
		}(i)
	}

	wg.Wait()

	if conflicts > 0 {
		t.Errorf("FAIL: Detected %d builder sharing conflicts", conflicts)
	}
}
