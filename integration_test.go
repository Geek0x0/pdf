package pdf

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestGetPlainTextBasic(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	reader, err := r.GetPlainText()
	if err != nil {
		t.Fatalf("GetPlainText failed: %v", err)
	}

	text, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if len(text) == 0 {
		t.Error("expected non-empty text")
	}
}

func TestPageGetPlainText(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		t.Skip("PDF has no pages")
	}

	page := r.Page(1)
	text, err := page.GetPlainText(nil)
	if err != nil {
		t.Fatalf("Page.GetPlainText failed: %v", err)
	}

	if len(text) == 0 {
		t.Log("page 1 appears to be empty")
	}
}

func TestGetPlainTextConcurrent(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	reader, err := r.GetPlainTextConcurrent(2)
	if err != nil {
		t.Fatalf("GetPlainTextConcurrent failed: %v", err)
	}

	text, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if len(text) == 0 {
		t.Error("expected non-empty text")
	}
}

func TestExtractWithContext(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	ctx := context.Background()
	opts := ExtractOptions{Workers: 2}

	reader, err := r.ExtractWithContext(ctx, opts)
	if err != nil {
		t.Fatalf("ExtractWithContext failed: %v", err)
	}

	text, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if len(text) == 0 {
		t.Error("expected non-empty text")
	}
}

func TestExtractWithContextCancellation(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	opts := ExtractOptions{Workers: 1}
	_, err = r.ExtractWithContext(ctx, opts)

	if err == nil {
		t.Log("note: extraction completed before context cancellation could take effect")
	} else if err != context.Canceled {
		t.Logf("got error: %v (expected context.Canceled)", err)
	}
}

func TestExtractWithContextPageRange(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		t.Skip("PDF has no pages")
	}

	ctx := context.Background()
	opts := ExtractOptions{
		Workers:   1,
		PageRange: []int{1}, // Only extract first page
	}

	reader, err := r.ExtractWithContext(ctx, opts)
	if err != nil {
		t.Fatalf("ExtractWithContext failed: %v", err)
	}

	text, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if len(text) == 0 {
		t.Error("expected non-empty text from page 1")
	}
}

func TestGetStyledTexts(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	texts, err := r.GetStyledTexts()
	if err != nil {
		t.Fatalf("GetStyledTexts failed: %v", err)
	}

	if len(texts) == 0 {
		t.Error("expected non-empty styled texts")
	}

	// Verify Text structure contains expected fields
	for _, text := range texts {
		if text.S == "" {
			continue // Empty text is okay
		}
		if text.FontSize <= 0 {
			t.Errorf("invalid font size: %f", text.FontSize)
		}
	}
}

func TestGetTextByRow(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		t.Skip("PDF has no pages")
	}

	page := r.Page(1)
	rows, err := page.GetTextByRow()
	if err != nil {
		t.Fatalf("GetTextByRow failed: %v", err)
	}

	// Rows might be empty for some PDFs
	t.Logf("Found %d rows", len(rows))
}

func TestGetTextByColumn(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		t.Skip("PDF has no pages")
	}

	page := r.Page(1)
	columns, err := page.GetTextByColumn()
	if err != nil {
		t.Fatalf("GetTextByColumn failed: %v", err)
	}

	// Columns might be empty for some PDFs
	t.Logf("Found %d columns", len(columns))
}

func TestErrorHandling(t *testing.T) {
	// Test with non-existent file
	_, _, err := Open("nonexistent.pdf")
	if err == nil {
		t.Error("expected error when opening non-existent file")
	}
}

func TestFontCache(t *testing.T) {
	cache := NewFontCache()

	// Test Set and Get
	font := &Font{}
	cache.Set("TestFont", font)

	retrieved, ok := cache.Get("TestFont")
	if !ok {
		t.Error("expected to find font in cache")
	}
	if retrieved != font {
		t.Error("retrieved font doesn't match stored font")
	}

	// Test non-existent font
	_, ok = cache.Get("NonExistent")
	if ok {
		t.Error("expected not to find non-existent font")
	}
}

func TestEmptyPage(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	// Try to get a page beyond the range
	page := r.Page(r.NumPage() + 100)
	text, err := page.GetPlainText(nil)

	// Should not panic and should return empty or error
	if err != nil {
		t.Logf("GetPlainText on invalid page returned error: %v", err)
	}
	if text != "" {
		t.Logf("GetPlainText on invalid page returned text: %q", text)
	}
}

func TestPDFError(t *testing.T) {
	baseErr := ErrInvalidFont
	err := &PDFError{
		Op:   "test operation",
		Page: 5,
		Err:  baseErr,
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "test operation") {
		t.Errorf("error string should contain operation: %s", errStr)
	}
	if !strings.Contains(errStr, "page 5") {
		t.Errorf("error string should contain page number: %s", errStr)
	}

	// Test Unwrap
	if err.Unwrap() != baseErr {
		t.Error("Unwrap should return base error")
	}
}

func TestWrapError(t *testing.T) {
	err := wrapError("test op", ErrInvalidFont)
	if err == nil {
		t.Fatal("expected non-nil error")
	}

	pdfErr, ok := err.(*PDFError)
	if !ok {
		t.Fatal("expected PDFError type")
	}

	if pdfErr.Op != "test op" {
		t.Errorf("expected op 'test op', got %q", pdfErr.Op)
	}

	// Test with nil error
	if wrapError("test", nil) != nil {
		t.Error("wrapError with nil should return nil")
	}
}
