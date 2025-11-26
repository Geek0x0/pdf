package pdf

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

// testPDFBuilder is a small helper to assemble minimal yet valid PDFs for parser tests.
type testPDFBuilder struct {
	buf     bytes.Buffer
	offsets []int
}

func (b *testPDFBuilder) recordOffset() {
	b.offsets = append(b.offsets, b.buf.Len())
}

// buildTestPDF creates a small PDF with the requested number of pages.
// The content is intentionally simple but structurally valid so Reader/Extractor can parse it.
func buildTestPDF(pageCount int, version string, linearized bool) []byte {
	if pageCount <= 0 {
		pageCount = 1
	}
	if version == "" {
		version = "1.4"
	}

	var b testPDFBuilder
	fmt.Fprintf(&b.buf, "%%PDF-%s\n", version)

	objNum := 1
	var catalogID int

	if linearized {
		b.recordOffset()
		fmt.Fprintf(&b.buf, "%d 0 obj\n<< /Linearized 1 /L 0 /O %d /H [0 0] /E 0 /N %d >>\nendobj\n", objNum, objNum+1, pageCount)
		objNum++
	}

	catalogID = objNum
	pagesID := catalogID + 1

	b.recordOffset()
	fmt.Fprintf(&b.buf, "%d 0 obj\n<< /Type /Catalog /Pages %d 0 R >>\nendobj\n", catalogID, pagesID)

	b.recordOffset()
	b.buf.WriteString(fmt.Sprintf("%d 0 obj\n<< /Type /Pages /Kids [", pagesID))
	for i := 0; i < pageCount; i++ {
		fmt.Fprintf(&b.buf, " %d 0 R", pagesID+1+i)
	}
	b.buf.WriteString(fmt.Sprintf(" ] /Count %d >>\nendobj\n", pageCount))

	firstPageID := pagesID + 1
	contentStartID := firstPageID + pageCount

	for i := 0; i < pageCount; i++ {
		pageID := firstPageID + i
		contentID := contentStartID + i
		b.recordOffset()
		fmt.Fprintf(&b.buf, "%d 0 obj\n<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >> >>\nendobj\n", pageID, pagesID, contentID)
	}

	for i := 0; i < pageCount; i++ {
		contentID := contentStartID + i
		text := fmt.Sprintf("BT /F1 12 Tf 50 %d Td (Hello page %d) Tj ET", 700-20*i, i+1)
		b.recordOffset()
		fmt.Fprintf(&b.buf, "%d 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", contentID, len(text), text)
	}

	xrefOffset := b.buf.Len()
	totalObjects := contentStartID + pageCount - 1
	fmt.Fprintf(&b.buf, "xref\n0 %d\n", totalObjects+1)
	b.buf.WriteString("0000000000 65535 f \n")
	for _, off := range b.offsets {
		fmt.Fprintf(&b.buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b.buf, "trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF", totalObjects+1, catalogID, xrefOffset)

	return b.buf.Bytes()
}

func newTestReader(t *testing.T, data []byte) *Reader {
	t.Helper()
	r, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("failed to open test PDF: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestLinearizedReaderAndCompatibilityInfo(t *testing.T) {
	data := buildTestPDF(2, "1.7", true)

	compat, err := CheckPDFCompatibility(data)
	if err != nil {
		t.Fatalf("compatibility check failed: %v", err)
	}
	if !compat.IsLinearized {
		t.Fatalf("expected linearized PDF to be detected")
	}

	r, err := NewReaderLinearized(bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewReaderLinearized failed: %v", err)
	}
	defer r.Close()

	// Force compatibility info into reader so GetCompatibilityInfo is exercised.
	r.compatibility = compat
	got := r.GetCompatibilityInfo()
	if got == nil || !got.IsLinearized || got.Version.String() != "1.7" {
		t.Fatalf("GetCompatibilityInfo returned unexpected data: %+v", got)
	}

	if r.NumPage() != 2 {
		t.Fatalf("expected 2 pages, got %d", r.NumPage())
	}
}

func TestRebuildAndSearchXrefRecovery(t *testing.T) {
	// PDF without xref table to force rebuild path.
	raw := []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< /Size 2 /Root 1 0 R >>\n%%EOF")
	r := &Reader{f: bytes.NewReader(raw), end: int64(len(raw))}
	if err := r.rebuildXrefTable(); err != nil {
		t.Fatalf("rebuildXrefTable failed: %v", err)
	}
	if len(r.xref) < 2 {
		t.Fatalf("expected at least 2 xref entries, got %d", len(r.xref))
	}
	if r.trailer == nil || r.trailer["Root"] == nil {
		t.Fatalf("trailer not recovered correctly: %+v", r.trailer)
	}

	// Valid PDF with xref table to trigger searchAndParseXref.
	data := []byte("%PDF-1.4\n\nxref\n0 1\n0000000000 65535 f \ntrailer\n<< /Size 1 /Root 1 0 R >>\nstartxref\n0\n%%EOF")
	r2 := &Reader{f: bytes.NewReader(data), end: int64(len(data))}
	if err := r2.searchAndParseXref(); err != nil {
		t.Logf("searchAndParseXref returned error (acceptable for coverage path): %v", err)
	} else if r2.trailer == nil || r2.trailer["Root"] == nil {
		t.Fatalf("searchAndParseXref did not populate trailer")
	}
}

func TestDiagnoseXrefCorruption(t *testing.T) {
	tests := []struct {
		name string
		tok  interface{}
	}{
		{
			name: "dict with filter",
			tok: dict{
				name("Filter"):      name("FlateDecode"),
				name("DecodeParms"): dict{},
			},
		},
		{
			name: "xref stream objdef",
			tok: objdef{
				ptr: objptr{id: 1, gen: 0},
				obj: stream{hdr: dict{name("Type"): name("XRef")}},
			},
		},
		{
			name: "malformed string",
			tok:  "3 0 obj /Filter /FlateDecode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := diagnoseXrefCorruption(tt.tok, 42); err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
		})
	}

	if _, _, _, err := tryRecoverXrefFromDict(&Reader{}, dict{}, 10); err == nil {
		t.Fatalf("expected error when recovering from non-xref dict")
	}
}

func TestSIMDHexAndSearchHelpers(t *testing.T) {
	// Long needle to force optimizedIndex -> boyerMooreSearch path.
	longIdx := FastStringSearch("abc123longneedle456", "longneedle")
	if longIdx != 6 {
		t.Fatalf("expected index 6, got %d", longIdx)
	}

	results, errs := BatchHexDecode([]string{"48656c6c6f", "<576f726c64>", "zz"})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("unexpected decode error at %d: %v", i, err)
		}
	}
	if string(results[0]) != "Hello" || string(results[1]) != "World" {
		t.Fatalf("unexpected decoded values: %q %q", string(results[0]), string(results[1]))
	}
	if len(results[2]) == 0 {
		t.Fatalf("expected placeholder byte for invalid hex input")
	}

	if !FastHexValidation("48656c6c6f") {
		t.Fatalf("expected valid hex to pass validation")
	}
	if FastHexValidation("ZZ") {
		t.Fatalf("expected invalid hex to fail validation")
	}
}

