// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestParallelExtractorUsage 演示 ParallelExtractor 的实际用法
func TestParallelExtractorUsage(t *testing.T) {
	// 创建一个并行提取器（使用 4 个工作协程）
	extractor := NewParallelExtractor(4)
	defer extractor.Close()

	// 验证提取器创建成功
	if extractor == nil {
		t.Fatal("Failed to create ParallelExtractor")
	}

	// 验证组件初始化
	if extractor.processor == nil {
		t.Error("Processor not initialized")
	}
	if extractor.cache == nil {
		t.Error("Cache not initialized")
	}
	if extractor.prefetcher == nil {
		t.Error("Prefetcher not initialized")
	}

	// 测试统计信息获取
	cacheStats := extractor.GetCacheStats()
	if cacheStats.Hits < 0 || cacheStats.Misses < 0 {
		t.Error("Invalid cache stats")
	}

	prefetchStats := extractor.GetPrefetchStats()
	if !prefetchStats.Enabled {
		t.Error("Prefetcher should be enabled by default")
	}
}

// TestReaderExtractAllPagesParallel 演示 Reader 的并行提取方法
func TestReaderExtractAllPagesParallel(t *testing.T) {
	// 注意：这个测试需要一个实际的 PDF 文件
	// 在实际使用中，你需要提供一个有效的 PDF 路径
	t.Skip("Skipping test that requires actual PDF file")

	// 示例代码：
	/*
		f, r, err := Open("sample.pdf")
		if err != nil {
			t.Fatalf("Failed to open PDF: %v", err)
		}
		defer f.Close()

		// 创建上下文（带超时）
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// 使用并行提取器提取所有页面
		// workers=0 表示自动使用 runtime.NumCPU()
		pages, err := r.ExtractAllPagesParallel(ctx, 0)
		if err != nil {
			t.Fatalf("Failed to extract pages: %v", err)
		}

		// 处理结果
		for i, text := range pages {
			fmt.Printf("Page %d: %d characters\n", i+1, len(text))
		}
	*/
}

// ExampleParallelExtractor_basic 基本使用示例
func ExampleParallelExtractor_basic() {
	// 创建并行提取器
	extractor := NewParallelExtractor(4) // 使用 4 个工作协程
	defer extractor.Close()

	// 注意：实际使用需要创建 Page 对象
	// pages := []Page{...}

	ctx := context.Background()

	// 模拟空页面列表
	var pages []Page

	// 提取所有页面
	results, err := extractor.ExtractAllPages(ctx, pages)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Extracted %d pages\n", len(results))
	// Output: Extracted 0 pages
}

// ExampleReader_ExtractAllPagesParallel 使用 Reader 的并行提取方法
func ExampleReader_ExtractAllPagesParallel() {
	// 注意：这个示例需要实际的 PDF 文件
	// 这里仅展示 API 用法

	/*
		// 打开 PDF 文件
		f, r, err := Open("document.pdf")
		if err != nil {
			panic(err)
		}
		defer f.Close()

		// 创建上下文
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()

		// 并行提取所有页面文本
		pages, err := r.ExtractAllPagesParallel(ctx, 0) // 0 = 自动检测 CPU 核心数
		if err != nil {
			panic(err)
		}

		// 输出每页的文本
		for i, pageText := range pages {
			fmt.Printf("Page %d has %d characters\n", i+1, len(pageText))
		}
	*/
}

// BenchmarkParallelExtractorVsSequential 对比并行和顺序提取性能
func BenchmarkParallelExtractorVsSequential(b *testing.B) {
	// 创建模拟页面
	numPages := 100
	pages := make([]Page, numPages)

	b.Run("Sequential", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ctx := context.Background()
			for _, page := range pages {
				content := page.Content()
				_ = content.Text
			}
			_ = ctx
		}
	})

	b.Run("Parallel-2Workers", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			extractor := NewParallelExtractor(2)
			ctx := context.Background()
			_, _ = extractor.ExtractAllPages(ctx, pages)
			extractor.Close()
		}
	})

	b.Run("Parallel-4Workers", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			extractor := NewParallelExtractor(4)
			ctx := context.Background()
			_, _ = extractor.ExtractAllPages(ctx, pages)
			extractor.Close()
		}
	})

	b.Run("Parallel-AutoWorkers", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			extractor := NewParallelExtractor(0) // 自动检测
			ctx := context.Background()
			_, _ = extractor.ExtractAllPages(ctx, pages)
			extractor.Close()
		}
	})
}

// TestParallelExtractorCancellation 测试取消功能
func TestParallelExtractorCancellation(t *testing.T) {
	extractor := NewParallelExtractor(4)
	defer extractor.Close()

	// 创建可取消的上下文
	ctx, cancel := context.WithCancel(context.Background())

	// 立即取消
	cancel()

	// 尝试提取（应该快速失败）
	pages := make([]Page, 10)
	_, err := extractor.ExtractAllPages(ctx, pages)

	// 应该返回取消错误
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}
}

// TestParallelExtractorTimeout 测试超时功能
func TestParallelExtractorTimeout(t *testing.T) {
	extractor := NewParallelExtractor(2)
	defer extractor.Close()

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// 等待超时
	time.Sleep(10 * time.Millisecond)

	// 尝试提取（应该超时）
	pages := make([]Page, 10)
	_, err := extractor.ExtractAllPages(ctx, pages)

	// 应该返回超时错误
	if err != context.DeadlineExceeded {
		t.Errorf("Expected context.DeadlineExceeded error, got: %v", err)
	}
}

// TestParallelExtractorStats 测试统计信息收集
func TestParallelExtractorStats(t *testing.T) {
	extractor := NewParallelExtractor(4)
	defer extractor.Close()

	// 获取初始统计
	cacheStats := extractor.GetCacheStats()
	prefetchStats := extractor.GetPrefetchStats()

	// 验证统计结构
	if cacheStats.Hits != 0 {
		t.Errorf("Expected 0 initial hits, got %d", cacheStats.Hits)
	}
	if cacheStats.Misses != 0 {
		t.Errorf("Expected 0 initial misses, got %d", cacheStats.Misses)
	}
	if !prefetchStats.Enabled {
		t.Error("Prefetcher should be enabled by default")
	}
}
