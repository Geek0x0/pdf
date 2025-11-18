// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"strings"
	"testing"
)

// Benchmark comparisons between original and optimized implementations

func BenchmarkGetPlainTextOriginal(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skip("test file not found")
	}
	defer f.Close()

	page := r.Page(1)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := page.GetPlainText(nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetPlainTextOptimized(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skip("test file not found")
	}
	defer f.Close()

	if err != nil {
		b.Fatal(err)
	}

	page := r.Page(1)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := page.OptimizedGetPlainText(nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetTextByRowOriginal(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skip("test file not found")
	}
	defer f.Close()

	if err != nil {
		b.Fatal(err)
	}

	page := r.Page(1)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := page.GetTextByRow()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetTextByRowOptimized(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skip("test file not found")
	}
	defer f.Close()

	if err != nil {
		b.Fatal(err)
	}

	page := r.Page(1)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := page.OptimizedGetTextByRow()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetTextByColumnOriginal(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skip("test file not found")
	}
	defer f.Close()

	if err != nil {
		b.Fatal(err)
	}

	page := r.Page(1)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := page.GetTextByColumn()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetTextByColumnOptimized(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skip("test file not found")
	}
	defer f.Close()

	if err != nil {
		b.Fatal(err)
	}

	page := r.Page(1)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := page.OptimizedGetTextByColumn()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark object pools

func BenchmarkBuilderPoolGet(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			builder := GetBuilder()
			builder.WriteString("test")
			PutBuilder(builder)
		}
	})
}

func BenchmarkBuilderDirectAlloc(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var builder strings.Builder
			builder.WriteString("test")
		}
	})
}

func BenchmarkTextPoolGet(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			text := GetText()
			text.S = "test"
			text.X = 10.0
			text.Y = 20.0
			PutText(text)
		}
	})
}

func BenchmarkTextDirectAlloc(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			text := &Text{
				S: "test",
				X: 10.0,
				Y: 20.0,
			}
			_ = text
		}
	})
}

// Benchmark lazy loading

func BenchmarkLazyPageLoad(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skip("test file not found")
	}
	defer f.Close()

	if err != nil {
		b.Fatal(err)
	}

	manager := NewLazyPageManager(r, 5)
	defer manager.Clear()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pageNum := (i % r.NumPage()) + 1
		lazyPage := manager.GetPage(pageNum)
		_ = lazyPage.GetContent()
	}
}

func BenchmarkDirectPageLoad(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skip("test file not found")
	}
	defer f.Close()

	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pageNum := (i % r.NumPage()) + 1
		page := r.Page(pageNum)
		_ = page.Content()
	}
}

// Benchmark batch extraction

func BenchmarkBatchExtractWithLazy(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skip("test file not found")
	}
	defer f.Close()

	if err != nil {
		b.Fatal(err)
	}

	pageNums := []int{1}
	if r.NumPage() > 1 {
		pageNums = append(pageNums, 2)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := r.BatchExtractText(pageNums, true)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBatchExtractWithoutLazy(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skip("test file not found")
	}
	defer f.Close()

	if err != nil {
		b.Fatal(err)
	}

	pageNums := []int{1}
	if r.NumPage() > 1 {
		pageNums = append(pageNums, 2)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := r.BatchExtractText(pageNums, false)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark streaming extraction

func BenchmarkStreamingExtractor(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skip("test file not found")
	}
	defer f.Close()

	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		extractor := NewStreamingTextExtractor(r, 5)

		for {
			_, _, hasMore, err := extractor.NextPage()
			if err != nil {
				b.Fatal(err)
			}
			if !hasMore {
				break
			}
		}

		extractor.Close()
	}
}

// Benchmark FastStringBuilder

func BenchmarkFastStringBuilder(b *testing.B) {
	b.Run("FastStringBuilder", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			builder := NewFastStringBuilder(100)
			for j := 0; j < 10; j++ {
				builder.WriteString("test string ")
			}
			_ = builder.String()
		}
	})

	b.Run("strings.Builder", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var builder strings.Builder
			builder.Grow(100)
			for j := 0; j < 10; j++ {
				builder.WriteString("test string ")
			}
			_ = builder.String()
		}
	})
}

// Memory allocation benchmarks

func BenchmarkMemoryAllocations(b *testing.B) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		b.Skip("test file not found")
	}
	defer f.Close()

	if err != nil {
		b.Fatal(err)
	}

	page := r.Page(1)

	b.Run("Original", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = page.GetPlainText(nil)
		}
	})

	b.Run("Optimized", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = page.OptimizedGetPlainText(nil)
		}
	})
}