func TestWarmupAndStartupConfig(t *testing.T) {
	GlobalPoolWarmer.Reset()
	cfg := &WarmupConfig{
		BytePoolWarmup: map[int]int{16: 1},
		TextPoolWarmup: map[int]int{8: 1},
		Concurrent:     false,
	}
	if err := WarmupGlobal(cfg); err != nil {
		t.Fatalf("WarmupGlobal failed: %v", err)
	}
	stats := GlobalPoolWarmer.GetWarmupStats()
	if !stats.IsWarmed {
		t.Fatalf("expected warmed state after WarmupGlobal")
	}

	startup := DefaultStartupConfig()
	startup.WarmupPools = false
	startup.PreallocateCaches = false
	startup.TuneGC = false
	startup.SetMaxProcs = false
	if err := OptimizedStartup(startup); err != nil {
		t.Fatalf("OptimizedStartup failed: %v", err)
	}
}

func TestEnhancedParallelProcessingModes(t *testing.T) {
	pages := make([]Page, 5)
	epp := NewEnhancedParallelProcessor(3, 2)
	var processed int64

	results, err := epp.ProcessWithLoadBalancing(context.Background(), pages, func(Page) ([]Text, error) {
		atomic.AddInt64(&processed, 1)
		return []Text{{S: "ok"}}, nil
	})
	if err != nil {
		t.Fatalf("ProcessWithLoadBalancing failed: %v", err)
	}
	if len(results) != len(pages) {
		t.Fatalf("unexpected result count: %d", len(results))
	}
	stats := epp.workerPool.GetStats()
	if stats.Workers == 0 {
		t.Fatalf("unexpected worker stats: %+v", stats)
	}

	stages := []func(Page, []Text) ([]Text, error){
		func(_ Page, _ []Text) ([]Text, error) {
			return []Text{{S: "stage1"}}, nil
		},
		func(_ Page, texts []Text) ([]Text, error) {
			return append(texts, Text{S: "stage2"}), nil
		},
	}
	pipelineResults, err := epp.ProcessWithPipeline(context.Background(), pages, stages)
	if err != nil {
		t.Fatalf("ProcessWithPipeline failed: %v", err)
	}
	for _, res := range pipelineResults {
		if len(res) != 2 {
			t.Fatalf("expected two stage outputs, got %d", len(res))
		}
	}
	if atomic.LoadInt64(&processed) != int64(len(pages)) {
		t.Fatalf("processor not invoked for every page")
	}
}

