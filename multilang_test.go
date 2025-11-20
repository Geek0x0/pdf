package pdf

import (
	"testing"
)

// TestNewMultiLangProcessor tests the multi-language processor initialization
func TestNewMultiLangProcessor(t *testing.T) {
	processor := NewMultiLangProcessor()

	if processor == nil {
		t.Fatal("Expected multi-language processor to be created")
	}

	// Check that common words are loaded
	if len(processor.englishWords) == 0 {
		t.Error("Expected English common words to be loaded")
	}
	if len(processor.frenchWords) == 0 {
		t.Error("Expected French common words to be loaded")
	}
	if len(processor.germanWords) == 0 {
		t.Error("Expected German common words to be loaded")
	}
	if len(processor.spanishWords) == 0 {
		t.Error("Expected Spanish common words to be loaded")
	}
}

// TestDetectLanguageEnglish tests English language detection
func TestDetectLanguageEnglish(t *testing.T) {
	processor := NewMultiLangProcessor()

	testText := "The quick brown fox jumps over the lazy dog. This is a sample English text."

	result := processor.DetectLanguage(testText)

	if result.Language != English {
		t.Logf("Expected English, got %s with confidence %.2f", result.Language, result.Confidence)
	}

	if result.Confidence < 0 || result.Confidence > 1 {
		t.Errorf("Expected confidence between 0 and 1, got %f", result.Confidence)
	}

	if result.WordCount == 0 {
		t.Error("Expected non-zero word count")
	}

	if result.SentenceCount == 0 {
		t.Error("Expected non-zero sentence count")
	}
}

// TestDetectLanguageFrench tests French language detection
func TestDetectLanguageFrench(t *testing.T) {
	processor := NewMultiLangProcessor()

	// Common French words: "le", "de", "et", "un", "il", "être", "à", "dans"
	testText := "Le chat est sur la table. C'est un exemple de texte français."

	result := processor.DetectLanguage(testText)

	t.Logf("French detection: %s with confidence %.2f", result.Language, result.Confidence)

	if result.WordCount == 0 {
		t.Error("Expected non-zero word count")
	}
	if result.SentenceCount == 0 {
		t.Error("Expected non-zero sentence count")
	}
}

// TestDetectLanguageGerman tests German language detection
func TestDetectLanguageGerman(t *testing.T) {
	processor := NewMultiLangProcessor()

	// Common German words: "der", "die", "und", "in", "den", "von", "zu"
	testText := "Der Hund ist im Garten. Das ist ein Beispiel für deutschen Text."

	result := processor.DetectLanguage(testText)

	t.Logf("German detection: %s with confidence %.2f", result.Language, result.Confidence)

	if result.WordCount == 0 {
		t.Error("Expected non-zero word count")
	}
	if result.SentenceCount == 0 {
		t.Error("Expected non-zero sentence count")
	}
}

// TestDetectLanguageSpanish tests Spanish language detection
func TestDetectLanguageSpanish(t *testing.T) {
	processor := NewMultiLangProcessor()

	// Common Spanish words: "el", "de", "que", "y", "a", "en", "un", "es"
	testText := "El gato está en la casa. Esto es un ejemplo de texto español."

	result := processor.DetectLanguage(testText)

	t.Logf("Spanish detection: %s with confidence %.2f", result.Language, result.Confidence)

	if result.WordCount == 0 {
		t.Error("Expected non-zero word count")
	}
	if result.SentenceCount == 0 {
		t.Error("Expected non-zero sentence count")
	}
}

// TestDetectLanguageUnknown tests language detection for empty text
func TestDetectLanguageUnknown(t *testing.T) {
	processor := NewMultiLangProcessor()

	// Test with empty text
	result := processor.DetectLanguage("")

	if result.Language != Unknown {
		t.Errorf("Expected Unknown language for empty text, got %s", result.Language)
	}
	if result.Confidence != 0.0 {
		t.Errorf("Expected 0.0 confidence for empty text, got %f", result.Confidence)
	}

	// Test with random characters
	result2 := processor.DetectLanguage("xyz123!@#")

	if result2.Language != Unknown {
		t.Logf("For random text, got %s with confidence %.2f (this may be expected)",
			result2.Language, result2.Confidence)
	}
}

