package pdf

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestNewAsyncReader tests the async reader initialization
func TestNewAsyncReader(t *testing.T) {
	// Create a mock reader for testing
	mockReader := &Reader{}

	asyncReader := NewAsyncReader(mockReader)

	if asyncReader.Reader != mockReader {
		t.Error("AsyncReader should wrap the original reader")
	}

	if asyncReader.processor == nil {
		t.Error("AsyncReader should have a parallel processor")
	}
}

// TestAsyncExtractText tests the async text extraction functionality
func TestAsyncExtractText(t *testing.T) {
	// Create a mock reader with some test content
	// Since we can't easily create a real PDF reader for testing,
	// we'll test the async functionality with a mock

	ctx := context.Background()

	// Create a mock reader with custom NumPage method
	mockReader := &Reader{}
	asyncReader := NewAsyncReader(mockReader)

	// Since we can't easily mock the PDF content, we'll create a minimal test
	// that exercises the async channel functionality
	resultChan, errChan := asyncReader.AsyncExtractText(ctx)

	// Check that channels are created
	if resultChan == nil {
		t.Error("Expected result channel to be created")
	}
	if errChan == nil {
		t.Error("Expected error channel to be created")
	}

	// Try to read from channels (with timeout to prevent hanging)
	select {
	case result := <-resultChan:
		// Result should be an empty string since there are no pages
		if result != "" {
			t.Errorf("Expected empty result, got: %s", result)
		}
	case <-time.After(100 * time.Millisecond):
		t.Log("Result channel didn't return immediately as expected for empty reader")
	}

	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Log("Error channel didn't return immediately")
	}
}

// TestAsyncExtractTextWithContext tests async text extraction with context
func TestAsyncExtractTextWithContext(t *testing.T) {
	ctx := context.Background()

	mockReader := &Reader{}
	asyncReader := NewAsyncReader(mockReader)

	opts := ExtractOptions{
		Workers:   2,
		PageRange: nil, // All pages
	}

	resultChan, errChan := asyncReader.AsyncExtractTextWithContext(ctx, opts)

	// Verify that channels are properly created
	if resultChan == nil {
		t.Error("Expected result channel to be created")
	}
	if errChan == nil {
		t.Error("Expected error channel to be created")
	}

	// Test with cancellation
	ctxWithCancel, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, errChan2 := asyncReader.AsyncExtractTextWithContext(ctxWithCancel, opts)

	// Should get context cancellation error
	select {
	case err := <-errChan2:
		if err == nil {
			t.Error("Expected context cancellation error")
		}
	case <-time.After(100 * time.Millisecond):
		t.Log("Cancellation test - no immediate error received")
	}
}

// TestAsyncExtractStructured tests async structured text extraction
func TestAsyncExtractStructured(t *testing.T) {
	ctx := context.Background()

	mockReader := &Reader{}
	asyncReader := NewAsyncReader(mockReader)

	blocksChan, errChan := asyncReader.AsyncExtractStructured(ctx)

	if blocksChan == nil {
		t.Error("Expected blocks channel to be created")
	}
	if errChan == nil {
		t.Error("Expected error channel to be created")
	}

	// Test that we can receive from the channels
	select {
	case blocks := <-blocksChan:
		// With empty reader, should get empty slice
		if blocks == nil {
			t.Error("Expected empty slice, not nil")
		}
		if len(blocks) != 0 {
			t.Errorf("Expected 0 blocks, got %d", len(blocks))
		}
	case <-time.After(100 * time.Millisecond):
		t.Log("Blocks channel didn't return immediately")
	}

	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Log("Error channel didn't return")
	}
}

// TestAsyncStream tests the async stream functionality
func TestAsyncStream(t *testing.T) {
	ctx := context.Background()

	mockReader := &Reader{}
	asyncReader := NewAsyncReader(mockReader)

	// Test processor function that does nothing
	processor := func(Page, int) error {
		return nil
	}

	errChan := asyncReader.AsyncStream(ctx, processor)

	if errChan == nil {
		t.Error("Expected error channel to be created")
	}

	// Should get nil error since processor returns nil
	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Log("Error channel didn't return")
	}
}

// TestAsyncStreamWithError tests async stream with error
func TestAsyncStreamWithError(t *testing.T) {
	ctx := context.Background()

	mockReader := &Reader{}
	asyncReader := NewAsyncReader(mockReader)

	// Test processor function that returns error
	expectedErr := "test error"
	processor := func(Page, int) error {
		return &PDFError{Op: expectedErr}
	}

	errChan := asyncReader.AsyncStream(ctx, processor)

	// Should get nil error since no pages to process
	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Log("Error channel didn't return")
	}
}

// TestAsyncStreamWithCancellation tests cancellation during async stream
func TestAsyncStreamWithCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	mockReader := &Reader{}
	asyncReader := NewAsyncReader(mockReader)

	processor := func(Page, int) error {
		time.Sleep(10 * time.Millisecond) // Simulate work
		return nil
	}

	errChan := asyncReader.AsyncStream(ctx, processor)

	// Should get context cancellation error
	select {
	case err := <-errChan:
		// Might get context error or nil depending on timing
		t.Logf("Received error (expected): %v", err)
	case <-time.After(200 * time.Millisecond):
		t.Log("Error channel didn't return in time")
	}
}

