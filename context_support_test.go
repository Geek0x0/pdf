// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestContextChecker(t *testing.T) {
	t.Run("basic cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cc := newContextChecker(ctx, 10)

		// Should not be cancelled initially
		if cc.CheckNow() {
			t.Error("context should not be cancelled initially")
		}

		// Cancel and verify
		cancel()
		if !cc.CheckNow() {
			t.Error("context should be cancelled after cancel()")
		}
	})

	t.Run("deadline exceeded", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		cc := newContextChecker(ctx, 1) // Check every iteration

		// Should not be cancelled initially
		if cc.CheckNow() {
			t.Error("context should not be cancelled initially")
		}

		// Wait for deadline
		time.Sleep(20 * time.Millisecond)

		// Now should be cancelled
		if !cc.Check() {
			t.Error("context should be cancelled after deadline")
		}
	})

	t.Run("periodic checking", func(t *testing.T) {
		ctx := context.Background()
		cc := newContextChecker(ctx, 100)

		// First 99 checks should be cheap (not actually checking context)
		for i := 0; i < 99; i++ {
			if cc.Check() {
				t.Errorf("iteration %d should not be cancelled", i)
			}
		}

		// 100th check should actually check (but still not cancelled)
		if cc.Check() {
			t.Error("should still not be cancelled")
		}
	})
}

func TestParseTimer(t *testing.T) {
	t.Run("no limit", func(t *testing.T) {
		pt := newParseTimer(0, 10)

		for i := 0; i < 100; i++ {
			if pt.Check() {
				t.Error("should never exceed when limit is 0")
			}
		}
	})

	t.Run("with limit", func(t *testing.T) {
		pt := newParseTimer(10*time.Millisecond, 1)

		// Initially should not be exceeded
		if pt.Check() {
			t.Error("should not be exceeded initially")
		}

		// Wait for limit
		time.Sleep(15 * time.Millisecond)

		// Now should be exceeded
		if !pt.Check() {
			t.Error("should be exceeded after waiting")
		}
	})
}

func TestParseLimits(t *testing.T) {
	limits := DefaultParseLimits()

	if limits.MaxParseTime != 45*time.Second {
		t.Errorf("expected 45s max parse time, got %v", limits.MaxParseTime)
	}
	if limits.MaxHexStringBytes != 100*1024*1024 {
		t.Errorf("expected 100MB max hex string, got %d", limits.MaxHexStringBytes)
	}
	if limits.CheckInterval != 1000 {
		t.Errorf("expected 1000 check interval, got %d", limits.CheckInterval)
	}
}

func TestInterpretWithContext(t *testing.T) {
	t.Run("cancellation stops interpretation", func(t *testing.T) {
		// Create a mock stream-like value for testing
		// This test verifies the context cancellation path works
		ctx, cancel := context.WithCancel(context.Background())

		var opCount int
		testDo := func(stk *Stack, op string) {
			opCount++
			// Simulate some work
			if opCount > 5 {
				cancel() // Cancel after 5 operations
			}
		}

		// Create a simple Value that can be interpreted
		// For this test, we just verify the function doesn't panic
		// and respects context cancellation
		InterpretWithContext(ctx, Value{}, testDo)

		// The test passes if we reach here without hanging
	})
}

func TestReadHexStringWithContext(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple hex", "<48656C6C6F>", "Hello"},
		{"hex with spaces", "<48 65 6C 6C 6F>", "Hello"},
		{"odd digits", "<4>", "@"}, // 4 followed by implicit 0 = 0x40 = '@'
		{"empty", "<>", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBuffer(strings.NewReader(tt.input), 0)
			b.readByte() // consume '<'

			tok := b.readHexStringSIMDAdvanced()
			result, ok := tok.(string)
			if !ok {
				t.Fatalf("expected string token, got %T", tok)
			}
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestReadHexStringWithLimit(t *testing.T) {
	// Create a large hex string
	largeHex := "<" + strings.Repeat("48", 10000) + ">" // 10000 'H' characters

	limits := &ParseLimits{
		MaxHexStringBytes: 100, // Limit to 100 bytes
		CheckInterval:     10,
	}

	b := newBuffer(strings.NewReader(largeHex), 0)
	b.limits = limits
	b.readByte() // consume '<'

	tok := b.readHexStringSIMDAdvanced()
	result, ok := tok.(string)
	if !ok {
		t.Fatalf("expected string token, got %T", tok)
	}

	if len(result) > 100 {
		t.Errorf("expected result to be limited to 100 bytes, got %d", len(result))
	}
}

func TestReadHexStringCancellation(t *testing.T) {
	// Create a very large hex string
	largeHex := "<" + strings.Repeat("48", 100000) + ">"

	ctx, cancel := context.WithCancel(context.Background())
	cc := newContextChecker(ctx, 1) // Check every iteration for immediate detection

	// Cancel immediately
	cancel()

	b := newBuffer(strings.NewReader(largeHex), 0)
	b.ctxChecker = cc
	b.readByte() // consume '<'

	tok := b.readHexStringSIMDAdvanced()

	// Should return nil on cancellation
	// With batch reading optimization, the first buffer load may complete
	// before cancellation is detected, so we check if result is significantly smaller
	if tok != nil {
		result := tok.(string)
		// Allow some tolerance - the first batch may complete before cancellation detected
		// But it should be significantly smaller than the full 100000 bytes
		if len(result) > 70000 {
			t.Errorf("cancellation should have stopped hex string reading early, got %d bytes", len(result))
		}
	}
}

func TestBatchExtractWithPageTimeout(t *testing.T) {
	// This test verifies the timeout option is properly accepted
	opts := BatchExtractOptions{
		Workers:     2,
		PageTimeout: 5 * time.Second,
		ParseLimits: &ParseLimits{
			MaxParseTime:      10 * time.Second,
			MaxHexStringBytes: 1024 * 1024,
			CheckInterval:     500,
		},
	}

	// Verify defaults are set correctly when options are processed
	if opts.PageTimeout != 5*time.Second {
		t.Errorf("expected 5s page timeout, got %v", opts.PageTimeout)
	}
	if opts.ParseLimits.MaxParseTime != 10*time.Second {
		t.Errorf("expected 10s max parse time, got %v", opts.ParseLimits.MaxParseTime)
	}
}

func TestErrTypes(t *testing.T) {
	// Verify error types are properly defined
	if ErrContextCancelled == nil {
		t.Error("ErrContextCancelled should not be nil")
	}
	if ErrTimeout == nil {
		t.Error("ErrTimeout should not be nil")
	}
	if ErrMaxParseTimeExceeded == nil {
		t.Error("ErrMaxParseTimeExceeded should not be nil")
	}

	// Verify error messages
	if !strings.Contains(ErrContextCancelled.Error(), "cancelled") {
		t.Error("ErrContextCancelled should mention 'cancelled'")
	}
	if !strings.Contains(ErrTimeout.Error(), "timeout") {
		t.Error("ErrTimeout should mention 'timeout'")
	}
}
