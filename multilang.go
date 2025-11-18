// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"strings"
	"unicode"
)

// Language represents a language code
type Language string

const (
	English Language = "en"
	French  Language = "fr"
	German  Language = "de"
	Spanish Language = "es"
	Unknown Language = "unknown"
)

// LanguageInfo contains information about a detected language
type LanguageInfo struct {
	Language      Language
	Confidence    float64 // Confidence level (0.0 to 1.0)
	Characters    []rune  // Unique characters in the text
	WordCount     int     // Number of words in the text
	SentenceCount int     // Number of sentences in the text
}

// MultiLangProcessor provides multi-language text processing
type MultiLangProcessor struct {
	// Common words for language detection
	englishWords []string
	frenchWords  []string
	germanWords  []string
	spanishWords []string
}

// NewMultiLangProcessor creates a new multi-language processor
func NewMultiLangProcessor() *MultiLangProcessor {
	return &MultiLangProcessor{
		englishWords: []string{"the", "be", "to", "of", "and", "a", "in", "that", "have", "i", "it", "for", "not", "on", "with", "he", "as", "you", "do", "at"},
		frenchWords:  []string{"le", "de", "et", "un", "il", "être", "et", "à", "dans", "ne", "la", "les", "son", "que", "pour", "se", "ce", "d'", "en", "du"},
		germanWords:  []string{"der", "die", "und", "in", "den", "von", "zu", "das", "mit", "sich", "des", "auf", "für", "ist", "im", "dem", "nicht", "ein", "eine", "als"},
		spanishWords: []string{"el", "de", "que", "y", "a", "en", "un", "es", "se", "no", "te", "lo", "le", "da", "su", "por", "son", "con", "para", "al"},
	}
}

// DetectLanguage detects the language of a given text
func (mlp *MultiLangProcessor) DetectLanguage(text string) LanguageInfo {
	if text == "" {
		return LanguageInfo{Language: Unknown, Confidence: 0.0}
	}

	englishScore := mlp.scoreText(text, mlp.englishWords)
	frenchScore := mlp.scoreText(text, mlp.frenchWords)
	germanScore := mlp.scoreText(text, mlp.germanWords)
	spanishScore := mlp.scoreText(text, mlp.spanishWords)

	maxScore := englishScore
	detectedLang := English

	if frenchScore > maxScore {
		maxScore = frenchScore
		detectedLang = French
	}
	if germanScore > maxScore {
		maxScore = germanScore
		detectedLang = German
	}
	if spanishScore > maxScore {
		maxScore = spanishScore
		detectedLang = Spanish
	}

	// Calculate confidence as a percentage of the highest score over total
	totalScore := englishScore + frenchScore + germanScore + spanishScore
	confidence := 0.0
	if totalScore > 0 {
		confidence = maxScore / totalScore
		// Normalize to 0-1 range, with minimum confidence of 0.25 for random chance
		confidence = 0.25 + (confidence * 0.75)
	}

	// Ensure confidence is between 0 and 1
	if confidence > 1.0 {
		confidence = 1.0
	}

	return LanguageInfo{
		Language:      detectedLang,
		Confidence:    confidence,
		Characters:    mlp.getUniqueCharacters(text),
		WordCount:     mlp.countWords(text),
		SentenceCount: mlp.countSentences(text),
	}
}

// scoreText calculates a score for how likely the text is in a particular language
func (mlp *MultiLangProcessor) scoreText(text string, commonWords []string) float64 {
	text = strings.ToLower(text)
	words := strings.Fields(text)

	score := 0.0
	for _, word := range words {
		// Remove punctuation from the word - build a new string with only letters and numbers
		var cleanWord string
		for _, r := range word {
			if unicode.IsLetter(r) || unicode.IsNumber(r) {
				cleanWord += string(r)
			}
		}

		if len(cleanWord) > 1 { // Only consider words with more than one character
			for _, commonWord := range commonWords {
				if cleanWord == commonWord {
					score++
					break
				}
			}
		}
	}

	// Normalize by text length to avoid bias towards longer texts
	if len(words) > 0 {
		score = score / float64(len(words))
	}

	return score
}

