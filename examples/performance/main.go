// Example demonstrating performance optimization features
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Geek0x0/pdf"
)

func main() {
	// Open PDF file
	f, r, err := pdf.Open("../../data/1.pdf")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	fmt.Println("=== Performance Optimization Demo ===")

	// Example 1: Optimized text extraction
	fmt.Println("1. Optimized Text Extraction")
	page := r.Page(1)

	start := time.Now()
	text, err := page.OptimizedGetPlainText(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	elapsed := time.Since(start)

	fmt.Printf("   Extracted %d characters in %v\n", len(text), elapsed)
	fmt.Printf("   First 100 chars: %s...\n\n", text[:min(100, len(text))])

	// Example 2: Batch extraction
	fmt.Println("2. Batch Extraction")
	pageNums := []int{1}
	if r.NumPage() >= 2 {
		pageNums = append(pageNums, 2)
	}

	start = time.Now()
	results, err := r.BatchExtractText(pageNums, true) // Use lazy loading
	if err != nil {
		log.Fatal(err)
	}
	elapsed = time.Since(start)

	fmt.Printf("   Extracted %d pages in %v\n", len(results), elapsed)
	for pageNum, text := range results {
		fmt.Printf("   Page %d: %d characters\n", pageNum, len(text))
	}
	fmt.Println()

	// Example 3: Streaming extraction
	fmt.Println("3. Streaming Extraction")
	extractor := pdf.NewStreamingTextExtractor(r, 5)
	defer extractor.Close()

	start = time.Now()
	pageCount := 0
	totalChars := 0

	for {
		pageNum, text, hasMore, err := extractor.NextPage()
		if err != nil {
			log.Fatal(err)
		}

		if pageNum > 0 {
			pageCount++
			totalChars += len(text)

			progress := extractor.GetProgress()
			fmt.Printf("   Progress: %.1f%% - Page %d: %d chars\n",
				progress*100, pageNum, len(text))
		}

		if !hasMore {
			break
		}
	}
	elapsed = time.Since(start)

	fmt.Printf("   Total: %d pages, %d characters in %v\n\n",
		pageCount, totalChars, elapsed)

	// Example 4: Lazy page manager
	fmt.Println("4. Lazy Page Manager")
	manager := pdf.NewLazyPageManager(r, 3) // Cache max 3 pages
	defer manager.Clear()

	start = time.Now()
	for i := 1; i <= min(5, r.NumPage()); i++ {
		lazyPage := manager.GetPage(i)
		content := lazyPage.GetContent()
		fmt.Printf("   Page %d loaded: %d text runs\n", i, len(content.Text))
	}
	elapsed = time.Since(start)

	total, loaded := manager.GetStats()
	fmt.Printf("   Stats: %d total pages, %d loaded in memory\n", total, loaded)
	fmt.Printf("   Time: %v\n\n", elapsed)

	// Example 5: Using object pools
	fmt.Println("5. Object Pool Usage")
	builder := pdf.GetBuilder()
	defer pdf.PutBuilder(builder)

	builder.WriteString("Example text from object pool")
	result := builder.String()
	fmt.Printf("   Built string: %s\n", result)
	fmt.Printf("   Pool benefits: Reduced GC pressure and allocations\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