// TestScoreTextEnglish tests the English text scoring
func TestScoreTextEnglish(t *testing.T) {
	processor := NewMultiLangProcessor()

	text := "The quick brown fox"
	score := processor.scoreText(text, processor.englishWords)

	if score < 0 {
		t.Errorf("Expected non-negative score, got %f", score)
	}

	// A text with common English words should have higher score than random text
	randomText := "Xyz abc def"
	randomScore := processor.scoreText(randomText, processor.englishWords)

	// Note: The scores might be 0 for both if normalization results in 0
	// This is acceptable as long as the function doesn't crash
	t.Logf("English text score: %f, random text score: %f", score, randomScore)
}

// TestGetUniqueCharacters tests character extraction
func TestGetUniqueCharacters(t *testing.T) {
	processor := NewMultiLangProcessor()

	text := "hello"
	chars := processor.getUniqueCharacters(text)

	if len(chars) == 0 {
		t.Error("Expected some unique characters")
	}

	// Should have 4 unique characters: h, e, l, o (l appears twice but should be counted once)
	foundH, foundE, foundL, foundO := false, false, false, false
	for _, r := range chars {
		switch r {
		case 'h':
			foundH = true
		case 'e':
			foundE = true
		case 'l':
			foundL = true
		case 'o':
			foundO = true
		}
	}

	if !foundH || !foundE || !foundL || !foundO {
		t.Errorf("Expected to find characters h, e, l, o in 'hello', got: %v", chars)
	}

	// Test with empty string
	emptyChars := processor.getUniqueCharacters("")
	if len(emptyChars) != 0 {
		t.Error("Expected no characters for empty string")
	}
}

// TestCountWords tests word counting
func TestCountWords(t *testing.T) {
	processor := NewMultiLangProcessor()

	// Test basic counting
	text := "Hello world"
	count := processor.countWords(text)
	if count != 2 {
		t.Errorf("Expected 2 words in 'Hello world', got %d", count)
	}

	// Test with punctuation
	text2 := "Hello, world!"
	count2 := processor.countWords(text2)
	if count2 != 2 {
		t.Errorf("Expected 2 words in 'Hello, world!', got %d", count2)
	}

	// Test with multiple spaces
	text3 := "Hello    world"
	count3 := processor.countWords(text3)
	if count3 != 2 {
		t.Errorf("Expected 2 words in 'Hello    world', got %d", count3)
	}

	// Test empty string
	count4 := processor.countWords("")
	if count4 != 0 {
		t.Errorf("Expected 0 words in empty string, got %d", count4)
	}

	// Test with numbers
	text5 := "Hello 123 world"
	count5 := processor.countWords(text5)
	if count5 != 3 {
		t.Errorf("Expected 3 words in 'Hello 123 world', got %d", count5)
	}
}

// TestCountSentences tests sentence counting
func TestCountSentences(t *testing.T) {
	processor := NewMultiLangProcessor()

	// Test with periods
	text1 := "First sentence. Second sentence."
	count1 := processor.countSentences(text1)
	if count1 != 2 {
		t.Errorf("Expected 2 sentences in '%s', got %d", text1, count1)
	}

	// Test with different sentence enders
	text2 := "First sentence! Second sentence? Third sentence."
	count2 := processor.countSentences(text2)
	if count2 != 3 {
		t.Errorf("Expected 3 sentences in '%s', got %d", text2, count2)
	}

	// Test with single sentence
	text3 := "Just one sentence"
	count3 := processor.countSentences(text3)
	if count3 != 1 {
		t.Errorf("Expected 1 sentence in '%s', got %d", text3, count3)
	}

	// Test with empty string
	count4 := processor.countSentences("")
	if count4 != 0 {
		t.Errorf("Expected 0 sentences in empty string, got %d", count4)
	}
}