// getUniqueCharacters returns the unique characters in a text
func (mlp *MultiLangProcessor) getUniqueCharacters(text string) []rune {
	seen := make(map[rune]bool)
	var uniqueChars []rune

	for _, r := range text {
		if !seen[r] {
			seen[r] = true
			uniqueChars = append(uniqueChars, r)
		}
	}

	return uniqueChars
}

// countWords counts the number of words in a text
func (mlp *MultiLangProcessor) countWords(text string) int {
	words := strings.Fields(text)
	count := 0
	for _, word := range words {
		// Count as a word if it contains at least one letter or digit
		for _, r := range word {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				count++
				break
			}
		}
	}
	return count
}

// countSentences counts the number of sentences in a text
func (mlp *MultiLangProcessor) countSentences(text string) int {
	count := 0
	inSentence := false

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			inSentence = true
		} else if r == '.' || r == '!' || r == '?' {
			if inSentence {
				count++
				inSentence = false
			}
		} else if unicode.IsSpace(r) {
			// End of potential sentence if we were in one
			if inSentence && (r == '\n' || r == '\r') {
				inSentence = false
			}
		}
	}

	// If we ended with a sentence in progress, count it
	if inSentence {
		count++
	}

	return count
}

// ProcessTextWithLanguageDetection processes text with language detection
func (mlp *MultiLangProcessor) ProcessTextWithLanguageDetection(texts []Text) []TextWithLanguage {
	var result []TextWithLanguage

	for _, text := range texts {
		langInfo := mlp.DetectLanguage(text.S)
		result = append(result, TextWithLanguage{
			Text:       text,
			Language:   langInfo,
			Confidence: langInfo.Confidence,
		})
	}

	return result
}

// TextWithLanguage represents text with detected language information
type TextWithLanguage struct {
	Text       Text
	Language   LanguageInfo
	Confidence float64
}

// LanguageTextExtractor extracts text while detecting languages
type LanguageTextExtractor struct {
	processor *MultiLangProcessor
}

// NewLanguageTextExtractor creates a new language-aware text extractor
func NewLanguageTextExtractor() *LanguageTextExtractor {
	return &LanguageTextExtractor{
		processor: NewMultiLangProcessor(),
	}
}

// ExtractTextByLanguage extracts text grouped by detected language
func (lte *LanguageTextExtractor) ExtractTextByLanguage(reader *Reader) (map[Language][]Text, error) {
	totalPages := reader.NumPage()
	result := make(map[Language][]Text)

	for pageNum := 1; pageNum <= totalPages; pageNum++ {
		page := reader.Page(pageNum)
		content := page.Content()

		// Process each text element to detect language
		for _, text := range content.Text {
			langInfo := lte.processor.DetectLanguage(text.S)
			result[langInfo.Language] = append(result[langInfo.Language], text)
		}
	}

	return result, nil
}

// GetTextsByLanguage returns text elements filtered by specific language
func (lte *LanguageTextExtractor) GetTextsByLanguage(texts []Text, targetLang Language) []Text {
	var filtered []Text

	for _, text := range texts {
		langInfo := lte.processor.DetectLanguage(text.S)
		if langInfo.Language == targetLang && langInfo.Confidence > 0.5 { // Only high confidence matches
			filtered = append(filtered, text)
		}
	}

	return filtered
}

// GetLanguageStats returns statistics about languages detected in the text
func (lte *LanguageTextExtractor) GetLanguageStats(texts []Text) map[Language]int {
	stats := make(map[Language]int)

	for _, text := range texts {
		langInfo := lte.processor.DetectLanguage(text.S)
		if langInfo.Confidence > 0.5 { // Only count high-confidence detections
			stats[langInfo.Language]++
		}
	}

	return stats
}

