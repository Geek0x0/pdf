package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"runtime"
	"time"

	pdf "github.com/Geek0x0/pdf"
)

// This example focuses on maximum throughput for text extraction:
// - optional pool warmup and cache preallocation
// - optimized font cache + smart ordering
// - concurrent batch extraction across all pages
func main() {
	file := flag.String("file", "", "目标 PDF 路径")
	workers := flag.Int("workers", runtime.NumCPU(), "并发 worker 数（0 表示自动）")
	smart := flag.Bool("smart", true, "是否开启智能版面排序")
	warmup := flag.Bool("warmup", true, "是否在启动时预热内存池并预分配缓存")
	fontCacheSize := flag.Int("fontcache", 2000, "字体缓存容量（UseFontCache=true 时生效）")
	flag.Parse()

	if *file == "" {
		log.Fatal("请使用 -file 指定 PDF 文件路径")
	}

	if *warmup {
		if err := pdf.OptimizedStartup(pdf.DefaultStartupConfig()); err != nil {
			log.Printf("预热失败（继续运行）: %v", err)
		}
	}

	f, reader, err := pdf.Open(*file)
	if err != nil {
		log.Fatalf("无法打开文件: %v", err)
	}
	defer f.Close()

	opts := pdf.BatchExtractOptions{
		Workers:       *workers,
		SmartOrdering: *smart,
		Context:       context.Background(),
		UseFontCache:  true,
		FontCacheSize: *fontCacheSize,
		FontCacheType: pdf.FontCacheOptimized,
	}

	start := time.Now()
	text, err := reader.ExtractPagesBatchToString(opts)
	elapsed := time.Since(start)
	if err != nil {
		log.Fatalf("提取失败: %v", err)
	}

	fmt.Printf("提取完成：页数=%d，字符数=%d，耗时=%s，workers=%d，智能排序=%v\n",
		reader.NumPage(), len(text), elapsed, opts.Workers, opts.SmartOrdering)
}
