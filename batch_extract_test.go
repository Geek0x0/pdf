// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"testing"
)

func TestBatchExtractBasic(t *testing.T) {
	// This test requires a real PDF file
	// Skip if we don't have test data
	t.Skip("Requires test PDF file")

	// Example usage:
	// r, err := Open("test.pdf")
	// if err != nil {
	//     t.Fatal(err)
	// }
	// defer r.Close()
	//
	// opts := BatchExtractOptions{
	//     Workers: 4,
	// }
	//
	// text, err := r.ExtractPagesBatchToString(opts)
	// if err != nil {
	//     t.Fatal(err)
	// }
	//
	// if len(text) == 0 {
	//     t.Error("Expected non-empty text")
	// }
}

func TestBatchExtractWithContext(t *testing.T) {
	t.Skip("Requires test PDF file")

	// Example with cancellation:
	// ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// defer cancel()
	//
	// opts := BatchExtractOptions{
	//     Context: ctx,
	//     Workers: 4,
	// }
	//
	// resultChan := r.ExtractPagesBatch(opts)
	// for result := range resultChan {
	//     if result.Error != nil {
	//         t.Errorf("Page %d error: %v", result.PageNum, result.Error)
	//     }
	// }
}

func TestSortPageResults(t *testing.T) {
	tests := []struct {
		name     string
		input    []pageResult
		expected []int
	}{
		{
			name: "already sorted",
			input: []pageResult{
				{pageNum: 1, text: "a"},
				{pageNum: 2, text: "b"},
				{pageNum: 3, text: "c"},
			},
			expected: []int{1, 2, 3},
		},
		{
			name: "reverse sorted",
			input: []pageResult{
				{pageNum: 3, text: "c"},
				{pageNum: 2, text: "b"},
				{pageNum: 1, text: "a"},
			},
			expected: []int{1, 2, 3},
		},
		{
			name: "random order",
			input: []pageResult{
				{pageNum: 5, text: "e"},
				{pageNum: 2, text: "b"},
				{pageNum: 8, text: "h"},
				{pageNum: 1, text: "a"},
				{pageNum: 3, text: "c"},
			},
			expected: []int{1, 2, 3, 5, 8},
		},
		{
			name:     "empty",
			input:    []pageResult{},
			expected: []int{},
		},
		{
			name: "single element",
			input: []pageResult{
				{pageNum: 1, text: "a"},
			},
			expected: []int{1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortPageResults(tt.input)

			if len(tt.input) != len(tt.expected) {
				t.Fatalf("Length mismatch: got %d, want %d", len(tt.input), len(tt.expected))
			}

			for i, result := range tt.input {
				if result.pageNum != tt.expected[i] {
					t.Errorf("Index %d: got page %d, want %d", i, result.pageNum, tt.expected[i])
				}
			}
		})
	}
}

func TestStreamingBatchExtractor(t *testing.T) {
	t.Skip("Requires test PDF file")

	// Example usage:
	// r, _ := Open("test.pdf")
	// defer r.Close()
	//
	// opts := BatchExtractOptions{Workers: 4}
	// extractor := NewStreamingBatchExtractor(r, opts)
	// extractor.Start()
	//
	// count := 0
	// for result := extractor.Next(); result != nil; result = extractor.Next() {
	//     if result.Error != nil {
	//         t.Errorf("Error on page %d: %v", result.PageNum, result.Error)
	//     }
	//     count++
	// }
	//
	// if count != r.NumPage() {
	//     t.Errorf("Got %d results, expected %d", count, r.NumPage())
	// }
}

func BenchmarkBatchExtractVsSequential(b *testing.B) {
	b.Skip("Requires test PDF file")

	// This benchmark compares batch vs sequential extraction
	// Example implementation:
	//
	// r, err := Open("test.pdf")
	// if err != nil {
	//     b.Fatal(err)
	// }
	// defer r.Close()
	//
	// b.Run("Sequential", func(b *testing.B) {
	//     for i := 0; i < b.N; i++ {
	//         for p := 1; p <= r.NumPage(); p++ {
	//             page := r.Page(p)
	//             _, err := page.GetPlainText(nil)
	//             if err != nil {
	//                 b.Fatal(err)
	//             }
	//         }
	//     }
	// })
	//
	// b.Run("Batch4Workers", func(b *testing.B) {
	//     for i := 0; i < b.N; i++ {
	//         opts := BatchExtractOptions{Workers: 4}
	//         _, err := r.ExtractPagesBatchToString(opts)
	//         if err != nil {
	//             b.Fatal(err)
	//         }
	//     }
	// })
	//
	// b.Run("Batch8Workers", func(b *testing.B) {
	//     for i := 0; i < b.N; i++ {
	//         opts := BatchExtractOptions{Workers: 8}
	//         _, err := r.ExtractPagesBatchToString(opts)
	//         if err != nil {
	//             b.Fatal(err)
	//         }
	//     }
	// })
}

