package pdf

import "testing"

func TestIdentityCMapDecode(t *testing.T) {
	enc := &identityCMap{width: 2}
	got := enc.Decode(string([]byte{0x00, 0x41, 0x00, 0x42}))
	if got != "AB" {
		t.Fatalf("Decode mismatch: got %q, want %q", got, "AB")
	}

	enc = &identityCMap{width: 1}
	got = enc.Decode(string([]byte{0x61, 0x62}))
	if got != "ab" {
		t.Fatalf("Decode mismatch for width=1: got %q, want %q", got, "ab")
	}
}
