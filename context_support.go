// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// ErrContextCancelled is returned when a context is cancelled during PDF processing
var ErrContextCancelled = errors.New("pdf: context cancelled")

// ErrTimeout is returned when processing times out
var ErrTimeout = errors.New("pdf: operation timeout")

// ErrMaxParseTimeExceeded is returned when max parse time is exceeded
var ErrMaxParseTimeExceeded = errors.New("pdf: max parse time exceeded")

// ParseLimits defines resource limits for PDF parsing operations
type ParseLimits struct {
	// MaxParseTime is the maximum time allowed for parsing a single page (0 = no limit)
	MaxParseTime time.Duration

	// MaxHexStringBytes is the maximum size for a single hex string (0 = no limit, default 10MB)
	MaxHexStringBytes int

	// MaxStreamBytes is the maximum size for a single stream (0 = no limit)
	MaxStreamBytes int64

	// CheckInterval specifies how often to check for cancellation during intensive loops
	// Higher values improve performance but reduce responsiveness to cancellation
	// Default: 1000 iterations
	CheckInterval int
}

// DefaultParseLimits returns sensible default limits
func DefaultParseLimits() ParseLimits {
	return ParseLimits{
		MaxParseTime:      45 * time.Second,  // 45 second timeout per page
		MaxHexStringBytes: 100 * 1024 * 1024, // 100MB max hex string
		MaxStreamBytes:    200 * 1024 * 1024, // 200MB max stream
		CheckInterval:     1000,              // Check every 1000 iterations
	}
}

// contextChecker provides efficient context cancellation checking
// It only actually checks the context every N operations to reduce overhead
type contextChecker struct {
	ctx           context.Context
	checkInterval int
	counter       int64
	cancelled     int32 // atomic flag
	deadline      time.Time
	hasDeadline   bool
}

// newContextChecker creates a new context checker
func newContextChecker(ctx context.Context, checkInterval int) *contextChecker {
	if ctx == nil {
		ctx = context.Background()
	}
	if checkInterval <= 0 {
		checkInterval = 1000
	}
	cc := &contextChecker{
		ctx:           ctx,
		checkInterval: checkInterval,
	}
	cc.deadline, cc.hasDeadline = ctx.Deadline()
	return cc
}

// Check returns true if the context is cancelled or deadline exceeded
// This should be called frequently in loops - it's optimized to be cheap
func (cc *contextChecker) Check() bool {
	// Fast path: already known to be cancelled
	if atomic.LoadInt32(&cc.cancelled) != 0 {
		return true
	}

	// Only do the expensive check periodically
	cc.counter++
	if cc.counter%int64(cc.checkInterval) != 0 {
		return false
	}

	// Check deadline first (cheaper than channel select)
	if cc.hasDeadline && time.Now().After(cc.deadline) {
		atomic.StoreInt32(&cc.cancelled, 1)
		return true
	}

	// Check context cancellation
	select {
	case <-cc.ctx.Done():
		atomic.StoreInt32(&cc.cancelled, 1)
		return true
	default:
		return false
	}
}

// CheckNow forces an immediate check regardless of interval
func (cc *contextChecker) CheckNow() bool {
	if atomic.LoadInt32(&cc.cancelled) != 0 {
		return true
	}

	select {
	case <-cc.ctx.Done():
		atomic.StoreInt32(&cc.cancelled, 1)
		return true
	default:
		return false
	}
}

// Err returns the context error if cancelled
func (cc *contextChecker) Err() error {
	if cc.ctx.Err() != nil {
		return cc.ctx.Err()
	}
	if cc.hasDeadline && time.Now().After(cc.deadline) {
		return ErrTimeout
	}
	return nil
}

// parseTimer tracks elapsed time during parsing and enforces limits
type parseTimer struct {
	startTime     time.Time
	maxDuration   time.Duration
	checkCounter  int64
	checkInterval int
	exceeded      int32 // atomic flag
}

// newParseTimer creates a new parse timer with the given max duration
func newParseTimer(maxDuration time.Duration, checkInterval int) *parseTimer {
	if checkInterval <= 0 {
		checkInterval = 1000
	}
	return &parseTimer{
		startTime:     time.Now(),
		maxDuration:   maxDuration,
		checkInterval: checkInterval,
	}
}

// Check returns true if the time limit has been exceeded
// This is optimized to minimize time.Now() calls
func (pt *parseTimer) Check() bool {
	if pt.maxDuration <= 0 {
		return false
	}

	// Fast path: already exceeded
	if atomic.LoadInt32(&pt.exceeded) != 0 {
		return true
	}

	// Only check periodically
	pt.checkCounter++
	if pt.checkCounter%int64(pt.checkInterval) != 0 {
		return false
	}

	if time.Since(pt.startTime) > pt.maxDuration {
		atomic.StoreInt32(&pt.exceeded, 1)
		return true
	}
	return false
}

// Elapsed returns the elapsed time since the timer started
func (pt *parseTimer) Elapsed() time.Duration {
	return time.Since(pt.startTime)
}
