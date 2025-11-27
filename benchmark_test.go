package pdf

import (
	"context"
	"io"
	"testing"
)

func BenchmarkGetPlainText(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skipf("skipping benchmark: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader, err := r.GetPlainText()
		if err != nil {
			b.Fatalf("GetPlainText failed: %v", err)
		}
		_, _ = io.ReadAll(reader)
	}
}

func BenchmarkGetPlainTextConcurrent(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skipf("skipping benchmark: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	workers := []int{1, 2, 4, 8}
	for _, w := range workers {
		b.Run("workers_"+string(rune('0'+w)), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				reader, err := r.GetPlainTextConcurrent(w)
				if err != nil {
					b.Fatalf("GetPlainTextConcurrent failed: %v", err)
				}
				_, _ = io.ReadAll(reader)
			}
		})
	}
}

func BenchmarkExtractWithContext(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skipf("skipping benchmark: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	ctx := context.Background()
	opts := ExtractOptions{Workers: 4}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader, err := r.ExtractWithContext(ctx, opts)
		if err != nil {
			b.Fatalf("ExtractWithContext failed: %v", err)
		}
		_, _ = io.ReadAll(reader)
	}
}

func BenchmarkPageGetPlainText(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skipf("skipping benchmark: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		b.Skip("PDF has no pages")
	}

	page := r.Page(1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := page.GetPlainText(context.Background(), nil)
		if err != nil {
			b.Fatalf("GetPlainText failed: %v", err)
		}
	}
}

func BenchmarkPageGetPlainTextWithFontCache(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skipf("skipping benchmark: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		b.Skip("PDF has no pages")
	}

	page := r.Page(1)
	fonts := make(map[string]*Font)

	// Pre-populate font cache
	for _, name := range page.Fonts() {
		f := page.Font(name)
		fonts[name] = &f
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := page.GetPlainText(context.Background(), fonts)
		if err != nil {
			b.Fatalf("GetPlainText failed: %v", err)
		}
	}
}

func BenchmarkGetStyledTexts(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skipf("skipping benchmark: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := r.GetStyledTexts()
		if err != nil {
			b.Fatalf("GetStyledTexts failed: %v", err)
		}
	}
}

func BenchmarkGetTextByRow(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skipf("skipping benchmark: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		b.Skip("PDF has no pages")
	}

	page := r.Page(1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := page.GetTextByRow()
		if err != nil {
			b.Fatalf("GetTextByRow failed: %v", err)
		}
	}
}

func BenchmarkGetTextByColumn(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skipf("skipping benchmark: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		b.Skip("PDF has no pages")
	}

	page := r.Page(1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := page.GetTextByColumn()
		if err != nil {
			b.Fatalf("GetTextByColumn failed: %v", err)
		}
	}
}

func BenchmarkFontCache(b *testing.B) {
	cache := NewFontCache()
	font := &Font{}

	b.Run("Set", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache.Set("TestFont", font)
		}
	})

	b.Run("Get", func(b *testing.B) {
		cache.Set("TestFont", font)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = cache.Get("TestFont")
		}
	})

	b.Run("Concurrent", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				cache.Set("TestFont", font)
				_, _ = cache.Get("TestFont")
			}
		})
	})
}
