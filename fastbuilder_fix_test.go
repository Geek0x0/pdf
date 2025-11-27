// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"sync"
	"testing"
)

// TestFastStringBuilderNilSafety tests that FastStringBuilder handles nil cases safely
func TestFastStringBuilderNilSafety(t *testing.T) {
	// Test 1: nil builder
	var nilBuilder *FastStringBuilder
	nilBuilder.WriteString("test") // Should not panic
	nilBuilder.Reset()             // Should not panic

	// Test 2: builder with nil buf
	builder := &FastStringBuilder{buf: nil}
	builder.WriteString("test") // Should not panic, should initialize buf
	if builder.buf == nil {
		t.Error("Expected buf to be initialized after WriteString")
	}
	if len(builder.buf) == 0 {
		t.Error("Expected buf to contain data")
	}

	// Test 3: Reset with nil buf
	builder2 := &FastStringBuilder{buf: nil}
	builder2.Reset() // Should not panic, should initialize buf
	if builder2.buf == nil {
		t.Error("Expected buf to be initialized after Reset")
	}
}

// TestGetSizedStringBuilderNeverReturnsNil tests that pool always returns valid builder
func TestGetSizedStringBuilderNeverReturnsNil(t *testing.T) {
	sizes := []int{0, 100, 1024, 10000, 100000}

	for _, size := range sizes {
		builder := GetSizedStringBuilder(size)
		if builder == nil {
			t.Errorf("GetSizedStringBuilder(%d) returned nil", size)
			continue
		}
		if builder.buf == nil {
			t.Errorf("GetSizedStringBuilder(%d) returned builder with nil buf", size)
			continue
		}

		// Use it
		builder.WriteString("test")
		if builder.String() != "test" {
			t.Errorf("Expected 'test', got '%s'", builder.String())
		}

		// Return to pool
		PutSizedStringBuilder(builder, size)
	}
}

// TestFastStringBuilderConcurrency tests concurrent use of string builder pool
func TestFastStringBuilderConcurrency(t *testing.T) {
	const numGoroutines = 100
	const iterations = 100

	var wg sync.WaitGroup

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for i := 0; i < iterations; i++ {
				// Get from pool
				builder := GetSizedStringBuilder(1000)
				if builder == nil {
					t.Errorf("Goroutine %d: GetSizedStringBuilder returned nil", id)
					return
				}

				// Use it
				for j := 0; j < 10; j++ {
					builder.WriteString("test")
				}

				result := builder.String()
				if len(result) == 0 {
					t.Errorf("Goroutine %d: Builder produced empty string", id)
				}

				// Return to pool
				PutSizedStringBuilder(builder, 1000)
			}
		}(g)
	}

	wg.Wait()
}

// TestPutSizedStringBuilderNilSafety tests that Put handles nil gracefully
func TestPutSizedStringBuilderNilSafety(t *testing.T) {
	// Should not panic
	PutSizedStringBuilder(nil, 0)
	PutSizedStringBuilder(nil, 1000)
	PutSizedStringBuilder(nil, 100000)
}
