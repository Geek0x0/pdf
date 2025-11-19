// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	// Uncomment when using with actual PDF files:
	// "context"
	// "log"
	// "time"
	// "github.com/Geek0x0/pdf"
)

func main() {
	// Example 1: Batch extraction WITHOUT font cache
	fmt.Println("=== Example 1: Batch extraction without font cache ===")
	extractWithoutCache()

	fmt.Println("\n=== Example 2: Batch extraction WITH font cache ===")
	extractWithCache()

	fmt.Println("\n=== Example 3: Batch extraction with custom cache size ===")
	extractWithCustomCacheSize()

	fmt.Println("\n=== Example 4: Batch extraction with cancellation ===")
	extractWithCancellation()
}

// extractWithoutCache demonstrates basic batch extraction
func extractWithoutCache() {
	// Open PDF file
	// r, err := pdf.Open("sample.pdf")
	// if err != nil {
	//     log.Fatal(err)
	// }
	// defer r.Close()

	fmt.Println("Extracting text without font cache...")

	// Create a mock scenario for demonstration
	// In real usage, use actual PDF Reader:
	/*
		opts := pdf.BatchExtractOptions{
			Pages:         nil,  // Extract all pages
			Workers:       4,    // Use 4 concurrent workers
			SmartOrdering: true, // Use smart text ordering
			UseFontCache:  false, // Disable font cache
		}

		start := time.Now()
		resultChan := r.ExtractPagesBatch(opts)

		for result := range resultChan {
			if result.Error != nil {
				log.Printf("Error on page %d: %v", result.PageNum, result.Error)
				continue
			}
			fmt.Printf("Page %d: %d characters\n", result.PageNum, len(result.Text))
		}

		fmt.Printf("Extraction completed in %v\n", time.Since(start))
	*/

	fmt.Println("(Use actual PDF file in production)")
}

// extractWithCache demonstrates batch extraction with font caching enabled
func extractWithCache() {
	// Open PDF file
	// r, err := pdf.Open("sample.pdf")
	// if err != nil {
	//     log.Fatal(err)
	// }
	// defer r.Close()

	fmt.Println("Extracting text WITH font cache...")

	// Create a mock scenario for demonstration
	// In real usage, use actual PDF Reader:
	/*
		opts := pdf.BatchExtractOptions{
			Pages:         nil,   // Extract all pages
			Workers:       4,     // Use 4 concurrent workers
			SmartOrdering: true,  // Use smart text ordering
			UseFontCache:  true,  // ENABLE font cache for better performance
			FontCacheSize: 0,     // Use default cache size (1000 fonts)
		}

		start := time.Now()
		resultChan := r.ExtractPagesBatch(opts)

		for result := range resultChan {
			if result.Error != nil {
				log.Printf("Error on page %d: %v", result.PageNum, result.Error)
				continue
			}
			fmt.Printf("Page %d: %d characters\n", result.PageNum, len(result.Text))
		}

		fmt.Printf("Extraction completed in %v\n", time.Since(start))
		fmt.Println("Font cache automatically cleaned up after batch completion")
	*/

	fmt.Println("(Use actual PDF file in production)")
	fmt.Println("Expected performance improvement: 2-5x faster on PDFs with many repeated fonts")
}

// extractWithCustomCacheSize demonstrates using a custom cache size
func extractWithCustomCacheSize() {
	// Open PDF file
	// r, err := pdf.Open("large_document.pdf")
	// if err != nil {
	//     log.Fatal(err)
	// }
	// defer r.Close()

	fmt.Println("Extracting text with custom font cache size...")

	/*
		opts := pdf.BatchExtractOptions{
			Pages:         nil,    // Extract all pages
			Workers:       8,      // Use more workers for large documents
			SmartOrdering: true,
			UseFontCache:  true,   // Enable font cache
			FontCacheSize: 2000,   // Custom cache size for documents with many fonts
		}

		start := time.Now()

		// Using the convenience function that returns a single string
		text, err := r.ExtractPagesBatchToString(opts)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("Extracted %d characters in %v\n", len(text), time.Since(start))
	*/

	fmt.Println("(Use actual PDF file in production)")
	fmt.Println("Tip: Increase FontCacheSize for PDFs with many unique fonts (>1000)")
}

// extractWithCancellation demonstrates batch extraction with context cancellation
func extractWithCancellation() {
	// Open PDF file
	// r, err := pdf.Open("very_large.pdf")
	// if err != nil {
	//     log.Fatal(err)
	// }
	// defer r.Close()

	fmt.Println("Extracting text with cancellation support...")

	/*
		// Create a context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		opts := pdf.BatchExtractOptions{
			Pages:         nil,   // Extract all pages
			Workers:       4,
			SmartOrdering: true,
			UseFontCache:  true,  // Enable font cache
			Context:       ctx,   // Add cancellation support
		}

		start := time.Now()
		resultChan := r.ExtractPagesBatch(opts)

		processedPages := 0
		for result := range resultChan {
			if result.Error != nil {
				if result.Error == context.DeadlineExceeded {
					log.Printf("Extraction cancelled: timeout reached")
					break
				}
				log.Printf("Error on page %d: %v", result.PageNum, result.Error)
				continue
			}
			processedPages++
			fmt.Printf("Page %d: %d characters\n", result.PageNum, len(result.Text))
		}

		fmt.Printf("Processed %d pages in %v\n", processedPages, time.Since(start))
	*/

	fmt.Println("(Use actual PDF file in production)")
}

// Example: Performance comparison
func performanceComparison() {
	// This example shows how to benchmark with and without font cache

	/*
		r, err := pdf.Open("test_document.pdf")
		if err != nil {
			log.Fatal(err)
		}
		defer r.Close()

		// Test WITHOUT font cache
		fmt.Println("Testing WITHOUT font cache...")
		opts1 := pdf.BatchExtractOptions{
			Workers:       4,
			SmartOrdering: true,
			UseFontCache:  false,
		}
		start1 := time.Now()
		_, err = r.ExtractPagesBatchToString(opts1)
		if err != nil {
			log.Fatal(err)
		}
		duration1 := time.Since(start1)
		fmt.Printf("Without cache: %v\n", duration1)

		// Test WITH font cache
		fmt.Println("Testing WITH font cache...")
		opts2 := pdf.BatchExtractOptions{
			Workers:       4,
			SmartOrdering: true,
			UseFontCache:  true,
			FontCacheSize: 1000,
		}
		start2 := time.Now()
		_, err = r.ExtractPagesBatchToString(opts2)
		if err != nil {
			log.Fatal(err)
		}
		duration2 := time.Since(start2)
		fmt.Printf("With cache: %v\n", duration2)

		// Calculate speedup
		speedup := float64(duration1) / float64(duration2)
		fmt.Printf("Speedup: %.2fx\n", speedup)
	*/
}
