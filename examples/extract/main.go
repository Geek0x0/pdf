// Example: Extract text from a PDF file with various methods
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/Geek0x0/pdf"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: example <pdf-file>")
		os.Exit(1)
	}

	filename := os.Args[1]

	// Example 1: Basic text extraction
	fmt.Println("=== Example 1: Basic Text Extraction ===")
	basicExtraction(filename)

	// Example 2: Extract with context and timeout
	fmt.Println("\n=== Example 2: Extraction with Context ===")
	extractWithContext(filename)

	// Example 3: Extract styled text
	fmt.Println("\n=== Example 3: Styled Text Extraction ===")
	styledExtraction(filename)

	// Example 4: Extract by rows
	fmt.Println("\n=== Example 4: Extract by Rows ===")
	extractByRows(filename)
}

func basicExtraction(filename string) {
	f, r, err := pdf.Open(filename)
	if err != nil {
		log.Printf("Error opening PDF: %v", err)
		return
	}
	defer f.Close()

	fmt.Printf("PDF has %d pages\n", r.NumPage())

	// Extract all text
	reader, err := r.GetPlainText()
	if err != nil {
		log.Printf("Error extracting text: %v", err)
		return
	}

	text, _ := io.ReadAll(reader)
	fmt.Printf("Extracted %d bytes of text\n", len(text))

	// Print first 200 characters
	if len(text) > 200 {
		fmt.Printf("First 200 chars: %s...\n", text[:200])
	} else {
		fmt.Printf("Text: %s\n", text)
	}
}

func extractWithContext(filename string) {
	f, r, err := pdf.Open(filename)
	if err != nil {
		log.Printf("Error opening PDF: %v", err)
		return
	}
	defer f.Close()

	// Create context with 30-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Extract with 4 workers
	opts := pdf.ExtractOptions{
		Workers: 4,
	}

	start := time.Now()
	reader, err := r.ExtractWithContext(ctx, opts)
	if err != nil {
		log.Printf("Error extracting text: %v", err)
		return
	}

	text, _ := io.ReadAll(reader)
	elapsed := time.Since(start)

	fmt.Printf("Extracted %d bytes in %v using 4 workers\n", len(text), elapsed)
}

func styledExtraction(filename string) {
	f, r, err := pdf.Open(filename)
	if err != nil {
		log.Printf("Error opening PDF: %v", err)
		return
	}
	defer f.Close()

	texts, err := r.GetStyledTexts()
	if err != nil {
		log.Printf("Error extracting styled text: %v", err)
		return
	}

	fmt.Printf("Found %d text segments\n", len(texts))

	// Show first 5 segments with styling info
	count := 5
	if len(texts) < count {
		count = len(texts)
	}

	for i := 0; i < count; i++ {
		t := texts[i]
		fmt.Printf("  [%s %.1fpt] %.50s\n", t.Font, t.FontSize, t.S)
	}
}

func extractByRows(filename string) {
	f, r, err := pdf.Open(filename)
	if err != nil {
		log.Printf("Error opening PDF: %v", err)
		return
	}
	defer f.Close()

	if r.NumPage() < 1 {
		fmt.Println("PDF has no pages")
		return
	}

	page := r.Page(1)
	rows, err := page.GetTextByRow()
	if err != nil {
		log.Printf("Error extracting rows: %v", err)
		return
	}

	fmt.Printf("Found %d rows on page 1\n", len(rows))

	// Show first 5 rows
	count := 5
	if len(rows) < count {
		count = len(rows)
	}

	for i := 0; i < count; i++ {
		row := rows[i]
		var rowText string
		for _, t := range row.Content {
			rowText += t.S
		}
		fmt.Printf("  Row %d (Y=%d): %.60s\n", i+1, row.Position, rowText)
	}
}
