package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/Geek0x0/pdf"
)

func main() {
	mode := flag.String("mode", "plain", "Extraction mode: plain, text, styled, rows, columns")
	page := flag.Int("page", 0, "Page number (required for rows/columns, optional for plain)")
	flag.Parse()

	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "Usage: pdfcli [options] file.pdf")
		flag.PrintDefaults()
		os.Exit(2)
	}

	filePath := flag.Arg(0)
	f, reader, err := pdf.Open(filePath)
	if err != nil {
		log.Fatalf("open %s: %v", filePath, err)
	}
	defer f.Close()

	switch strings.ToLower(*mode) {
	case "plain":
		handlePlain(reader, *page)
	case "text":
		handleText(reader, filePath)
	case "styled":
		handleStyled(reader)
	case "rows":
		requirePage(*page)
		handleRows(reader, *page)
	case "columns":
		requirePage(*page)
		handleColumns(reader, *page)
	default:
		log.Fatalf("unknown mode %q", *mode)
	}
}

func handlePlain(reader *pdf.Reader, page int) {
	if page <= 0 {
		out, err := reader.GetPlainText()
		if err != nil {
			log.Fatalf("GetPlainText: %v", err)
		}
		if _, err := io.Copy(os.Stdout, out); err != nil {
			log.Fatalf("write output: %v", err)
		}
		return
	}
	text, err := reader.Page(page).GetPlainText(nil)
	if err != nil {
		log.Fatalf("Page(%d).GetPlainText: %v", page, err)
	}
	fmt.Print(text)
}

func handleText(reader *pdf.Reader, filePath string) {
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		text, err := page.GetPlainText(nil)
		if err != nil {
			log.Fatalf("Page(%d).GetPlainText: %v", i, err)
		}
		if !isReadable(text) {
			if ocrText, err := ocrPage(filePath, i); err == nil && len(strings.TrimSpace(ocrText)) > 0 {
				text = ocrText
			} else if err != nil {
				log.Printf("OCR fallback failed for page %d: %v", i, err)
			}
		}
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		fmt.Print(text)
	}
}

func handleStyled(reader *pdf.Reader) {
	styled, err := reader.GetStyledTexts()
	if err != nil {
		log.Fatalf("GetStyledTexts: %v", err)
	}
	for _, text := range styled {
		fmt.Printf("[%s size=%.2f x=%.2f y=%.2f] %s\n", text.Font, text.FontSize, text.X, text.Y, text.S)
	}
}

func handleRows(reader *pdf.Reader, page int) {
	rows, err := reader.Page(page).GetTextByRow()
	if err != nil {
		log.Fatalf("GetTextByRow: %v", err)
	}
	for _, row := range rows {
		var builder strings.Builder
		for _, t := range row.Content {
			builder.WriteString(t.S)
		}
		fmt.Printf("Row %d: %s\n", row.Position, builder.String())
	}
}

func handleColumns(reader *pdf.Reader, page int) {
	columns, err := reader.Page(page).GetTextByColumn()
	if err != nil {
		log.Fatalf("GetTextByColumn: %v", err)
	}
	for _, col := range columns {
		var builder strings.Builder
		for _, t := range col.Content {
			builder.WriteString(t.S)
			builder.WriteString("\n")
		}
		fmt.Printf("Column %d:\n%s\n", col.Position, builder.String())
	}
}

func requirePage(page int) {
	if page <= 0 {
		log.Fatal("the -page flag must be specified for this mode")
	}
}

func isReadable(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	spaceRatio := float64(strings.Count(s, " ")) / float64(len(s))
	if spaceRatio < 0.02 {
		return false
	}
	var wordCount, run int
	for _, r := range s {
		if unicode.IsLetter(r) {
			run++
		} else {
			if run >= 4 {
				wordCount++
			}
			run = 0
		}
	}
	if run >= 4 {
		wordCount++
	}
	return wordCount >= 3
}

func ocrPage(filePath string, page int) (string, error) {
	tmpDir, err := os.MkdirTemp("", "pdfcli-ocr")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	prefix := filepath.Join(tmpDir, "page")
	args := []string{
		"-singlefile",
		"-png",
		"-f", strconv.Itoa(page),
		"-l", strconv.Itoa(page),
		filePath,
		prefix,
	}
	if err := exec.Command("pdftoppm", args...).Run(); err != nil {
		return "", fmt.Errorf("pdftoppm: %w", err)
	}
	pngPath := prefix + ".png"
	cmd := exec.Command("tesseract", pngPath, "stdout")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tesseract: %w", err)
	}
	return string(out), nil
}
