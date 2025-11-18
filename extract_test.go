package pdf

import (
	"io"
	"testing"
)

func TestEmptyReader(t *testing.T) {
	reader := emptyReader()

	// Test reading from empty reader
	buf := make([]byte, 10)
	n, err := reader.Read(buf)

	if n != 0 {
		t.Errorf("Expected to read 0 bytes from empty reader, got %d", n)
	}

	if err != io.EOF {
		t.Errorf("Expected EOF from empty reader, got %v", err)
	}
}

func TestWriteBuffer_Read(t *testing.T) {
	wb := &writeBuffer{}

	// Add some data
	wb.WriteString("Hello")
	wb.WriteString(" ")
	wb.WriteString("World")

	// Read it back
	buf := make([]byte, 20)
	n, err := wb.Read(buf)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if n != 11 { // "Hello World" is 11 bytes
		t.Errorf("Expected to read 11 bytes, got %d", n)
	}

	if string(buf[:n]) != "Hello World" {
		t.Errorf("Expected 'Hello World', got %q", string(buf[:n]))
	}

	// Read again should get EOF
	n2, err2 := wb.Read(buf)
	if n2 != 0 {
		t.Errorf("Expected 0 bytes on second read, got %d", n2)
	}

	if err2 != io.EOF {
		t.Errorf("Expected EOF on second read, got %v", err2)
	}
}

func TestWriteBuffer_Read_Partial(t *testing.T) {
	wb := &writeBuffer{}
	wb.WriteString("Hello World")

	// Read in small chunks
	buf := make([]byte, 5)
	n1, err1 := wb.Read(buf)
	if err1 != nil || n1 != 5 || string(buf[:n1]) != "Hello" {
		t.Errorf("First read failed: n=%d, err=%v, data=%q", n1, err1, string(buf[:n1]))
	}

	n2, err2 := wb.Read(buf)
	if err2 != nil || n2 != 5 || string(buf[:n2]) != " Worl" {
		t.Errorf("Second read failed: n=%d, err=%v, data=%q", n2, err2, string(buf[:n2]))
	}

	n3, err3 := wb.Read(buf)
	if err3 != nil || n3 != 1 || string(buf[:n3]) != "d" {
		t.Errorf("Third read failed: n=%d, err=%v, data=%q", n3, err3, string(buf[:n3]))
	}

	n4, err4 := wb.Read(buf)
	if n4 != 0 || err4 != io.EOF {
		t.Errorf("Fourth read should be EOF: n=%d, err=%v", n4, err4)
	}
}
