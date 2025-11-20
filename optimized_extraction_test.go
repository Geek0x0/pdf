package pdf

import (
	"testing"
)

func TestOptimizedTextRunsToPlain(t *testing.T) {
	texts := []Text{
		{S: "hello", X: 100, Y: 200},
		{S: "world", X: 120, Y: 200},
	}

	plain := optimizedTextRunsToPlain(texts)

	if plain != "hello world" {
		t.Errorf("Expected 'hello world', got '%s'", plain)
	}
}

func TestOptimizedTextRunsToPlainEmpty(t *testing.T) {
	texts := []Text{}
	plain := optimizedTextRunsToPlain(texts)

	if plain != "" {
		t.Errorf("Expected empty string, got '%s'", plain)
	}
}

func TestOptimizedTextRunsToPlainSingle(t *testing.T) {
	texts := []Text{{S: "single", X: 100, Y: 200}}
	plain := optimizedTextRunsToPlain(texts)

	if plain != "single" {
		t.Errorf("Expected 'single', got '%s'", plain)
	}
}