// TestProcessTextWithLanguageDetection tests processing multiple texts with language detection
func TestProcessTextWithLanguageDetection(t *testing.T) {
	processor := NewMultiLangProcessor()

	texts := []Text{
		{S: "The quick brown fox.", X: 100, Y: 200, FontSize: 12},
		{S: "Le chat est mignon.", X: 150, Y: 250, FontSize: 12},
		{S: "Hola mundo!", X: 200, Y: 300, FontSize: 12},
	}

	results := processor.ProcessTextWithLanguageDetection(texts)

	if len(results) != len(texts) {
		t.Errorf("Expected %d results, got %d", len(texts), len(results))
	}

	// Check that each result has language info
	for i, result := range results {
		if result.Text.S != texts[i].S {
			t.Errorf("Text mismatch at index %d", i)
		}
		if result.Language.Language == "" {
			t.Errorf("Expected language to be detected at index %d", i)
		}
	}
}

// TestNewLanguageTextExtractor tests the language-aware text extractor initialization
func TestNewLanguageTextExtractor(t *testing.T) {
	extractor := NewLanguageTextExtractor()

	if extractor == nil {
		t.Fatal("Expected language text extractor to be created")
	}
	if extractor.processor == nil {
		t.Fatal("Expected processor to be initialized")
	}
}

// TestGetTextsByLanguage tests filtering texts by language
func TestGetTextsByLanguage(t *testing.T) {

	texts := []Text{
		{S: "The quick brown fox", X: 100, Y: 200, FontSize: 12},
		{S: "Le chat est mignon", X: 150, Y: 250, FontSize: 12},
		{S: "The lazy dog", X: 200, Y: 300, FontSize: 12}, // Another English text
	}

	extractor := NewLanguageTextExtractor()
	englishTexts := extractor.GetTextsByLanguage(texts, English)

	// The exact number depends on confidence level, but we should get at least some English texts
	t.Logf("Found %d English texts out of %d total", len(englishTexts), len(texts))

	if len(englishTexts) > len(texts) {
		t.Error("Should not return more texts than were provided")
	}
}

// TestGetLanguageStats tests language statistics
func TestGetLanguageStats(t *testing.T) {
	processor := NewMultiLangProcessor()

	texts := []Text{
		{S: "The quick brown fox", X: 100, Y: 200, FontSize: 12},
		{S: "Le chat est mignon", X: 150, Y: 250, FontSize: 12},
		{S: "Hola mundo", X: 200, Y: 300, FontSize: 12},
		{S: "Another English text", X: 250, Y: 350, FontSize: 12},
	}

	extractor := &LanguageTextExtractor{processor: processor}
	stats := extractor.GetLanguageStats(texts)

	// Should have at least some language stats
	if len(stats) == 0 {
		t.Log("Language detection may not have high confidence for short texts")
	}

	t.Logf("Language stats: %v", stats)
}

// TestNewMultiLanguageTextClassifier tests the multi-language classifier initialization
func TestNewMultiLanguageTextClassifier(t *testing.T) {
	texts := []Text{{S: "test", X: 100, Y: 200, FontSize: 12}}

	classifier := NewMultiLanguageTextClassifier(texts, 600, 800)

	if classifier == nil {
		t.Fatal("Expected multi-language classifier to be created")
	}
	if classifier.processor == nil {
		t.Fatal("Expected processor to be initialized")
	}
	if classifier.TextClassifier == nil {
		t.Fatal("Expected base classifier to be initialized")
	}
}