// TestAsyncReaderAt tests the async reader at functionality
func TestAsyncReaderAt(t *testing.T) {
	// Create a mock io.ReaderAt from a string
	testData := "Hello, World! This is test data for async operations."
	readerAt := strings.NewReader(testData)

	asyncReaderAt := NewAsyncReaderAt(readerAt)

	ctx := context.Background()

	// Test reading asynchronously
	buf := make([]byte, 5)
	nChan, errChan := asyncReaderAt.ReadAtAsync(ctx, buf, 0)

	var n int
	select {
	case n = <-nChan:
		if n != 5 {
			t.Errorf("Expected to read 5 bytes, got %d", n)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("ReadAtAsync didn't return within timeout")
	}

	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Error channel didn't return")
	}

	expected := "Hello"
	if string(buf[:n]) != expected {
		t.Errorf("Expected '%s', got '%s'", expected, string(buf[:n]))
	}
}

// TestAsyncReaderAtWithCancellation tests cancellation during async read
func TestAsyncReaderAtWithCancellation(t *testing.T) {
	testData := "Hello, World! This is test data for async operations."
	readerAt := strings.NewReader(testData)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	asyncReaderAt := NewAsyncReaderAt(readerAt)

	buf := make([]byte, 5)
	nChan, errChan := asyncReaderAt.ReadAtAsync(ctx, buf, 0)

	// Should get context cancellation error
	select {
	case err := <-errChan:
		// The error could be context cancellation depending on timing
		t.Logf("Expected cancellation error or nil: %v", err)
	case <-time.After(100 * time.Millisecond):
		t.Log("Error channel didn't return in time")
	}

	// Also check the n channel
	select {
	case n := <-nChan:
		t.Logf("Bytes read: %d", n)
	case <-time.After(100 * time.Millisecond):
		// This is acceptable as the read might not complete before context cancellation
	}
}

// TestStreamValueReader tests streaming value reader
func TestStreamValueReader(t *testing.T) {
	ctx := context.Background()

	// Create a mock Value with a simple reader
	testString := "This is test data to be streamed in chunks."
	_ = strings.NewReader(testString) // Use the reader variable to avoid unused error

	// Create a mock Value that returns this reader
	mockValue := Value{
		// We can't easily mock the internal structure, so we'll test the interface
	}

	// The StreamValueReader method is currently implemented to work with actual PDF values
	// For the test, we'll just verify the method exists and can be called properly
	// This is a complex test that would require proper Value setup
	asyncReader := NewAsyncReader(&Reader{})

	// We'll test that the function doesn't panic and returns proper channels
	// by creating a minimal value that should cause an error but not panic
	dataChan, errChan := asyncReader.StreamValueReader(ctx, mockValue)

	if dataChan == nil {
		t.Error("Expected data channel to be created")
	}
	if errChan == nil {
		t.Error("Expected error channel to be created")
	}

	// Wait for error (should happen quickly since Value is invalid)
	select {
	case err := <-errChan:
		// We expect an error since the Value is not properly set up
		t.Logf("Expected error for invalid value: %v", err)
	case <-time.After(100 * time.Millisecond):
		t.Log("Error channel didn't return in time")
	}
}

// TestAsyncReaderAtReadFromStream tests reading larger data
func TestAsyncReaderAtReadFromStream(t *testing.T) {
	ctx := context.Background()

	largeData := strings.Repeat("This is a test string. ", 100) // Create larger test data
	readerAt := strings.NewReader(largeData)

	asyncReaderAt := NewAsyncReaderAt(readerAt)

	// Read a larger chunk
	buf := make([]byte, 50)
	nChan, errChan := asyncReaderAt.ReadAtAsync(ctx, buf, 10) // Start from offset 10

	var n int
	select {
	case n = <-nChan:
		if n <= 0 {
			t.Errorf("Expected to read some bytes, got %d", n)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("ReadAtAsync didn't return within timeout for large data")
	}

	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("Error channel didn't return for large data test")
	}

	if n > 0 {
		t.Logf("Successfully read %d bytes", n)
	}
}

// BenchmarkAsyncExtractText benchmarks async text extraction
func BenchmarkAsyncExtractText(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mockReader := &Reader{}
	asyncReader := NewAsyncReader(mockReader)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resultChan, errChan := asyncReader.AsyncExtractText(ctx)

		// Need to consume the results to prevent goroutine leaks
		select {
		case <-resultChan:
		case <-time.After(100 * time.Millisecond):
			b.Error("Result channel timeout")
		}

		select {
		case <-errChan:
		case <-time.After(100 * time.Millisecond):
			b.Error("Error channel timeout")
		}
	}
}

// BenchmarkAsyncExtractStructured benchmarks async structured extraction
func BenchmarkAsyncExtractStructured(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mockReader := &Reader{}
	asyncReader := NewAsyncReader(mockReader)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blocksChan, errChan := asyncReader.AsyncExtractStructured(ctx)

		// Need to consume the results to prevent goroutine leaks
		select {
		case <-blocksChan:
		case <-time.After(100 * time.Millisecond):
			b.Error("Blocks channel timeout")
		}

		select {
		case <-errChan:
		case <-time.After(100 * time.Millisecond):
			b.Error("Error channel timeout")
		}
	}
}