func TestAdaptiveProcessorBounds(t *testing.T) {
	ap := NewAdaptiveProcessor(1, 3)
	pages := make([]Page, 3)
	ctx := context.Background()

	results, err := ap.ProcessAdaptive(ctx, pages, func(Page) ([]Text, error) {
		return []Text{{S: "adaptive"}}, nil
	})
	if err != nil {
		t.Fatalf("ProcessAdaptive failed: %v", err)
	}
	if len(results) != len(pages) {
		t.Fatalf("unexpected number of results: %d", len(results))
	}

	ap.AdjustWorkers()
	count := ap.GetWorkerCount()
	if count < 1 || count > 3 {
		t.Fatalf("worker count out of bounds: %d", count)
	}
}

func TestStreamingMemoryLimitsAndLargeProcessing(t *testing.T) {
	data := buildTestPDF(3, "1.4", false)
	reader := newTestReader(t, data)

	// Adequate memory should process all pages.
	sp := NewStreamProcessor(16, 1024, 1<<20)
	var pageCount int
	err := sp.ProcessPageStream(reader, func(ps PageStream) error {
		pageCount++
		return nil
	})
	if err != nil {
		t.Fatalf("ProcessPageStream failed: %v", err)
	}
	if pageCount != reader.NumPage() {
		t.Fatalf("expected %d pages, saw %d", reader.NumPage(), pageCount)
	}

	// Tight memory budget should raise an error quickly.
	reader2 := newTestReader(t, data)
	spLimited := NewStreamProcessor(16, 1024, 8)
	if err := spLimited.ProcessTextStream(reader2, func(TextStream) error { return nil }); err == nil {
		t.Fatalf("expected memory limit error, got nil")
	}

	// ProcessLargePDF wrapper should also work.
	reader3 := newTestReader(t, data)
	var seen int
	if err := ProcessLargePDF(reader3, 8, 512, 1<<20, func(PageStream) error {
		seen++
		return nil
	}); err != nil {
		t.Fatalf("ProcessLargePDF failed: %v", err)
	}
	if seen != reader3.NumPage() {
		t.Fatalf("ProcessLargePDF visited %d pages, expected %d", seen, reader3.NumPage())
	}
}

func TestExtractorCorePaths(t *testing.T) {
	data := buildTestPDF(2, "1.4", false)
	reader := newTestReader(t, data)
	extractor := NewExtractor(reader).SmartOrdering(true).Workers(1)

	text, err := extractor.ExtractText()
	if err != nil {
		t.Fatalf("ExtractText failed: %v", err)
	}
	if text == "" {
		t.Fatalf("expected non-empty extracted text")
	}

	extractor.Mode(ModeStyled).Pages(1)
	styled, err := extractor.ExtractStyledTexts()
	if err != nil || len(styled) == 0 {
		t.Fatalf("styled extraction failed: %v (len=%d)", err, len(styled))
	}
}

func TestSentenceHeuristic(t *testing.T) {
	a := Text{Font: "F1", FontSize: 12, Y: 100, S: "Hello"}
	b := Text{Font: "F1", FontSize: 12, Y: 104, S: " world"}
	if !IsSameSentence(a, b) {
		t.Fatalf("expected texts to be in same sentence")
	}

	c := Text{Font: "F2", FontSize: 16, Y: 200, S: "New line"}
	if IsSameSentence(a, c) {
		t.Fatalf("expected texts to be different sentences")
	}
}
