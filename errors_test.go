package pdf

import (
	"errors"
	"testing"
)

func TestPDFError_Error(t *testing.T) {
	err := &PDFError{
		Op:   "test operation",
		Page: 5,
		Err:  errors.New("underlying error"),
	}

	result := err.Error()
	if result == "" {
		t.Error("Expected non-empty error string")
	}

	// Check that operation is included
	if !contains(result, "test operation") {
		t.Errorf("Expected error to contain operation, got: %s", result)
	}

	// Check that page is included
	if !contains(result, "5") {
		t.Errorf("Expected error to contain page number, got: %s", result)
	}
}

func TestPDFError_Unwrap(t *testing.T) {
	underlying := errors.New("underlying error")
	err := &PDFError{
		Op:   "test operation",
		Page: 5,
		Err:  underlying,
	}

	unwrapped := err.Unwrap()
	if unwrapped != underlying {
		t.Errorf("Expected Unwrap to return underlying error, got %v", unwrapped)
	}
}

func TestWrapPageError(t *testing.T) {
	underlying := errors.New("underlying error")

	result := wrapPageError("test operation", 10, underlying)

	pdfErr, ok := result.(*PDFError)
	if !ok {
		t.Errorf("Expected wrapPageError to return PDFError, got %T", result)
	}

	if pdfErr.Op != "test operation" {
		t.Errorf("Expected operation 'test operation', got %q", pdfErr.Op)
	}

	if pdfErr.Page != 10 {
		t.Errorf("Expected page 10, got %d", pdfErr.Page)
	}

	if pdfErr.Err != underlying {
		t.Errorf("Expected underlying error to be preserved")
	}
}

func TestWrapPageError_NilError(t *testing.T) {
	result := wrapPageError("test operation", 10, nil)

	if result != nil {
		t.Errorf("Expected wrapPageError with nil error to return nil, got %v", result)
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || containsAt(s, substr, 0))
}

func containsAt(s, substr string, pos int) bool {
	if pos+len(substr) > len(s) {
		return false
	}
	for i := 0; i < len(substr); i++ {
		if s[pos+i] != substr[i] {
			return containsAt(s, substr, pos+1)
		}
	}
	return true
}
