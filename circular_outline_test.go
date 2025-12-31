package pdf

import (
	"testing"
)

// TestCircularOutlineReference tests that circular outline references don't cause stack overflow
func TestCircularOutlineReference(t *testing.T) {
	// Create a reader with circular outline structure
	// Outline1 -> First: Outline2, Outline2 -> Next: Outline1 (circular)
	outline1 := dict{
		name("Title"): "Outline1",
	}
	outline2 := dict{
		name("Title"): "Outline2",
		name("Next"):  outline1, // This would create a cycle
	}
	outline1[name("First")] = outline2

	r := &Reader{
		trailer: dict{
			name("Root"): dict{
				name("Outlines"): outline1,
			},
		},
	}

	// This should not panic or cause stack overflow
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Circular outline caused panic: %v", r)
		}
	}()

	outline := r.Outline()

	// Should succeed without stack overflow
	t.Logf("Successfully extracted outline with title: %s, children: %d", outline.Title, len(outline.Child))
}

// TestDeepOutlineNesting tests that deeply nested outlines are handled safely
func TestDeepOutlineNesting(t *testing.T) {
	// Create a reader with very deep nesting (beyond maxOutlineDepth)
	// Create a chain: root -> child1 -> child2 -> ... -> child150
	current := dict{
		name("Title"): "Leaf",
	}

	// Build a deep chain
	for i := 150; i > 0; i-- {
		parent := dict{
			name("Title"): "Level" + string(rune('0'+i%10)),
			name("First"): current,
		}
		current = parent
	}

	r := &Reader{
		trailer: dict{
			name("Root"): dict{
				name("Outlines"): current,
			},
		},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Deep outline nesting caused panic: %v", r)
		}
	}()

	outline := r.Outline()

	// Should succeed - extraction stops at max depth
	t.Logf("Successfully handled deep nesting, title: %s", outline.Title)

	// Count actual depth
	depth := 0
	curr := &outline
	for len(curr.Child) > 0 {
		depth++
		curr = &curr.Child[0]
		if depth > maxOutlineDepth+5 {
			t.Fatalf("Depth exceeded max: %d", depth)
		}
	}
	t.Logf("Actual depth: %d (max allowed: %d)", depth, maxOutlineDepth)
}

// TestManySiblingOutlines tests handling of many sibling outlines
func TestManySiblingOutlines(t *testing.T) {
	// Create an outline with 2000 siblings to test the sibling limit
	var first dict
	var prev dict

	for i := 0; i < 2000; i++ {
		current := dict{
			name("Title"): "Sibling",
		}
		if i == 0 {
			first = current
		} else {
			prev[name("Next")] = current
		}
		prev = current
	}

	r := &Reader{
		trailer: dict{
			name("Root"): dict{
				name("Outlines"): dict{
					name("Title"): "Root",
					name("First"): first,
				},
			},
		},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Many siblings caused panic: %v", r)
		}
	}()

	outline := r.Outline()

	// Should succeed and limit siblings to prevent excessive memory
	if len(outline.Child) > 1000 {
		t.Fatalf("Too many children extracted: %d", len(outline.Child))
	}
	t.Logf("Successfully handled %d siblings (capped at safety limit)", len(outline.Child))
}

// TestNormalOutlineStructure tests that normal outline structures still work correctly
func TestNormalOutlineStructure(t *testing.T) {
	// Create a normal outline structure
	r := &Reader{
		trailer: dict{
			name("Root"): dict{
				name("Outlines"): dict{
					name("Title"): "Root",
					name("First"): dict{
						name("Title"): "Chapter 1",
						name("Next"): dict{
							name("Title"): "Chapter 2",
							name("First"): dict{
								name("Title"): "Section 2.1",
								name("Next"): dict{
									name("Title"): "Section 2.2",
								},
							},
						},
					},
				},
			},
		},
	}

	outline := r.Outline()

	if outline.Title != "Root" {
		t.Errorf("Root title = %q, want %q", outline.Title, "Root")
	}

	if len(outline.Child) != 2 {
		t.Errorf("Root children = %d, want 2", len(outline.Child))
	}

	if outline.Child[0].Title != "Chapter 1" {
		t.Errorf("First child title = %q, want %q", outline.Child[0].Title, "Chapter 1")
	}

	if outline.Child[1].Title != "Chapter 2" {
		t.Errorf("Second child title = %q, want %q", outline.Child[1].Title, "Chapter 2")
	}

	if len(outline.Child[1].Child) != 2 {
		t.Errorf("Chapter 2 children = %d, want 2", len(outline.Child[1].Child))
	}

	t.Logf("Normal outline structure works correctly")
}
