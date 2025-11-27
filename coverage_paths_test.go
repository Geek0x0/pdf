package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestAsyncAndOptimizedExtractionPaths(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skip async coverage: %v", err)
		return
	}
	defer f.Close()

	ctx := context.Background()
	ar := NewAsyncReader(r)

	textChan, errChan := ar.AsyncExtractTextWithContext(ctx, ExtractOptions{Workers: 2})
	for range textChan {
	}
	if err := <-errChan; err != nil {
		t.Fatalf("AsyncExtractTextWithContext error: %v", err)
	}

	structChan, structErr := ar.AsyncExtractStructured(ctx)
	for range structChan {
	}
	if err := <-structErr; err != nil {
		t.Fatalf("AsyncExtractStructured error: %v", err)
	}

	streamErr := ar.AsyncStream(ctx, func(p Page, i int) error {
		_, _ = p.GetPlainText(context.Background(), nil)
		return nil
	})
	if err := <-streamErr; err != nil {
		t.Fatalf("AsyncStream error: %v", err)
	}

	val := r.Page(1).V.Key("Contents")
	dataChan, serrChan := ar.StreamValueReader(ctx, val)
	for range dataChan {
	}
	<-serrChan

	ara := NewAsyncReaderAt(bytes.NewReader([]byte("hello async")))
	nChan, eChan := ara.ReadAtAsync(ctx, make([]byte, 5), 0)
	<-nChan
	<-eChan

	page := r.Page(1)
	if _, err := page.OptimizedGetPlainText(context.Background(), nil); err != nil {
		t.Fatalf("OptimizedGetPlainText error: %v", err)
	}
	if _, err := page.OptimizedGetTextByRow(); err != nil {
		t.Fatalf("OptimizedGetTextByRow error: %v", err)
	}
	if _, err := page.OptimizedGetTextByColumn(); err != nil {
		t.Fatalf("OptimizedGetTextByColumn error: %v", err)
	}

	if res, err := r.BatchExtractText([]int{1}, true); err == nil && len(res) == 0 {
		t.Fatalf("BatchExtractText returned empty map")
	}

	extractor := NewStreamingTextExtractor(r, 2)
	if _, _, _, err := extractor.NextPage(); err != nil {
		t.Fatalf("NextPage error: %v", err)
	}
	if _, _, err := extractor.NextBatch(); err != nil {
		t.Fatalf("NextBatch error: %v", err)
	}
	extractor.Reset()
	extractor.Close()

	if _, err := ProcessTextWithMultiLanguage(r); err != nil {
		t.Fatalf("ProcessTextWithMultiLanguage error: %v", err)
	}

	ext := NewExtractor(r).Workers(2)
	if _, err := ext.ExtractText(); err != nil {
		t.Fatalf("Extractor concurrent path failed: %v", err)
	}
}

func TestCacheContextAndLexCoverage(t *testing.T) {
	cache := NewResultCache(2, time.Millisecond*10, "LRU")
	cache.Put("k1", "v1")
	cache.Put("k2", "v2")
	time.Sleep(time.Millisecond * 15)
	cache.Close()

	cctx := newContextChecker(context.Background(), 5)
	cctx.Check()
	timer := newParseTimer(time.Millisecond*5, 1)
	_ = timer.Check()
	_ = timer.Elapsed()

	b := newBuffer(strings.NewReader("<48656c6c6f>"), 0)
	token := b.readHexString()
	if token == "" {
		t.Fatalf("readHexString returned empty")
	}
	PutPDFBuffer(b)
}

func TestFontCacheAndSortingCoverage(t *testing.T) {
	sorter := NewOptimizedSorter()
	texts := []Text{{S: "b", X: 2}, {S: "a", X: 1}, {S: "c", X: 3}}
	sorter.parallelThreshold = 5
	sorter.SortTexts(texts, func(i, j int) bool { return texts[i].X < texts[j].X })
	sorter.standardSort(texts, func(i, j int) bool { return texts[i].S < texts[j].S })
}