// MultiLanguageTextClassifier extends the text classifier with language awareness
type MultiLanguageTextClassifier struct {
	*TextClassifier
	processor *MultiLangProcessor
}

// NewMultiLanguageTextClassifier creates a new multi-language text classifier
func NewMultiLanguageTextClassifier(texts []Text, pageWidth, pageHeight float64) *MultiLanguageTextClassifier {
	// First classify with the base classifier
	baseClassifier := NewTextClassifier(texts, pageWidth, pageHeight)

	return &MultiLanguageTextClassifier{
		TextClassifier: baseClassifier,
		processor:      NewMultiLangProcessor(),
	}
}

// ClassifyBlocksWithLanguage extends the classification with language information
func (mltc *MultiLanguageTextClassifier) ClassifyBlocksWithLanguage() []ClassifiedBlockWithLanguage {
	baseBlocks := mltc.ClassifyBlocks()
	var result []ClassifiedBlockWithLanguage

	for _, baseBlock := range baseBlocks {
		// Detect language for the block text
		langInfo := mltc.processor.DetectLanguage(baseBlock.Text)

		result = append(result, ClassifiedBlockWithLanguage{
			ClassifiedBlock: baseBlock,
			Language:        langInfo,
		})
	}

	return result
}

// ClassifiedBlockWithLanguage represents a classified block with language information
type ClassifiedBlockWithLanguage struct {
	ClassifiedBlock
	Language LanguageInfo
}

// ProcessTextWithMultiLanguage handles multi-language text processing for the entire PDF
func ProcessTextWithMultiLanguage(reader *Reader) (map[Language][]ClassifiedBlock, error) {
	totalPages := reader.NumPage()
	result := make(map[Language][]ClassifiedBlock)

	for pageNum := 1; pageNum <= totalPages; pageNum++ {
		page := reader.Page(pageNum)
		blocks, err := page.ClassifyTextBlocks()
		if err != nil {
			return nil, err
		}

		// Process each block to detect language
		processor := NewMultiLangProcessor()
		for _, block := range blocks {
			langInfo := processor.DetectLanguage(block.Text)
			result[langInfo.Language] = append(result[langInfo.Language], block)
		}
	}

	return result, nil
}

// IsEnglish checks if text is likely English
func (mlp *MultiLangProcessor) IsEnglish(text string) bool {
	langInfo := mlp.DetectLanguage(text)
	return langInfo.Language == English && langInfo.Confidence > 0.6
}

// IsFrench checks if text is likely French
func (mlp *MultiLangProcessor) IsFrench(text string) bool {
	langInfo := mlp.DetectLanguage(text)
	return langInfo.Language == French && langInfo.Confidence > 0.6
}

// IsGerman checks if text is likely German
func (mlp *MultiLangProcessor) IsGerman(text string) bool {
	langInfo := mlp.DetectLanguage(text)
	return langInfo.Language == German && langInfo.Confidence > 0.6
}

// IsSpanish checks if text is likely Spanish
func (mlp *MultiLangProcessor) IsSpanish(text string) bool {
	langInfo := mlp.DetectLanguage(text)
	return langInfo.Language == Spanish && langInfo.Confidence > 0.6
}

// GetSupportedLanguages returns the list of supported languages
func (mlp *MultiLangProcessor) GetSupportedLanguages() []Language {
	return []Language{English, French, German, Spanish}
}

// GetLanguageName returns the full name of a language
func (mlp *MultiLangProcessor) GetLanguageName(lang Language) string {
	switch lang {
	case English:
		return "English"
	case French:
		return "French"
	case German:
		return "German"
	case Spanish:
		return "Spanish"
	default:
		return "Unknown"
	}
}

// GetLanguageConfidenceThreshold returns a confidence threshold for reliable detection
func (mlp *MultiLangProcessor) GetLanguageConfidenceThreshold() float64 {
	return 0.5
}
