package main

import (
	"fmt"
	"log"

	"github.com/Geek0x0/pdf"
)

func main() {
	f, reader, err := pdf.Open("../../data/1.pdf")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	if reader.NumPage() == 0 {
		log.Fatal("No pages in PDF")
	}

	page := reader.Page(1)
	content := page.Content()

	fmt.Println("First 10 text runs with coordinates:")
	for i, t := range content.Text {
		if i >= 10 {
			break
		}
		fmt.Printf("[%d] Y=%.2f X=%.2f Font=%.1f Text=%q\n", i, t.Y, t.X, t.FontSize, t.S)
	}
}
