// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"bufio"
	"context"
	"io"
	"sync"
)

// StreamProcessor handles streaming processing of PDF content to minimize memory usage
type StreamProcessor struct {
	chunkSize    int           // Size of processing chunks
	bufferSize   int           // Size of internal buffers
	maxMemory    int64         // Maximum memory to use
	currentUsage int64         // Current memory usage
	mu           sync.Mutex    // Mutex for memory tracking
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewStreamProcessor creates a new streaming processor
func NewStreamProcessor(chunkSize, bufferSize int, maxMemory int64) *StreamProcessor {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &StreamProcessor{
		chunkSize:  chunkSize,
		bufferSize: bufferSize,
		maxMemory:  maxMemory,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Close releases resources used by the stream processor
func (sp *StreamProcessor) Close() {
	sp.cancel()
}

// TextStream represents a stream of text with metadata
type TextStream struct {
	Text       string
	PageNum    int
	Font       string
	FontSize   float64
	X, Y       float64
	W          float64
	Vertical   bool
	Confidence float64 // Confidence in the text recognition (0-1)
}

// ProcessTextStream processes text in a streaming fashion
func (sp *StreamProcessor) ProcessTextStream(reader *Reader, handler func(TextStream) error) error {
	totalPages := reader.NumPage()
	
	for pageNum := 1; pageNum <= totalPages; pageNum++ {
		select {
		case <-sp.ctx.Done():
			return sp.ctx.Err()
		default:
		}

		page := reader.Page(pageNum)
		content := page.Content()

		for _, text := range content.Text {
			select {
			case <-sp.ctx.Done():
				return sp.ctx.Err()
			default:
			}

			textStream := TextStream{
				Text:       text.S,
				PageNum:    pageNum,
				Font:       text.Font,
				FontSize:   text.FontSize,
				X:          text.X,
				Y:          text.Y,
				W:          text.W,
				Vertical:   text.Vertical,
				Confidence: 1.0, // Default confidence
			}

			if err := handler(textStream); err != nil {
				return err
			}
		}
	}

	return nil
}

// TextBlockStream represents a stream of text blocks
type TextBlockStream struct {
	Block   *TextBlock
	PageNum int
	Type    BlockType
	Level   int
	Text    string
}

// ProcessTextBlockStream processes text blocks in a streaming fashion
func (sp *StreamProcessor) ProcessTextBlockStream(reader *Reader, handler func(TextBlockStream) error) error {
	totalPages := reader.NumPage()
	
	for pageNum := 1; pageNum <= totalPages; pageNum++ {
		select {
		case <-sp.ctx.Done():
			return sp.ctx.Err()
		default:
		}

		page := reader.Page(pageNum)
		blocks, err := page.ClassifyTextBlocks()
		if err != nil {
			return err
		}

		for _, block := range blocks {
			select {
			case <-sp.ctx.Done():
				return sp.ctx.Err()
			default:
			}

			blockStream := TextBlockStream{
				Block: &TextBlock{
					Texts:       block.Content,
					MinX:        block.Bounds.Min.X,
					MaxX:        block.Bounds.Max.X,
					MinY:        block.Bounds.Min.Y,
					MaxY:        block.Bounds.Max.Y,
					AvgFontSize: calculateAvgFontSize(block.Content),
				},
				PageNum: pageNum,
				Type:    block.Type,
				Level:   block.Level,
				Text:    block.Text,
			}

			if err := handler(blockStream); err != nil {
				return err
			}
		}
	}

	return nil
}

// calculateAvgFontSize calculates the average font size of a text slice
func calculateAvgFontSize(texts []Text) float64 {
	if len(texts) == 0 {
		return 0
	}

	var total float64
	for _, t := range texts {
		total += t.FontSize
	}
	return total / float64(len(texts))
}

// PageStream represents a stream of pages
type PageStream struct {
	Page    Page
	PageNum int
	HasText bool
	TextCount int
}

// ProcessPageStream processes pages in a streaming fashion
func (sp *StreamProcessor) ProcessPageStream(reader *Reader, handler func(PageStream) error) error {
	totalPages := reader.NumPage()
	
	for pageNum := 1; pageNum <= totalPages; pageNum++ {
		select {
		case <-sp.ctx.Done():
			return sp.ctx.Err()
		default:
		}

		page := reader.Page(pageNum)
		content := page.Content()

		pageStream := PageStream{
			Page:      page,
			PageNum:   pageNum,
			HasText:   len(content.Text) > 0,
			TextCount: len(content.Text),
		}

		if err := handler(pageStream); err != nil {
			return err
		}
	}

	return nil
}

// MemoryEfficientExtractor provides memory-efficient extraction using streaming
type MemoryEfficientExtractor struct {
	processor *StreamProcessor
}

// NewMemoryEfficientExtractor creates a new memory-efficient extractor
func NewMemoryEfficientExtractor(chunkSize, bufferSize int, maxMemory int64) *MemoryEfficientExtractor {
	return &MemoryEfficientExtractor{
		processor: NewStreamProcessor(chunkSize, bufferSize, maxMemory),
	}
}

// ExtractTextStream extracts text in a memory-efficient streaming way
func (mee *MemoryEfficientExtractor) ExtractTextStream(reader *Reader) (<-chan TextStream, <-chan error) {
	textChan := make(chan TextStream, 10)
	errChan := make(chan error, 1)

	go func() {
		defer close(textChan)
		defer close(errChan)

		err := mee.processor.ProcessTextStream(reader, func(ts TextStream) error {
			select {
			case textChan <- ts:
				return nil
			case <-mee.processor.ctx.Done():
				return mee.processor.ctx.Err()
			}
		})

		if err != nil {
			errChan <- err
		} else {
			errChan <- nil
		}
	}()

	return textChan, errChan
}

// ExtractTextToWriter extracts text directly to an io.Writer to minimize memory usage
func (mee *MemoryEfficientExtractor) ExtractTextToWriter(reader *Reader, writer io.Writer) error {
	// Create a buffered writer for better performance
	bufWriter := bufio.NewWriterSize(writer, mee.processor.bufferSize)
	defer bufWriter.Flush()

	// Process by pages to avoid loading all text into memory
	totalPages := reader.NumPage()
	
	for pageNum := 1; pageNum <= totalPages; pageNum++ {
		select {
		case <-mee.processor.ctx.Done():
			return mee.processor.ctx.Err()
		default:
		}

		page := reader.Page(pageNum)
		content := page.Content()

		// Group texts by lines and write them incrementally
		lines := groupTextsByLines(content.Text)
		
		for _, line := range lines {
			select {
			case <-mee.processor.ctx.Done():
				return mee.processor.ctx.Err()
			default:
			}

			lineText := buildLineText(line)
			if _, err := bufWriter.WriteString(lineText); err != nil {
				return err
			}
			if _, err := bufWriter.WriteString("\n"); err != nil {
				return err
			}
		}
	}

	return nil
}

// groupTextsByLines groups texts into lines based on Y position
func groupTextsByLines(texts []Text) [][]Text {
	if len(texts) == 0 {
		return [][]Text{}
	}

	// Sort texts by Y position (top to bottom)
	sortedTexts := make([]Text, len(texts))
	copy(sortedTexts, texts)
	
	// Use a simple sorting algorithm - for very large arrays,
	// a more efficient algorithm may be needed
	for i := 0; i < len(sortedTexts); i++ {
		for j := i + 1; j < len(sortedTexts); j++ {
			if sortedTexts[i].Y < sortedTexts[j].Y {
				sortedTexts[i], sortedTexts[j] = sortedTexts[j], sortedTexts[i]
			}
		}
	}

	// Group by lines with tolerance
	const lineTolerance = 2.0
	var lines [][]Text
	var currentLine []Text
	var currentY float64

	for i, t := range sortedTexts {
		if i == 0 {
			currentLine = []Text{t}
			currentY = t.Y
			continue
		}

		if abs(t.Y-currentY) <= lineTolerance {
			currentLine = append(currentLine, t)
		} else {
			if len(currentLine) > 0 {
				lines = append(lines, currentLine)
			}
			currentLine = []Text{t}
			currentY = t.Y
		}
	}
	if len(currentLine) > 0 {
		lines = append(lines, currentLine)
	}

	return lines
}

// buildLineText constructs a line of text from multiple text elements
func buildLineText(texts []Text) string {
	if len(texts) == 0 {
		return ""
	}

	// Sort texts by X position (left to right) within the line
	sortedTexts := make([]Text, len(texts))
	copy(sortedTexts, texts)
	
	for i := 0; i < len(sortedTexts); i++ {
		for j := i + 1; j < len(sortedTexts); j++ {
			if sortedTexts[i].X > sortedTexts[j].X {
				sortedTexts[i], sortedTexts[j] = sortedTexts[j], sortedTexts[i]
			}
		}
	}

	var result string
	var lastX float64
	for i, t := range sortedTexts {
		if i > 0 {
			// Add space if there's a significant gap
			gap := t.X - lastX
			if gap > 5.0 { // Threshold for adding space
				result += " "
			}
		}
		result += t.S
		lastX = t.X + t.W
	}

	return result
}

// abs returns the absolute value of a float64
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// StreamingTextClassifier classifies text in a streaming fashion to minimize memory usage
type StreamingTextClassifier struct {
	processor *StreamProcessor
}

// NewStreamingTextClassifier creates a new streaming text classifier
func NewStreamingTextClassifier(chunkSize, bufferSize int, maxMemory int64) *StreamingTextClassifier {
	return &StreamingTextClassifier{
		processor: NewStreamProcessor(chunkSize, bufferSize, maxMemory),
	}
}

// ClassifyTextStream classifies text in a streaming way
func (stc *StreamingTextClassifier) ClassifyTextStream(reader *Reader) (<-chan ClassifiedBlock, <-chan error) {
	blockChan := make(chan ClassifiedBlock, 5) // Smaller buffer for memory efficiency
	errChan := make(chan error, 1)

	go func() {
		defer close(blockChan)
		defer close(errChan)

		err := stc.processor.ProcessTextBlockStream(reader, func(tbs TextBlockStream) error {
			select {
			case blockChan <- ClassifiedBlock{
				Type:    tbs.Type,
				Level:   tbs.Level,
				Content: tbs.Block.Texts,
				Bounds:  tbs.Block.Bounds(),
				Text:    tbs.Text,
			}:
				return nil
			case <-stc.processor.ctx.Done():
				return stc.processor.ctx.Err()
			}
		})

		if err != nil {
			errChan <- err
		} else {
			errChan <- nil
		}
	}()

	return blockChan, errChan
}

// ProcessLargePDF handles very large PDFs with streaming
func ProcessLargePDF(reader *Reader, chunkSize, bufferSize int, maxMemory int64, 
	handler func(PageStream) error) error {
	
	extractor := NewMemoryEfficientExtractor(chunkSize, bufferSize, maxMemory)
	defer extractor.processor.Close()
	
	return extractor.processor.ProcessPageStream(reader, handler)
}

// StreamingMetadataExtractor extracts metadata in a streaming fashion
type StreamingMetadataExtractor struct {
	processor *StreamProcessor
}

// NewStreamingMetadataExtractor creates a new streaming metadata extractor
func NewStreamingMetadataExtractor(chunkSize, bufferSize int, maxMemory int64) *StreamingMetadataExtractor {
	return &StreamingMetadataExtractor{
		processor: NewStreamProcessor(chunkSize, bufferSize, maxMemory),
	}
}

// ExtractMetadataStream extracts metadata in a streaming way
func (sme *StreamingMetadataExtractor) ExtractMetadataStream(reader *Reader) (<-chan Metadata, <-chan error) {
	metaChan := make(chan Metadata, 1)
	errChan := make(chan error, 1)

	go func() {
		defer close(metaChan)
		defer close(errChan)

		metadata, err := reader.GetMetadata()
		if err != nil {
			errChan <- err
			return
		}

		select {
		case metaChan <- metadata:
		case <-sme.processor.ctx.Done():
			errChan <- sme.processor.ctx.Err()
		}

		errChan <- nil
	}()

	return metaChan, errChan
}