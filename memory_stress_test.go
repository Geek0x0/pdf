package pdf

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestStringBuilderPoolMemoryStress tests memory usage under high concurrent load
func TestStringBuilderPoolMemoryStress(t *testing.T) {
	const (
		goroutines = 500
		iterations = 1000
	)

	var m1, m2 runtime.MemStats
	runtime.ReadMemStats(&m1)
	gcBefore := m1.NumGC

	var wg sync.WaitGroup
	wg.Add(goroutines)

	startTime := time.Now()

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				// Simulate real-world usage pattern
				size := (id*iterations + j) % 100000
				builder := GetSizedStringBuilder(size)

				// Write some data
				for k := 0; k < 10; k++ {
					builder.WriteString("test data ")
					builder.WriteByte('\n')
				}

				_ = builder.String()
				PutSizedStringBuilder(builder, size)
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	runtime.ReadMemStats(&m2)
	gcAfter := m2.NumGC

	t.Logf("Stress Test Results:")
	t.Logf("  Goroutines: %d", goroutines)
	t.Logf("  Iterations per goroutine: %d", iterations)
	t.Logf("  Total operations: %d", goroutines*iterations)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Ops/sec: %.0f", float64(goroutines*iterations)/duration.Seconds())
	t.Logf("  Memory allocated: %.2f MB", float64(m2.TotalAlloc-m1.TotalAlloc)/1024/1024)
	t.Logf("  Memory in-use: %.2f MB", float64(m2.Alloc)/1024/1024)
	t.Logf("  GC cycles: %d", gcAfter-gcBefore)
	t.Logf("  Avg allocation per op: %.2f bytes", float64(m2.TotalAlloc-m1.TotalAlloc)/float64(goroutines*iterations))
}

// TestBuildPlainTextMemoryEfficiency tests memory efficiency of buildPlainTextOptimized
func TestBuildPlainTextMemoryEfficiency(t *testing.T) {
	// Create test data
	texts := make([]Text, 1000)
	for i := range texts {
		texts[i] = Text{
			X:        float64(i * 10),
			Y:        float64(i / 50 * 20),
			W:        5.0,
			FontSize: 12.0,
			S:        "Sample text content",
		}
	}

	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	const iterations = 1000
	for i := 0; i < iterations; i++ {
		result := buildPlainTextOptimized(texts)
		if len(result) == 0 {
			t.Fatal("Empty result")
		}
	}

	runtime.ReadMemStats(&m2)

	t.Logf("BuildPlainText Memory Efficiency:")
	t.Logf("  Iterations: %d", iterations)
	t.Logf("  Input texts: %d", len(texts))
	t.Logf("  Total allocated: %.2f MB", float64(m2.TotalAlloc-m1.TotalAlloc)/1024/1024)
	t.Logf("  Avg per iteration: %.2f KB", float64(m2.TotalAlloc-m1.TotalAlloc)/float64(iterations)/1024)
	t.Logf("  GC cycles: %d", m2.NumGC-m1.NumGC)
}

// TestPoolReuseEfficiency verifies that pool reuse is working effectively
func TestPoolReuseEfficiency(t *testing.T) {
	const iterations = 10000

	// Count allocations for new builders
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	for i := 0; i < iterations; i++ {
		builder := NewFastStringBuilder(1000)
		builder.WriteString("test")
		_ = builder.String()
		// Not returned to pool - should cause more allocations
	}

	runtime.ReadMemStats(&m2)
	allocWithoutPool := m2.TotalAlloc - m1.TotalAlloc

	// Count allocations with pool reuse
	runtime.GC()
	runtime.ReadMemStats(&m1)

	for i := 0; i < iterations; i++ {
		builder := GetSizedStringBuilder(1000)
		builder.WriteString("test")
		_ = builder.String()
		PutSizedStringBuilder(builder, 1000)
	}

	runtime.ReadMemStats(&m2)
	allocWithPool := m2.TotalAlloc - m1.TotalAlloc

	t.Logf("Pool Reuse Efficiency:")
	t.Logf("  Iterations: %d", iterations)
	t.Logf("  Without pool: %.2f MB", float64(allocWithoutPool)/1024/1024)
	t.Logf("  With pool: %.2f MB", float64(allocWithPool)/1024/1024)
	t.Logf("  Reduction: %.1f%%", (1.0-float64(allocWithPool)/float64(allocWithoutPool))*100)

	if allocWithPool > allocWithoutPool {
		t.Errorf("Pool should reduce allocations, but got more: with=%d without=%d",
			allocWithPool, allocWithoutPool)
	}
}

// TestConcurrentPoolSafety ensures no race conditions or panics under concurrent access
func TestConcurrentPoolSafety(t *testing.T) {
	const (
		goroutines = 100
		iterations = 1000
	)

	var wg sync.WaitGroup
	var panics sync.Map
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panics.Store(id, r)
					t.Errorf("Goroutine %d panicked: %v", id, r)
				}
			}()

			for j := 0; j < iterations; j++ {
				size := (j % 3) * 5000 // Mix sizes to hit different pools
				builder := GetSizedStringBuilder(size)

				if builder == nil {
					t.Errorf("Got nil builder at iteration %d", j)
					continue
				}

				builder.WriteString("concurrent ")
				builder.WriteByte('t')
				builder.WriteString("est")
				_ = builder.Len()
				_ = builder.String()
				builder.Reset()

				PutSizedStringBuilder(builder, size)
			}
		}(i)
	}

	wg.Wait()

	panicCount := 0
	panics.Range(func(key, value interface{}) bool {
		panicCount++
		return true
	})

	if panicCount > 0 {
		t.Fatalf("Detected %d panics during concurrent access", panicCount)
	}
}
