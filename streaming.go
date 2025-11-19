// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
)

// StreamProcessor handles streaming processing of PDF content to minimize memory usage
type StreamProcessor struct {
	chunkSize    int        // Size of processing chunks
	bufferSize   int        // Size of internal buffers
	maxMemory    int64      // Maximum memory to use
	currentUsage int64      // Current memory usage
	mu           sync.Mutex // Mutex for memory tracking
	ctx          context.Context
	cancel       context.CancelFunc
}

var ErrMemoryLimitExceeded = errors.New("pdf: stream processor memory limit exceeded")

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

func (sp *StreamProcessor) tryReserveMemory(n int64) bool {
	if sp == nil || sp.maxMemory <= 0 || n <= 0 {
		return true
	}
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.currentUsage+n > sp.maxMemory {
		return false
	}
	sp.currentUsage += n
	return true
}

func (sp *StreamProcessor) releaseMemory(n int64) {
	if sp == nil || sp.maxMemory <= 0 || n <= 0 {
		return
	}
	sp.mu.Lock()
	sp.currentUsage -= n
	if sp.currentUsage < 0 {
		sp.currentUsage = 0
	}
	sp.mu.Unlock()
}

// Close releases resources used by the stream processor
func (sp *StreamProcessor) Close() {
	sp.cancel()
	sp.mu.Lock()
	sp.currentUsage = 0
	sp.mu.Unlock()
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

			memCost := estimateTextMemory(text)
			if !sp.tryReserveMemory(memCost) {
				return ErrMemoryLimitExceeded
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
				sp.releaseMemory(memCost)
				return err
			}
			sp.releaseMemory(memCost)
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

			blockCopy := &TextBlock{
				Texts:       block.Content,
				MinX:        block.Bounds.Min.X,
				MaxX:        block.Bounds.Max.X,
				MinY:        block.Bounds.Min.Y,
				MaxY:        block.Bounds.Max.Y,
				AvgFontSize: calculateAvgFontSize(block.Content),
			}
			blockStream := TextBlockStream{
				Block:   blockCopy,
				PageNum: pageNum,
				Type:    block.Type,
				Level:   block.Level,
				Text:    block.Text,
			}

			memCost := estimateBlockMemory(blockCopy)
			if !sp.tryReserveMemory(memCost) {
				return ErrMemoryLimitExceeded
			}

			if err := handler(blockStream); err != nil {
				sp.releaseMemory(memCost)
				return err
			}
			sp.releaseMemory(memCost)
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
	Page      Page
	PageNum   int
	HasText   bool
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

		memCost := estimatePageMemory(len(content.Text))
		if !sp.tryReserveMemory(memCost) {
			return ErrMemoryLimitExceeded
		}

		if err := handler(pageStream); err != nil {
			sp.releaseMemory(memCost)
			return err
		}
		sp.releaseMemory(memCost)
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
func (mee *MemoryEfficientExtractor) ExtractTextToWriter(reader *Reader, writer io.Writer) (err error) {
	bufWriter := bufio.NewWriterSize(writer, mee.processor.bufferSize)

	chunkThreshold := mee.processor.chunkSize
	if chunkThreshold <= 0 {
		chunkThreshold = mee.processor.bufferSize
	}
	if chunkThreshold <= 0 {
		chunkThreshold = 4096
	}
	if mee.processor.maxMemory > 0 && int64(chunkThreshold) > mee.processor.maxMemory {
		chunkThreshold = int(mee.processor.maxMemory)
	}

	var chunkBuilder strings.Builder
	var chunkReserved int64

	growReservation := func() error {
		additional := int64(chunkBuilder.Len()) - chunkReserved
		if additional <= 0 {
			return nil
		}
		if !mee.processor.tryReserveMemory(additional) {
			return ErrMemoryLimitExceeded
		}
		chunkReserved += additional
		return nil
	}

	flushChunk := func() error {
		if chunkBuilder.Len() == 0 {
			return nil
		}
		if _, err := bufWriter.WriteString(chunkBuilder.String()); err != nil {
			chunkBuilder.Reset()
			if chunkReserved > 0 {
				mee.processor.releaseMemory(chunkReserved)
				chunkReserved = 0
			}
			return err
		}
		chunkBuilder.Reset()
		if chunkReserved > 0 {
			mee.processor.releaseMemory(chunkReserved)
			chunkReserved = 0
		}
		return nil
	}

	defer func() {
		if flushErr := flushChunk(); err == nil {
			err = flushErr
		}
		if chunkReserved > 0 {
			mee.processor.releaseMemory(chunkReserved)
			chunkReserved = 0
		}
		if flushErr := bufWriter.Flush(); err == nil {
			err = flushErr
		} else {
			// Ensure buffer is flushed even if error is already set
			bufWriter.Flush()
		}
	}()

	totalPages := reader.NumPage()
	for pageNum := 1; pageNum <= totalPages; pageNum++ {
		select {
		case <-mee.processor.ctx.Done():
			return mee.processor.ctx.Err()
		default:
		}

		page := reader.Page(pageNum)
		content := page.Content()
		lines := groupTextsByLines(content.Text)

		for _, line := range lines {
			select {
			case <-mee.processor.ctx.Done():
				return mee.processor.ctx.Err()
			default:
			}

			lineText := buildLineText(line)
			if lineText == "" {
				continue
			}
			chunkBuilder.WriteString(lineText)
			chunkBuilder.WriteByte('\n')

			if err := growReservation(); err != nil {
				return err
			}

			if chunkThreshold > 0 && chunkBuilder.Len() >= chunkThreshold {
				if err := flushChunk(); err != nil {
					return err
				}
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

	// P0优化: 使用strings.Builder代替字符串拼接
	builder := GetBuilder()
	defer PutBuilder(builder)

	// 预估容量
	totalLen := 0
	for _, t := range sortedTexts {
		totalLen += len(t.S)
	}
	builder.Grow(totalLen + len(sortedTexts))

	var lastX float64
	for i, t := range sortedTexts {
		if i > 0 {
			// Add space if there's a significant gap
			gap := t.X - lastX
			if gap > 5.0 { // Threshold for adding space
				builder.WriteByte(' ')
			}
		}
		builder.WriteString(t.S)
		lastX = t.X + t.W
	}

	return builder.String()
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

func estimateTextMemory(text Text) int64 {
	return int64(len(text.S)) + 128
}

func estimateBlockMemory(block *TextBlock) int64 {
	if block == nil {
		return 128
	}
	size := int64(128)
	for _, t := range block.Texts {
		size += int64(len(t.S)) + 64
	}
	return size
}

func estimatePageMemory(textCount int) int64 {
	if textCount <= 0 {
		return 64
	}
	return int64(textCount)*128 + 64
}
