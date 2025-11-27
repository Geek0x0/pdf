// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// TestContextCancellationInGetPlainText verifies that GetPlainText respects context cancellation
func TestContextCancellationInGetPlainText(t *testing.T) {
	// Create a minimal PDF for testing
	data := buildMinimalPDF()
	reader, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	page := reader.Page(1)
	if page.V.IsNull() {
		t.Skip("Page is null")
	}

	// Test with already-cancelled context
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = page.GetPlainText(cancelledCtx, nil)
	if err == nil {
		t.Error("Expected error with cancelled context, got nil")
	}
	if err != context.Canceled {
		t.Logf("Got error: %v (expected context.Canceled)", err)
	}

	// Test with timeout context (should succeed for simple PDF)
	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer timeoutCancel()

	_, err = page.GetPlainText(timeoutCtx, nil)
	if err != nil {
		t.Fatalf("GetPlainText with timeout failed: %v", err)
	}
}

// TestContextCancellationInGetPlainTextWithSmartOrdering verifies smart ordering respects context
func TestContextCancellationInGetPlainTextWithSmartOrdering(t *testing.T) {
	data := buildMinimalPDF()
	reader, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	page := reader.Page(1)
	if page.V.IsNull() {
		t.Skip("Page is null")
	}

	// Test with already-cancelled context
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = page.GetPlainTextWithSmartOrdering(cancelledCtx, nil)
	if err == nil {
		t.Error("Expected error with cancelled context, got nil")
	}
	if err != context.Canceled {
		t.Logf("Got error: %v (expected context.Canceled)", err)
	}
}
