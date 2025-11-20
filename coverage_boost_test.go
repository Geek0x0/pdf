package pdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"
)

// buildMinimalPDF constructs a tiny but valid PDF file in memory so that
// parsing paths in read.go/lex.go are exercised without external fixtures.
func buildMinimalPDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	// Record offsets for xref table.
	offsets := make([]int, 4)

	offsets[1] = buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	offsets[2] = buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Count 1 /Kids [3 0 R] >>\nendobj\n")

	offsets[3] = buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] >>\nendobj\n")

	xrefPos := buf.Len()
	buf.WriteString("xref\n0 4\n")
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= 3; i++ {
		buf.WriteString(sprintf("%010d 00000 n \n", offsets[i]))
	}

	buf.WriteString("trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n")
	buf.WriteString(sprintf("%d\n", xrefPos))
	buf.WriteString("%%EOF\n")
	return buf.Bytes()
}

func sprintf(format string, args ...interface{}) string {
	return fmt.Sprintf(format, args...)
}

// stubReader constructs a minimal Reader with the requested number of empty pages
// and some metadata so helper methods can operate without real PDF content.
func stubReader(pageCount int) *Reader {
	mediaBox := array{int64(0), int64(0), float64(200), float64(200)}
	kids := make(array, pageCount)
	for i := 0; i < pageCount; i++ {
		kids[i] = dict{
			name("Type"):     name("Page"),
			name("Contents"): nil, // force empty content path
			name("MediaBox"): mediaBox,
			name("Resources"): dict{
				name("Font"): dict{},
			},
		}
	}

	pages := dict{
		name("Type"):  name("Pages"),
		name("Count"): int64(pageCount),
		name("Kids"):  kids,
	}

	root := dict{
		name("Type"):     name("Catalog"),
		name("Pages"):    pages,
		name("Metadata"): stream{hdr: dict{}, ptr: objptr{}, offset: 0},
	}

	info := dict{
		name("Title"):        "D:20240102030405+08'00'",
		name("Author"):       "tester",
		name("Keywords"):     "alpha,beta",
		name("CreationDate"): "D:20240505101010+02'30'",
		name("ModDate"):      "D:20240506111111-05'00'",
		name("Trapped"):      name("True"),
		name("CustomKey"):    "custom",
	}

	return &Reader{
		trailer: dict{
			name("Root"): root,
			name("Info"): info,
		},
		fontCache: NewFontCache(),
	}
}

func TestMinimalPDFParsing(t *testing.T) {
	data := buildMinimalPDF()
	reader, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}
	if got := reader.NumPage(); got != 1 {
		t.Fatalf("NumPage = %d, want 1", got)
	}

	// Verify trailer navigation and basic text extraction on the minimal page.
	page := reader.Page(1)
	text, err := page.GetPlainText(nil)
	if err != nil {
		t.Fatalf("GetPlainText: %v", err)
	}
	if text != "" {
		t.Fatalf("expected empty text, got %q", text)
	}

	// Concurrent plain text path should also succeed.
	out, err := reader.GetPlainTextConcurrent(0)
	if err != nil {
		t.Fatalf("GetPlainTextConcurrent: %v", err)
	}
	if data, _ := io.ReadAll(out); len(data) != 0 {
		t.Fatalf("expected empty output, got %q", string(data))
	}
}

func TestExtractWithContextStub(t *testing.T) {
	r := stubReader(3)
	ctx := context.Background()
	rd, err := r.ExtractWithContext(ctx, ExtractOptions{Workers: 10})
	if err != nil {
		t.Fatalf("ExtractWithContext: %v", err)
	}
	buf := make([]byte, 8)
	if n, err := rd.Read(buf); err != io.EOF && err != nil {
		t.Fatalf("reading buffer: %v", err)
	} else if n != 0 {
		t.Fatalf("expected zero bytes, got %d", n)
	}

	// Cancellation before dispatch should short circuit.
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.ExtractWithContext(cancelCtx, ExtractOptions{}); err == nil {
		t.Fatalf("expected cancellation error")
	}
}

func TestExtractorModes(t *testing.T) {
	r := stubReader(2)
	ex := NewExtractor(r).
		Workers(1).
		Pages(1, 2).
		SmartOrdering(true).
		Context(context.Background())

	result, err := ex.Extract()
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.PageCount != 2 {
		t.Fatalf("PageCount = %d", result.PageCount)
	}

	if text, err := ex.ExtractText(); err != nil {
		t.Fatalf("ExtractText: %v", err)
	} else if len(text) != 1 { // two pages produce one separator
		t.Fatalf("unexpected text length %d", len(text))
	}

	if styled, err := ex.ExtractStyledTexts(); err != nil {
		t.Fatalf("ExtractStyledTexts: %v", err)
	} else if len(styled) != 0 {
		t.Fatalf("expected no styled texts, got %d", len(styled))
	}

	if blocks, err := ex.ExtractStructured(); err != nil {
		t.Fatalf("ExtractStructured: %v", err)
	} else if len(blocks) != 0 {
		t.Fatalf("expected no blocks, got %d", len(blocks))
	}

	meta, err := r.GetMetadata()
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta.Trapped == "" || len(meta.Keywords) == 0 || meta.Custom["_HasXMP"] != "true" {
		t.Fatalf("metadata not populated as expected: %+v", meta)
	}
}

func TestBatchAndCacheHelpers(t *testing.T) {
	r := stubReader(3)
	opts := BatchExtractOptions{
		Workers:       2,
		Context:       context.Background(),
		UseFontCache:  true,
		FontCacheType: FontCacheOptimized,
	}

	if combined, err := r.ExtractPagesBatchToString(opts); err != nil {
		t.Fatalf("ExtractPagesBatchToString: %v", err)
	} else if len(combined) != 2 { // two newlines between three pages
		t.Fatalf("unexpected combined length %d", len(combined))
	}

	structured := r.ExtractStructuredBatch(opts)
	for res := range structured {
		if res.Error != nil {
			t.Fatalf("structured batch error: %v", res.Error)
		}
	}

	cache := NewResultCache(1024, time.Second, "LRU")
	cr := NewCachedReader(r, cache)
	if texts, err := cr.CachedPage(1); err != nil {
		t.Fatalf("CachedPage: %v", err)
	} else if len(texts) != 0 {
		t.Fatalf("expected empty page content, got %v", texts)
	}
	if _, err := cr.CachedClassifyTextBlocks(1); err != nil {
		t.Fatalf("CachedClassifyTextBlocks: %v", err)
	}
	// second call should hit cache without error
	if _, err := cr.CachedClassifyTextBlocks(1); err != nil {
		t.Fatalf("CachedClassifyTextBlocks cache hit failed: %v", err)
	}
}

func TestAsyncReaderOperations(t *testing.T) {
	r := stubReader(2)
	ar := NewAsyncReader(r)

	ctx := context.Background()
	textCh, errCh := ar.AsyncExtractText(ctx)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("AsyncExtractText err: %v", err)
		}
	case text := <-textCh:
		if text != "" {
			t.Fatalf("unexpected text %q", text)
		}
	}

	_, errCh = ar.AsyncExtractStructured(ctx)
	if err := <-errCh; err != nil {
		t.Fatalf("AsyncExtractStructured err: %v", err)
	}

	// Ensure cancellation propagates
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelTextCh, cancelErrCh := ar.AsyncExtractText(cancelCtx)
	select {
	case err, ok := <-cancelErrCh:
		if ok && err == nil {
			t.Fatalf("expected cancellation error")
		}
	case <-cancelTextCh:
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for cancellation")
	}
}
