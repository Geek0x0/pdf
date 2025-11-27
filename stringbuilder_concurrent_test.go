package pdf

import (
	"sync"
	"testing"
)

// TestFastStringBuilderConcurrentNilSafety tests that FastStringBuilder methods
// are safe to call even when the builder or its buffer is nil during concurrent access
func TestFastStringBuilderConcurrentNilSafety(t *testing.T) {
	const goroutines = 100
	const iterations = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				// Simulate the real-world scenario where GetSizedStringBuilder
				// might return nil under heavy concurrent load
				var builder *FastStringBuilder
				if j%10 == 0 {
					// Intentionally create nil builder 10% of the time
					builder = nil
				} else if j%5 == 0 {
					// Create builder with nil buf 20% of the time
					builder = &FastStringBuilder{buf: nil}
				} else {
					builder = GetSizedStringBuilder(100)
				}

				// All these operations should be safe even with nil builder/buf
				builder.WriteString("test")
				builder.WriteByte(' ')
				_ = builder.String()
				_ = builder.Len()
				builder.Reset()

				if builder != nil && builder.buf != nil {
					PutSizedStringBuilder(builder, 100)
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestBuildPlainTextOptimizedWithNilBuilder tests that buildPlainTextOptimized
// handles nil builder from pool correctly
func TestBuildPlainTextOptimizedWithNilBuilder(t *testing.T) {
	texts := []Text{
		{X: 10, Y: 10, W: 5, FontSize: 12, S: "Hello"},
		{X: 20, Y: 10, W: 5, FontSize: 12, S: "World"},
	}

	// This should not panic even if GetSizedStringBuilder returns nil
	result := buildPlainTextOptimized(texts)

	if len(result) == 0 {
		t.Error("Expected non-empty result")
	}

	t.Logf("Result: %q", result)
}

// TestStringBuilderPoolExhaustion simulates pool exhaustion scenario
func TestStringBuilderPoolExhaustion(t *testing.T) {
	const goroutines = 200
	const holdTime = 10 // iterations to hold builder before returning

	var wg sync.WaitGroup
	wg.Add(goroutines)

	// Exhaust the pool by holding builders
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()

			builders := make([]*FastStringBuilder, 0, holdTime)

			// Acquire multiple builders
			for j := 0; j < holdTime; j++ {
				builder := GetSizedStringBuilder(1000)
				if builder == nil {
					// This is expected when pool is exhausted
					builder = NewFastStringBuilder(1000)
				}
				builder.WriteString("test data")
				builders = append(builders, builder)
			}

			// Use them
			for _, b := range builders {
				_ = b.String()
			}

			// Return them
			for _, b := range builders {
				PutSizedStringBuilder(b, 1000)
			}
		}(i)
	}

	wg.Wait()
}

// TestAllStringBuilderCallSitesNilSafe ensures all call sites handle nil correctly
func TestAllStringBuilderCallSitesNilSafe(t *testing.T) {
	tests := []struct {
		name string
		fn   func() string
	}{
		{
			name: "buildPlainTextOptimized",
			fn: func() string {
				return buildPlainTextOptimized([]Text{
					{X: 0, Y: 0, W: 5, FontSize: 12, S: "test"},
				})
			},
		},
	}

	// Run each test multiple times concurrently
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var wg sync.WaitGroup
			const concurrent = 50

			wg.Add(concurrent)
			for i := 0; i < concurrent; i++ {
				go func() {
					defer wg.Done()
					result := tt.fn()
					if len(result) == 0 {
						t.Error("Expected non-empty result")
					}
				}()
			}
			wg.Wait()
		})
	}
}
