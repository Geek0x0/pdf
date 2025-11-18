package main

import (
	"fmt"
	"log"
	"os"
	"pdf"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <pdf-file> [--smart]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		fmt.Fprintf(os.Stderr, "  --smart    Use smart text ordering (better for multi-column layouts)\n")
		os.Exit(1)
	}

	filePath := os.Args[1]
	useSmart := len(os.Args) > 2 && os.Args[2] == "--smart"

	f, reader, err := pdf.Open(filePath)
	if err != nil {
		log.Fatalf("Error opening PDF: %v", err)
	}
	defer f.Close()

	numPages := reader.NumPage()
	if numPages == 0 {
		fmt.Println("PDF has no pages")
		return
	}

	fmt.Printf("PDF has %d pages\n", numPages)
	if useSmart {
		fmt.Println("Using smart text ordering (multi-column aware)")
	} else {
		fmt.Println("Using simple text ordering")
	}
	fmt.Println(strings.Repeat("=", 60))

	for i := 1; i <= numPages; i++ {
		page := reader.Page(i)

		var text string
		if useSmart {
			text, err = page.GetPlainTextWithSmartOrdering(nil)
		} else {
			text, err = page.GetPlainText(nil)
		}

		if err != nil {
			log.Printf("Error extracting text from page %d: %v", i, err)
			continue
		}

		if text == "" {
			fmt.Printf("\n--- Page %d (empty) ---\n", i)
			continue
		}

		fmt.Printf("\n--- Page %d ---\n", i)
		fmt.Println(text)
	}
}
