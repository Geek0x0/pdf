package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

type closableReader struct {
	*bytes.Reader
}

func (c *closableReader) Close() error { return nil }

func TestPageFontHelpers(t *testing.T) {
	r := &Reader{fontCache: NewFontCache()}
	fontDef := dict{
		name("BaseFont"):  name("Helvetica"),
		name("FirstChar"): int64(0),
		name("LastChar"):  int64(2),
		name("Widths"):    array{float64(1), float64(2), float64(3)},
		name("Encoding"):  name("WinAnsiEncoding"),
	}
	resources := dict{name("Font"): dict{name("F1"): fontDef}}
	pageDict := dict{
		name("Type"):      name("Page"),
		name("Resources"): resources,
		name("Contents"):  nil,
		name("MediaBox"):  array{int64(0), int64(0), float64(100), float64(100)},
	}
	page := Page{V: Value{r, objptr{}, pageDict}}

	fonts := page.Fonts()
	if len(fonts) != 1 || fonts[0] != "F1" {
		t.Fatalf("Fonts returned %v", fonts)
	}
	f := page.Font("F1")
	if f.BaseFont() == "" || f.FirstChar() != 0 || f.LastChar() != 2 {
		t.Fatalf("font metadata incorrect")
	}
	if f.Width(1) == 0 || len(f.Widths()) == 0 {
		t.Fatalf("font widths not populated")
	}
	if enc := f.Encoder(); enc == nil {
		t.Fatalf("expected encoder")
	}

	cache := make(map[string]*Font)
	scope := page.buildFontScope(page.Resources(), cache, nil)
	if scope.Get("F1") == nil || cache["F1"] == nil {
		t.Fatalf("buildFontScope did not cache font")
	}

	texts := []Text{
		{S: "B", X: 5, Y: 0, W: 1, FontSize: 10},
		{S: "A", X: 0, Y: 0, W: 1, FontSize: 10},
		{S: "Next", X: 0, Y: -5, W: 1, FontSize: 10},
	}
	plain := textRunsToPlain(texts)
	if !strings.Contains(plain, "\n") {
		t.Fatalf("textRunsToPlain did not include line break: %q", plain)
	}
}

func TestPageContentParsing(t *testing.T) {
	content := "BT /F1 12 Tf 1 2 Td (Hello) Tj T* (World) Tj ET"
	r := &Reader{
		f:         bytes.NewReader([]byte(content)),
		end:       int64(len(content)),
		fontCache: NewFontCache(),
	}
	fontDef := dict{
		name("BaseFont"):  name("Helvetica"),
		name("FirstChar"): int64(0),
		name("LastChar"):  int64(5),
		name("Widths"):    array{float64(1), float64(1), float64(1), float64(1), float64(1), float64(1)},
		name("Encoding"):  name("WinAnsiEncoding"),
	}
	pageDict := dict{
		name("Type"): name("Page"),
		name("Resources"): dict{
			name("Font"): dict{name("F1"): fontDef},
		},
		name("Contents"): stream{
			hdr:    dict{name("Length"): int64(len(content))},
			offset: 0,
		},
		name("MediaBox"): array{int64(0), int64(0), float64(100), float64(100)},
	}
	page := Page{V: Value{r, objptr{}, pageDict}}

	text, err := page.GetPlainText(nil)
	if err != nil || !strings.Contains(text, "Hello") {
		t.Fatalf("GetPlainText failed: %v %q", err, text)
	}
	rows, err := page.GetTextByRow()
	if err != nil || len(rows) == 0 {
		t.Fatalf("GetTextByRow failed: %v len=%d", err, len(rows))
	}
	cols, err := page.GetTextByColumn()
	if err != nil || len(cols) == 0 {
		t.Fatalf("GetTextByColumn failed: %v len=%d", err, len(cols))
	}
}

func TestLazyPageManagerCache(t *testing.T) {
	r := stubReader(2)
	lp := NewLazyPage(r, 1)
	if lp.IsLoaded() {
		t.Fatalf("new LazyPage should not be loaded")
	}
	if lp.GetContent() == nil || !lp.IsLoaded() {
		t.Fatalf("GetContent did not load content")
	}
	lp.Release()
	if lp.IsLoaded() {
		t.Fatalf("Release should clear content")
	}

	manager := NewLazyPageManager(r, 1)
	p1 := manager.GetPage(1)
	p1.GetContent()
	manager.GetPage(2).GetContent()
	manager.GetPage(1)
	manager.Clear()
	total, loaded := manager.GetStats()
	if total != 0 || loaded != 0 {
		t.Fatalf("unexpected stats after Clear: total=%d loaded=%d", total, loaded)
	}
}

func TestOptimizationExamplesExtras(t *testing.T) {
	texts := []Text{
		{S: "hello", FontSize: 10, X: 0, Y: 0, W: 2},
		{S: "world", FontSize: 10, X: 1, Y: 0, W: 2},
	}
	bsb := NewBatchStringBuilder(texts)
	if combined := bsb.AppendTexts(texts); combined == "" {
		t.Fatalf("AppendTexts returned empty string")
	}
	if bsb.String() == "" {
		t.Fatalf("String should return built buffer")
	}
	bsb.Reset()
	if bsb.String() != "" {
		t.Fatalf("Reset did not clear buffer")
	}

	clusters := ClusterTextBlocksOptimized(texts)
	if len(clusters) == 0 || clusters[0] == nil {
		t.Fatalf("ClusterTextBlocksOptimized returned nil")
	}

	cache := NewMultiLevelCache()
	cache.Put("k1", "v1")
	if v, ok := cache.Get("k1"); !ok || v.(string) != "v1" {
		t.Fatalf("cache get failed")
	}
	cache.Prefetch([]string{"k1", "k2"})
	time.Sleep(50 * time.Millisecond)
	stats := cache.Stats()
	if stats["prefetch"] == 0 {
		t.Fatalf("prefetch stats not updated: %v", stats)
	}
}