func TestBatchExtractCancellation(t *testing.T) {
	t.Skip("Requires test PDF file")

	// Test that cancellation works correctly:
	// ctx, cancel := context.WithCancel(context.Background())
	//
	// opts := BatchExtractOptions{
	//     Context: ctx,
	//     Workers: 4,
	// }
	//
	// resultChan := r.ExtractPagesBatch(opts)
	//
	// // Cancel after receiving first result
	// result := <-resultChan
	// cancel()
	//
	// // Count remaining results (should be interrupted)
	// count := 1
	// for range resultChan {
	//     count++
	// }
	//
	// if count == r.NumPage() {
	//     t.Error("Cancellation did not interrupt processing")
	// }
}

func TestBatchExtractOptions(t *testing.T) {
	// Test option validation and defaults
	opts := BatchExtractOptions{}

	// Workers should default to 0 (will be set to NumCPU)
	if opts.Workers != 0 {
		t.Errorf("Expected default Workers to be 0, got %d", opts.Workers)
	}

	// Context can be nil (will be set to Background)
	if opts.Context != nil {
		t.Error("Expected default Context to be nil")
	}

	// SmartOrdering defaults to false
	if opts.SmartOrdering {
		t.Error("Expected default SmartOrdering to be false")
	}
}

func TestPageResultType(t *testing.T) {
	// Test the pageResult type
	pr := pageResult{
		pageNum: 5,
		text:    "test text",
	}

	if pr.pageNum != 5 {
		t.Errorf("pageNum = %d, want 5", pr.pageNum)
	}

	if pr.text != "test text" {
		t.Errorf("text = %q, want %q", pr.text, "test text")
	}
}

func TestBatchResultType(t *testing.T) {
	// Test the BatchResult type
	br := BatchResult{
		PageNum: 3,
		Text:    "sample",
		Error:   nil,
	}

	if br.PageNum != 3 {
		t.Errorf("PageNum = %d, want 3", br.PageNum)
	}

	if br.Text != "sample" {
		t.Errorf("Text = %q, want %q", br.Text, "sample")
	}

	if br.Error != nil {
		t.Errorf("Error = %v, want nil", br.Error)
	}
}

// Benchmark sorting algorithms
func BenchmarkSortPageResults(b *testing.B) {
	sizes := []int{10, 50, 100, 500, 1000}

	for _, size := range sizes {
		b.Run(string(rune(size)), func(b *testing.B) {
			// Create unsorted data
			data := make([]pageResult, size)
			for i := 0; i < size; i++ {
				data[i] = pageResult{
					pageNum: size - i,
					text:    "test",
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Make a copy to sort
				testData := make([]pageResult, size)
				copy(testData, data)
				sortPageResults(testData)
			}
		})
	}
}

func ExampleReader_ExtractPagesBatch() {
	// This example shows how to use batch extraction
	// (requires a real PDF file to run)

	// r, err := Open("document.pdf")
	// if err != nil {
	//     log.Fatal(err)
	// }
	// defer r.Close()
	//
	// opts := BatchExtractOptions{
	//     Workers: 4,
	//     Pages:   []int{1, 2, 3, 4, 5}, // Extract first 5 pages
	// }
	//
	// for result := range r.ExtractPagesBatch(opts) {
	//     if result.Error != nil {
	//         log.Printf("Error on page %d: %v", result.PageNum, result.Error)
	//         continue
	//     }
	//     fmt.Printf("Page %d: %d characters\n", result.PageNum, len(result.Text))
	// }
}

func ExampleReader_ExtractPagesBatchToString() {
	// This example shows how to extract all pages to a single string

	// r, err := Open("document.pdf")
	// if err != nil {
	//     log.Fatal(err)
	// }
	// defer r.Close()
	//
	// opts := BatchExtractOptions{
	//     Workers:       8,
	//     SmartOrdering: true,
	// }
	//
	// text, err := r.ExtractPagesBatchToString(opts)
	// if err != nil {
	//     log.Fatal(err)
	// }
	//
	// fmt.Printf("Extracted %d characters from %d pages\n", len(text), r.NumPage())
}

func ExampleStreamingBatchExtractor() {
	// This example shows streaming batch extraction with a callback

	// r, err := Open("document.pdf")
	// if err != nil {
	//     log.Fatal(err)
	// }
	// defer r.Close()
	//
	// ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	// defer cancel()
	//
	// opts := BatchExtractOptions{
	//     Context: ctx,
	//     Workers: 4,
	// }
	//
	// extractor := NewStreamingBatchExtractor(r, opts)
	// extractor.Start()
	//
	// err = extractor.ProcessAll(func(result BatchResult) error {
	//     if result.Error != nil {
	//         return result.Error
	//     }
	//     // Process each page as it arrives
	//     fmt.Printf("Processing page %d...\n", result.PageNum)
	//     return nil
	// })
	//
	// if err != nil {
	//     log.Fatal(err)
	// }
}
