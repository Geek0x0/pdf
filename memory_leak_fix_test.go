// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"runtime"
	"testing"
	"time"
)

// TestPageCleanup verifies that Page.Cleanup() properly releases resources
func TestPageCleanup(t *testing.T) {
	// Create a mock page
	page := Page{
		V: Value{},
	}

	// Set a font cache
	cache := NewGlobalFontCache(100, 0)
	page.SetFontCacheInterface(cache)

	// Verify cache is set
	if page.fontCache == nil {
		t.Error("fontCache should be set")
	}

	// Cleanup
	page.Cleanup()

	// Verify cache is cleared
	if page.fontCache != nil {
		t.Error("fontCache should be nil after Cleanup()")
	}

	// Cleanup should be idempotent
	page.Cleanup()
	if page.fontCache != nil {
		t.Error("fontCache should still be nil after second Cleanup()")
	}
}

// TestFontScopeCleanup verifies that font scope cleanup happens in contentWithFonts
func TestFontScopeCleanup(t *testing.T) {
	// This is an indirect test - we verify that processing doesn't leak memory

	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	baseline := m1.Alloc

	// Simulate multiple content extractions
	for i := 0; i < 100; i++ {
		page := Page{V: Value{}}
		fonts := make(map[string]*Font)

		// Call contentWithFonts (which should cleanup scope)
		_, _ = page.contentWithFonts(fonts)
	}

	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	afterProcessing := m2.Alloc

	growth := int64(afterProcessing) - int64(baseline)
	growthMB := growth / 1024 / 1024

	t.Logf("Memory growth after 100 contentWithFonts calls: %d MB", growthMB)

	if growthMB > 10 {
		t.Errorf("Possible memory leak in contentWithFonts: grew by %d MB", growthMB)
	}
}
