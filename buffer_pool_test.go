// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"strings"
	"sync"
	"testing"
)

// TestBufferPoolConcurrency tests that the buffer pool is safe under concurrent access
// This test is designed to catch double-Put bugs and race conditions
func TestBufferPoolConcurrency(t *testing.T) {
	const numGoroutines = 100
	const iterations = 100

	var wg sync.WaitGroup

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for i := 0; i < iterations; i++ {
				// Simulate the pattern in tryRecoverFromOffset116
				r := strings.NewReader("test data for buffer pool concurrency test")
				b := newBuffer(r, 0)

				// Read some data
				_ = b.readByte()

				// Put buffer back to pool
				// Note: After Put, buffer must not be accessed (including checking b.r)
				// as it may be reused by another goroutine
				PutPDFBuffer(b)
			}
		}()
	}

	wg.Wait()
}

// TestBufferPoolNilReader tests that using a buffer after Put causes predictable behavior
func TestBufferPoolNilReader(t *testing.T) {
	r := strings.NewReader("test data")
	b := newBuffer(r, 0)

	// Read a byte successfully
	c := b.readByte()
	if c != 't' {
		t.Errorf("Expected 't', got %c", c)
	}

	// Put buffer back
	PutPDFBuffer(b)

	// Verify r is nil
	if b.r != nil {
		t.Errorf("Expected b.r to be nil after Put, got %v", b.r)
	}

	// Note: We cannot test calling readByte() after Put because it would panic
	// This is the expected behavior - buffers must not be used after being returned to pool
}

// TestTryRecoverFromOffset116DoublePut tests for the specific double-Put bug fix
func TestTryRecoverFromOffset116DoublePut(t *testing.T) {
	// This test verifies that buffers are not double-Put in the recovery loop
	// We can't easily test the exact scenario without a corrupted PDF,
	// but we can verify the pool behaves correctly under stress

	const concurrency = 50
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Simulate rapid Get/Put cycles that could expose double-Put
			for j := 0; j < 100; j++ {
				r := strings.NewReader("simulated PDF data for concurrent buffer pool stress test")
				b := newBuffer(r, 0)

				// Simulate some reads
				for k := 0; k < 10; k++ {
					if b.pos >= len(b.buf) {
						if !b.reload() {
							break
						}
					}
					_ = b.readByte()
				}

				// Put back
				PutPDFBuffer(b)
			}
		}(i)
	}

	wg.Wait()
}

// TestBufferPoolReuse tests that buffers are properly reused from the pool
func TestBufferPoolReuse(t *testing.T) {
	// Get a buffer and note some property
	r1 := strings.NewReader("first")
	b1 := newBuffer(r1, 0)
	initialCap := cap(b1.buf)

	// Put it back
	PutPDFBuffer(b1)

	// Get another buffer - might be the same one from the pool
	r2 := strings.NewReader("second")
	b2 := newBuffer(r2, 0)

	// Verify it's properly initialized
	if b2.r == nil {
		t.Error("Expected b2.r to be set after newBuffer, got nil")
	}

	// Verify capacity is preserved (buffers are reused)
	if cap(b2.buf) != initialCap {
		// This is not an error - pool might create new buffer
		// But typically it should reuse
		t.Logf("Buffer capacity changed: %d -> %d (may indicate new buffer)", initialCap, cap(b2.buf))
	}

	// Read should work
	c := b2.readByte()
	if c != 's' {
		t.Errorf("Expected 's', got %c", c)
	}

	PutPDFBuffer(b2)
}

// TestNewBufferAlwaysSetsReader tests that newBuffer always sets b.r
func TestNewBufferAlwaysSetsReader(t *testing.T) {
	testCases := []struct {
		name   string
		data   string
		offset int64
	}{
		{"strings.Reader", "test", 0},
		{"strings.Reader with offset", "test", 10},
		{"empty reader", "", 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reader := strings.NewReader(tc.data)
			b := newBuffer(reader, tc.offset)

			if b.r == nil {
				t.Error("newBuffer returned buffer with nil reader")
			}

			if b.r != reader {
				t.Errorf("Expected reader %v, got %v", reader, b.r)
			}

			if b.offset != tc.offset {
				t.Errorf("Expected offset %d, got %d", tc.offset, b.offset)
			}

			PutPDFBuffer(b)
		})
	}
}
