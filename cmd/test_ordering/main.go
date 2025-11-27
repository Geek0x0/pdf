package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/Geek0x0/pdf"
)

func main() {
	f, reader, err := pdf.Open("../../data/2.pdf")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	if reader.NumPage() == 0 {
		log.Fatal("No pages in PDF")
	}

	page := reader.Page(1)

	// Test simple ordering
	plainText, err := page.GetPlainText(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}

	// Test smart ordering
	smartText, err := page.GetPlainTextWithSmartOrdering(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("=== Simple Ordering (first 500 chars) ===")
	if len(plainText) > 500 {
		fmt.Println(plainText[:500])
	} else {
		fmt.Println(plainText)
	}

	fmt.Println("\n=== Smart Ordering (first 500 chars) ===")
	if len(smartText) > 500 {
		fmt.Println(smartText[:500])
	} else {
		fmt.Println(smartText)
	}

	fmt.Println("\n=== Comparison ===")
	fmt.Printf("Simple ordering length: %d\n", len(plainText))
	fmt.Printf("Smart ordering length: %d\n", len(smartText))

	// Check if they're the same
	if plainText == smartText {
		fmt.Println("Results are identical")
	} else {
		// Count differences
		simpleLines := strings.Split(plainText, "\n")
		smartLines := strings.Split(smartText, "\n")
		fmt.Printf("Simple lines: %d\n", len(simpleLines))
		fmt.Printf("Smart lines: %d\n", len(smartLines))
	}
}
