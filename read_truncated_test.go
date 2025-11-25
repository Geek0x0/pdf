package pdf

import (
	"bytes"
	"testing"
)

// 确认截断的 xref 不会触发 panic，而是返回可处理的错误。
func TestNewReaderTruncatedXrefNoPanic(t *testing.T) {
	data := []byte("%PDF-1.4\nxref\n0 1\n") // 故意截断的 xref 表
	data = append(data, []byte("startxref\n0\n%%EOF")...)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()

	_, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err == nil {
		t.Fatalf("expected error for truncated xref")
	}
}
