package pdf

import (
	"bytes"
	"compress/lzw"
	"io"
	"testing"
)

func TestRunLengthDecode(t *testing.T) {
	// literal run of 3 bytes followed by repeat run of 2 bytes
	src := []byte{2, 'A', 'B', 'C', 255, 'Z', 128}
	rd := applyFilter(bytes.NewReader(src), "RunLengthDecode", Value{})
	out, err := io.ReadAll(rd)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(out) != "ABCZZ" {
		t.Fatalf("unexpected decode: %q", string(out))
	}
}

func TestLZWDecode(t *testing.T) {
	var buf bytes.Buffer
	w := lzw.NewWriter(&buf, lzw.MSB, 8)
	testString := "Hello, PDF!"
	if _, err := w.Write([]byte(testString)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	rd := applyFilter(bytes.NewReader(buf.Bytes()), "LZWDecode", Value{})
	out, err := io.ReadAll(rd)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(out) != testString {
		t.Fatalf("unexpected decode: %q", string(out))
	}
}

func TestPassthroughFilters(t *testing.T) {
	sample := []byte{0x01, 0x02, 0x03}
	tests := []string{"DCTDecode", "JPXDecode", "CCITTFaxDecode"}
	for _, name := range tests {
		rd := applyFilter(bytes.NewReader(sample), name, Value{})
		out, err := io.ReadAll(rd)
		if err != nil {
			t.Fatalf("%s read failed: %v", name, err)
		}
		if !bytes.Equal(out, sample) {
			t.Fatalf("%s altered data", name)
		}
	}
}
