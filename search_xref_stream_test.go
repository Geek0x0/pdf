package pdf

import (
	"bytes"
	"reflect"
	"testing"
)

func TestFindXRefStreamPositionsAllowsWhitespace(t *testing.T) {
	data := []byte("start /Type\n/XRef mid /Type /Page end /Type \r\n /XRef trailer")

	got := findXRefStreamPositions(data)

	first := bytes.Index(data, []byte("/Type\n/XRef"))
	secondRel := bytes.Index(data[first+1:], []byte("/Type \r\n /XRef"))
	if first < 0 || secondRel < 0 {
		t.Fatalf("test data malformed, could not locate markers")
	}
	second := first + 1 + secondRel

	want := []int{first, second}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected positions: got %v, want %v", got, want)
	}
}