func TestStreamingAndReadHelpers(t *testing.T) {
	sp := NewStreamProcessor(1, 1, 8)
	if !sp.tryReserveMemory(4) || sp.tryReserveMemory(10) {
		t.Fatalf("memory reservation logic failed")
	}
	sp.releaseMemory(2)
	sp.Close()

	if estimateTextMemory(Text{S: "abcd"}) == 0 {
		t.Fatalf("estimateTextMemory failed")
	}
	if estimateBlockMemory(&TextBlock{Texts: []Text{{S: "x"}, {S: "y"}}}) < 128 {
		t.Fatalf("estimateBlockMemory too small")
	}
	if estimatePageMemory(0) != 64 || estimatePageMemory(2) <= 64 {
		t.Fatalf("estimatePageMemory unexpected")
	}

	if decodeInt([]byte{0x01, 0x02}) != 0x0102 {
		t.Fatalf("decodeInt failed")
	}
	valBool := Value{data: true}
	if !valBool.Bool() {
		t.Fatalf("Bool conversion failed")
	}
	valFloat := Value{data: float64(1.5)}
	if valFloat.Float64() != 1.5 {
		t.Fatalf("Float64 conversion failed")
	}
	valInt := Value{data: int64(2)}
	if valInt.Float64() != 2 {
		t.Fatalf("Float64 int conversion failed")
	}
	utf16Val := Value{data: string([]byte{0x00, 0x41, 0x00, 0x42})}
	if utf16Val.TextFromUTF16() != "AB" {
		t.Fatalf("TextFromUTF16 failed")
	}
	textVal := Value{data: string([]byte{0xFE, 0xFF, 0x00, 0x43})}
	if textVal.Text() == "" {
		t.Fatalf("Text decoding missing")
	}

	data := buildMinimalPDF()
	cr := &closableReader{bytes.NewReader(data)}
	r, err := NewReaderEncryptedWithMmap(cr, int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewReaderEncryptedWithMmap failed: %v", err)
	}
	r.Close()
}

func TestReaderAggregateFunctions(t *testing.T) {
	r := stubReader(1)
	if _, err := r.GetPlainText(); err != nil {
		t.Fatalf("GetPlainText: %v", err)
	}
	if styled, err := r.GetStyledTexts(); err != nil || len(styled) != 0 {
		t.Fatalf("GetStyledTexts unexpected: %v len=%d", err, len(styled))
	}
	if _, err := r.ExtractAllPagesParallel(context.Background(), 0); err != nil {
		t.Fatalf("ExtractAllPagesParallel: %v", err)
	}

	outlineReader := &Reader{
		trailer: dict{
			name("Root"): dict{
				name("Outlines"): dict{
					name("Title"): "root",
					name("First"): dict{
						name("Title"): "child",
						name("Next"): dict{
							name("Title"): "sibling",
						},
					},
				},
			},
		},
	}
	out := outlineReader.Outline()
	if len(out.Child) != 2 {
		t.Fatalf("Outline children = %d", len(out.Child))
	}

	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic from errorf")
		}
	}()
	new(Reader).errorf("boom")
}

func TestReaderEncryptionPrimitives(t *testing.T) {
	encDict := dict{
		name("Filter"): name("Standard"),
		name("Length"): int64(40),
		name("V"):      int64(2),
		name("R"):      int64(2),
		name("O"):      string(bytes.Repeat([]byte{1}, 32)),
		name("U"):      string(passwordPad[:32]),
		name("P"):      int64(-4),
	}
	r := &Reader{
		trailer: dict{
			name("Encrypt"): encDict,
			name("ID"):      array{string("01234567890123456789012345678901")},
		},
	}
	if err := r.initEncrypt("pw"); err != nil && err != ErrInvalidPassword {
		t.Fatalf("initEncrypt unexpected error: %v", err)
	}
	key := cryptKey([]byte{1, 2, 3, 4, 5}, false, objptr{id: 1})
	if len(key) == 0 {
		t.Fatalf("cryptKey returned empty key")
	}
	dec := decryptString(key, false, objptr{id: 1}, "text")
	if dec == "" {
		t.Fatalf("decryptString returned empty")
	}
	reader := decryptStream(key, false, objptr{id: 1}, bytes.NewReader([]byte("data payload....")))
	buf := make([]byte, 4)
	reader.Read(buf)
}

func TestFontEncodingPaths(t *testing.T) {
	r := &Reader{fontCache: NewFontCache()}
	font := Font{V: Value{r, objptr{}, dict{
		name("Subtype"):         name("Type0"),
		name("Encoding"):        name("Identity-H"),
		name("DescendantFonts"): array{dict{name("WMode"): int64(1)}},
	}}}
	font.type0Encoder()
	font.cmapEncodingFromValue(Value{})
	font.charmapEncoding()

	if m, ok := matrixFromValue(Value{data: array{float64(1), float64(0), float64(0), float64(1), float64(2), float64(3)}}); !ok || m[2][0] != 2 {
		t.Fatalf("matrixFromValue failed")
	}

	registerCMap("TestCMap", &nopEncoder{})
	if lookupCMap("TestCMap") == nil {
		t.Fatalf("lookupCMap failed")
	}
}