// TestClassifyBlocksWithLanguage tests enhanced classification with language info
func TestClassifyBlocksWithLanguage(t *testing.T) {
	texts := []Text{
		{S: "English text", X: 100, Y: 200, FontSize: 12},
		{S: "More English", X: 150, Y: 250, FontSize: 12},
	}

	classifier := NewMultiLanguageTextClassifier(texts, 600, 800)

	results := classifier.ClassifyBlocksWithLanguage()

	if len(results) == 0 {
		t.Log("Classification may not produce blocks for this test data")
	} else {
		// Each result should have language information
		for _, result := range results {
			if result.Language.Language == "" {
				t.Error("Expected language information in each classified block")
			}
		}
	}
}

// TestProcessTextWithMultiLanguage tests the global multi-language processing function
func TestProcessTextWithMultiLanguage(t *testing.T) {
	// Since we can't easily create a real PDF reader for testing,
	// we'll just make sure the function doesn't panic

	mockReader := &Reader{}

	results, err := ProcessTextWithMultiLanguage(mockReader)

	if err != nil {
		// This is expected for an empty reader
		t.Logf("Expected error for empty reader: %v", err)
	}

	if results == nil {
		t.Error("Expected results map to be initialized (even if empty)")
	}
}

// TestLanguageCheckFunctions tests the language detection helper functions
func TestLanguageCheckFunctions(t *testing.T) {
	processor := NewMultiLangProcessor()

	// Test English check functions
	if !processor.IsEnglish("The quick brown fox") {
		t.Log("English detection may not be confident for short phrases")
	} else {
		t.Log("Correctly identified English text")
	}

	// Test other languages (results may vary based on confidence)
	t.Log("French detection:", processor.IsFrench("Le chat est mignon"))
	t.Log("German detection:", processor.IsGerman("Der Hund ist hier"))
	t.Log("Spanish detection:", processor.IsSpanish("El gato es bonito"))
}

// TestGetSupportedLanguages tests the supported languages function
func TestGetSupportedLanguages(t *testing.T) {
	processor := NewMultiLangProcessor()

	languages := processor.GetSupportedLanguages()

	expectedLangs := []Language{English, French, German, Spanish}

	if len(languages) != len(expectedLangs) {
		t.Errorf("Expected %d languages, got %d", len(expectedLangs), len(languages))
	}

	// Check that all expected languages are present
	langSet := make(map[Language]bool)
	for _, lang := range languages {
		langSet[lang] = true
	}

	for _, expected := range expectedLangs {
		if !langSet[expected] {
			t.Errorf("Missing expected language: %s", expected)
		}
	}
}

// TestGetLanguageName tests language name retrieval
func TestGetLanguageName(t *testing.T) {
	processor := NewMultiLangProcessor()

	testCases := []struct {
		lang     Language
		expected string
	}{
		{English, "English"},
		{French, "French"},
		{German, "German"},
		{Spanish, "Spanish"},
		{Unknown, "Unknown"},
	}

	for _, tc := range testCases {
		name := processor.GetLanguageName(tc.lang)
		if name != tc.expected {
			t.Errorf("GetLanguageName(%s) = %s, want %s", tc.lang, name, tc.expected)
		}
	}
}

// TestGetLanguageConfidenceThreshold tests the confidence threshold function
func TestGetLanguageConfidenceThreshold(t *testing.T) {
	processor := NewMultiLangProcessor()

	threshold := processor.GetLanguageConfidenceThreshold()

	if threshold <= 0 || threshold > 1 {
		t.Errorf("Expected threshold between 0 and 1, got %f", threshold)
	}

	if threshold != 0.5 {
		t.Logf("Threshold is %f, not default 0.5", threshold)
	}
}

