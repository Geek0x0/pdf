// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"testing"
)

func TestStack_Len(t *testing.T) {
	stk := &Stack{}

	if stk.Len() != 0 {
		t.Errorf("Expected empty stack length 0, got %d", stk.Len())
	}

	stk.Push(Value{})
	if stk.Len() != 1 {
		t.Errorf("Expected stack length 1 after push, got %d", stk.Len())
	}
}

func TestStack_PushPop(t *testing.T) {
	stk := &Stack{}

	// Test push
	v1 := Value{}
	stk.Push(v1)

	if stk.Len() != 1 {
		t.Errorf("Expected length 1 after push, got %d", stk.Len())
	}

	// Test pop
	stk.Pop()
	if stk.Len() != 0 {
		t.Errorf("Expected length 0 after pop, got %d", stk.Len())
	}

	// Test pop from empty stack
	emptyPop := stk.Pop()
	if emptyPop != (Value{}) {
		t.Errorf("Expected empty Value from empty stack pop, got %v", emptyPop)
	}
}

func TestNewDict(t *testing.T) {
	d := newDict()

	if d.Kind() != Dict {
		t.Errorf("Expected Dict kind, got %v", d.Kind())
	}
}
