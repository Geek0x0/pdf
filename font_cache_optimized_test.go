package pdf

import (
	"testing"
)

func TestNewOptimizedFontCache(t *testing.T) {
	cache := NewOptimizedFontCache(1000)

	if cache == nil {
		t.Error("Expected non-nil cache")
	}

	if len(cache.shards) != 16 {
		t.Errorf("Expected 16 shards, got %d", len(cache.shards))
	}
}

func TestOptimizedFontCacheGetSet(t *testing.T) {
	cache := NewOptimizedFontCache(1000)

	font := &Font{}

	// Test Set
	cache.Set("test_key", font)

	// Test Get
	retrieved, ok := cache.Get("test_key")

	if !ok {
		t.Error("Expected to find the key")
	}

	if retrieved != font {
		t.Error("Retrieved font is not the same")
	}
}

func TestOptimizedFontCacheGetStats(t *testing.T) {
	cache := NewOptimizedFontCache(1000)

	stats := cache.GetStats()

	// Just check it doesn't crash
	_ = stats
}

func TestOptimizedFontCacheClear(t *testing.T) {
	cache := NewOptimizedFontCache(1000)

	font := &Font{}
	cache.Set("test_key", font)

	cache.Clear()

	_, ok := cache.Get("test_key")
	if ok {
		t.Error("Expected key to be cleared")
	}
}