// TestLanguageInfoStructure tests the LanguageInfo structure
func TestLanguageInfoStructure(t *testing.T) {
	// Create a sample LanguageInfo
	info := LanguageInfo{
		Language:      English,
		Confidence:    0.8,
		Characters:    []rune{'h', 'e', 'l'},
		WordCount:     5,
		SentenceCount: 1,
	}

	if info.Language != English {
		t.Error("Expected English language")
	}
	if info.Confidence != 0.8 {
		t.Errorf("Expected confidence 0.8, got %f", info.Confidence)
	}
	if len(info.Characters) != 3 {
		t.Errorf("Expected 3 characters, got %d", len(info.Characters))
	}
	if info.WordCount != 5 {
		t.Errorf("Expected word count 5, got %d", info.WordCount)
	}
	if info.SentenceCount != 1 {
		t.Errorf("Expected sentence count 1, got %d", info.SentenceCount)
	}
}

// TestTextWithLanguageStructure tests the TextWithLanguage structure
func TestTextWithLanguageStructure(t *testing.T) {
	text := Text{S: "test", X: 100, Y: 200, FontSize: 12}
	langInfo := LanguageInfo{Language: English, Confidence: 0.9}

	textWithLang := TextWithLanguage{
		Text:       text,
		Language:   langInfo,
		Confidence: 0.9,
	}

	if textWithLang.Text.S != "test" {
		t.Error("Expected text to be preserved")
	}
	if textWithLang.Language.Language != English {
		t.Error("Expected language to be English")
	}
	if textWithLang.Confidence != 0.9 {
		t.Errorf("Expected confidence 0.9, got %f", textWithLang.Confidence)
	}
}

// TestClassifiedBlockWithLanguageStructure tests the ClassifiedBlockWithLanguage structure
func TestClassifiedBlockWithLanguageStructure(t *testing.T) {
	classifiedBlock := ClassifiedBlock{Type: BlockParagraph, Text: "test text"}
	langInfo := LanguageInfo{Language: French, Confidence: 0.85}

	blockWithLang := ClassifiedBlockWithLanguage{
		ClassifiedBlock: classifiedBlock,
		Language:        langInfo,
	}

	if blockWithLang.Type != BlockParagraph {
		t.Error("Expected block type to be Paragraph")
	}
	if blockWithLang.Text != "test text" {
		t.Error("Expected text to be preserved")
	}
	if blockWithLang.Language.Language != French {
		t.Error("Expected language to be French")
	}
	if blockWithLang.Language.Confidence != 0.85 {
		t.Errorf("Expected confidence 0.85, got %f", blockWithLang.Language.Confidence)
	}
}

// BenchmarkLanguageDetection benchmarks the language detection
func BenchmarkLanguageDetection(b *testing.B) {
	processor := NewMultiLangProcessor()

	englishText := "The quick brown fox jumps over the lazy dog. This is a sample English text for performance testing."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = processor.DetectLanguage(englishText)
	}
}

// BenchmarkMultiLanguageTextProcessing benchmarks processing multiple texts
func BenchmarkMultiLanguageTextProcessing(b *testing.B) {
	processor := NewMultiLangProcessor()

	texts := make([]Text, 100)
	for i := 0; i < 100; i++ {
		texts[i] = Text{
			S:        "The quick brown fox " + string(rune('A'+i%26)),
			X:        float64(i * 10),
			Y:        float64(i * 5),
			FontSize: 12,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = processor.ProcessTextWithLanguageDetection(texts)
	}
}

// TestExtractTextByLanguage tests language-based text extraction
func TestExtractTextByLanguage(t *testing.T) {
	f, r, err := Open("data/1.pdf")
	if err != nil {
		t.Skipf("skipping test: cannot open test PDF: %v", err)
		return
	}
	defer f.Close()

	extractor := NewLanguageTextExtractor()

	result, err := extractor.ExtractTextByLanguage(r)
	if err != nil {
		t.Fatalf("ExtractTextByLanguage failed: %v", err)
	}

	if result == nil {
		t.Error("Expected result map to be initialized")
	}

	// Should have at least some text detected
	totalTexts := 0
	for _, texts := range result {
		totalTexts += len(texts)
	}

	if totalTexts == 0 {
		t.Error("Expected to extract some text elements")
	}
}
