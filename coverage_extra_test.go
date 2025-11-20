package pdf

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestToLatin1AndNameMap(t *testing.T) {
	s := "ASCII and 漢"
	b := toLatin1(s)
	if len(b) == 0 {
		t.Fatalf("toLatin1 returned empty")
	}
	// check that a known name exists in the map
	if r, ok := nameToRune["Euro"]; !ok || r != 0x20AC {
		t.Fatalf("nameToRune missing Euro entry")
	}
}

func TestFontCacheSetGet(t *testing.T) {
	fc := NewFontCache()
	if fc == nil {
		t.Fatal("NewFontCache returned nil")
	}
	var f *Font
	fc.Set("k", f)
	if got, ok := fc.Get("k"); !ok || got != nil {
		t.Fatalf("FontCache Get/Set failed: got=%v ok=%v", got, ok)
	}
}

func TestErrorsHelpers(t *testing.T) {
	e := wrapError("op", errors.New("boom"))
	if e == nil {
		t.Fatal("wrapError returned nil")
	}
	if pe, ok := e.(*PDFError); ok {
		if pe.Error() == "" {
			t.Fatal("PDFError.Error empty")
		}
	} else {
		t.Fatalf("wrapError returned wrong type: %T", e)
	}

	pe := wrapPageError("op2", 3, errors.New("whoops"))
	if pe == nil {
		t.Fatal("wrapPageError returned nil")
	}
}

func TestAlphaReaderCheck(t *testing.T) {
	// construct data containing ascii85 chars, a '~>' sequence and invalid chars
	src := []byte("!!!~>garbage")
	r := newAlphaReader(bytes.NewReader(src))
	buf := make([]byte, len(src))
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("alphaReader Read unexpected error: %v", err)
	}
	if n == 0 {
		t.Fatalf("alphaReader Read returned 0 bytes")
	}
}

func TestReaderCacheAndEvict(t *testing.T) {
	r := &Reader{}
	// ensure cache structures are initialized via storeCachedObject
	ptr := objptr{id: 1, gen: 0}
	var o object = nil
	r.storeCachedObject(ptr, o)
	// storing nil should be ignored
	if _, ok := r.getCachedObject(ptr); ok {
		t.Fatalf("expected no cached object for nil value")
	}

	// store a non-nil dummy object
	dummy := &struct{ s string }{s: "x"}
	var objAsObject object = dummy
	r.storeCachedObject(ptr, objAsObject)
	if got, ok := r.getCachedObject(ptr); !ok || got == nil {
		t.Fatalf("expected cached object present got=%v ok=%v", got, ok)
	}

	// set capacity to 0 and ensure evictOldest handles empty list
	r.SetCacheCapacity(0)
	r.evictOldest()

	// set small capacity and add more entries to trigger eviction
	r.SetCacheCapacity(1)
	ptr2 := objptr{id: 2, gen: 0}
	r.storeCachedObject(ptr2, objAsObject)
	// after adding second entry with cap=1, oldest should be evicted
	if _, ok := r.getCachedObject(ptr); ok {
		t.Fatalf("expected first entry evicted")
	}
}
