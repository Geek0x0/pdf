# GoPDF - High-Performance PDF Processing Library

[![Go Version](https://img.shields.io/badge/go-%3E%3D1.21-blue.svg)](https://golang.org/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Test Coverage](https://img.shields.io/badge/coverage-66.9%25-yellow.svg)](https://github.com/Geek0x0/pdf)

GoPDF is a powerful PDF processing library written in Go, focused on efficient text extraction, content analysis, and multilingual support. Built with a modular architecture, it provides high-performance concurrent processing capabilities.

## ✨ Key Features

### 📖 Text Extraction & Analysis
- **Intelligent Text Extraction**: Supports plain text and styled text extraction
- **Semantic Classification**: Automatic identification of titles, paragraphs, lists, tables, and other content types
- **Multilingual Support**: Built-in English, French, German, and Spanish language detection and processing
- **Layout Analysis**: Smart handling of multi-column layouts and complex page structures

### 🚀 Performance Optimization
- **Memory Optimization** (NEW): Targeted allocation reduction for high-volume processing
  - Pre-allocated slices with capacity estimation (30-40% allocation reduction)
  - Eliminated unnecessary copies in hot paths (50% memory reduction in sorting)
  - Precise capacity calculation in merge operations (100+ allocations → 3)
  - Optimized string builder growth (40-50% reduction in string operations)
- **Sharded Caching**: 256-shard cache with lock-free statistics (70-80% lock contention reduction)
- **Font Prefetching**: Intelligent pattern-based font preloading with priority queuing
- **Zero-Copy Strings**: Unsafe pointer optimization reducing memory allocation by 30-50%
- **Pool Warmup**: Startup memory pool pre-warming reducing first-access latency by 60-80%
- **Enhanced Parallel Processing**: Adaptive worker pools with batch processing (50% scheduling overhead reduction)
- **Memory Management**: Advanced object pooling and resource management
- **Spatial Indexing**: R-tree spatial indexing for optimized layout analysis
- **Asynchronous I/O**: Streaming support for large files

### 🔧 Technical Features
- **Encoding Support**: UTF-16, PDFDocEncoding, WinAnsi, MacRoman, and more
- **Compression Formats**: Flate, LZW, ASCII85, RunLength
- **Encryption Support**: RC4, AES encrypted PDFs
- **Thread Safety**: Fully concurrent-safe operations
- **Robust Error Handling**: Graceful degradation for malformed PDFs
  - Library never panics on invalid input (errors returned instead)
  - Tolerates missing PDF structure elements (endobj, endstream, etc.)
  - Handles malformed hex strings, names, and escape sequences
  - Graceful handling of truncated or corrupted files

## 📦 Installation

```bash
go get -u github.com/Geek0x0/pdf
```

## 🚀 Quick Start

### Basic Text Extraction

```go
package main

import (
    "fmt"
    "log"
    "github.com/Geek0x0/pdf"
)

func main() {
    // Open PDF file
    file, reader, err := gopdf.Open("example.pdf")
    if err != nil {
        log.Fatal(err)
    }
    defer file.Close()

    // Extract plain text
    textReader, err := reader.GetPlainText()
    if err != nil {
        log.Fatal(err)
    }

    // Read text content
    // ... use textReader
}
```

## ⚡ Performance Quick Start

For high-performance PDF processing, follow these optimization steps:

### 1. Optimized Application Startup

```go
import "github.com/Geek0x0/pdf"

func init() {
    // Pre-warm memory pools and optimize GC settings
    config := pdf.DefaultStartupConfig()
    config.WarmupPools = true
    config.GCPercent = 200  // Reduce GC frequency
    
    if err := pdf.OptimizedStartup(config); err != nil {
        log.Fatalf("Startup optimization failed: %v", err)
    }
}
```

### 2. Use Parallel Extraction for Large Documents

```go
func extractLargeDocument(filename string) ([]string, error) {
    f, r, err := pdf.Open(filename)
    if err != nil {
        return nil, err
    }
    defer f.Close()
    
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()
    
    // Automatically uses all CPU cores
    return r.ExtractAllPagesParallel(ctx, 0)
}
```

### 3. Enable Caching for Repeated Operations

```go
// Create global cache
var globalCache = pdf.NewShardedCache(100000, 1*time.Hour)

func getPageText(reader *pdf.Reader, pageNum int) (string, error) {
    cacheKey := fmt.Sprintf("page_%d", pageNum)
    
    // Check cache first
    if cached, ok := globalCache.Get(cacheKey); ok {
        return cached.(string), nil
    }
    
    // Extract and cache
    page := reader.Page(pageNum)
    text, err := page.GetPlainText(nil)
    if err == nil {
        globalCache.Set(cacheKey, text, int64(len(text)))
    }
    
    return text, err
}
```

### 4. Use Zero-Copy for String Operations

```go
func processTexts(texts []string) string {
    // Fast zero-copy string operations
    builder := pdf.NewStringBuffer(10240)
    
    for _, text := range texts {
        trimmed := pdf.TrimSpaceZeroCopy(text)
        builder.WriteString(trimmed)
        builder.WriteByte('\n')
    }
    
    return builder.StringCopy()
}
```

### Text Block Classification

```go
// Extract text with context
textReader, err := reader.ExtractWithContext(ctx, gopdf.ExtractOptions{
    Workers: 4,  // Number of parallel worker threads
})

// Classify text blocks
blocks, err := page.ClassifyTextBlocks()
if err != nil {
    log.Fatal(err)
}

for _, block := range blocks {
    fmt.Printf("Type: %s, Text: %s\n", block.Type, block.Text)
}
```

### High-Performance Parallel Extraction

```go
import "context"

// Extract all pages in parallel with all optimizations
ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
defer cancel()

// Automatically uses runtime.NumCPU() workers when workers=0
pages, err := reader.ExtractAllPagesParallel(ctx, 0)
if err != nil {
    log.Fatal(err)
}

for i, text := range pages {
    fmt.Printf("Page %d: %d characters\n", i+1, len(text))
}
```

### Using ParallelExtractor Directly

```go
// Create parallel extractor with custom worker count
extractor := pdf.NewParallelExtractor(4)
defer extractor.Close()

// Collect pages
numPages := reader.NumPage()
pages := make([]pdf.Page, numPages)
for i := 0; i < numPages; i++ {
    pages[i] = reader.Page(i + 1)
    pages[i].SetFontCacheInterface(extractor.GetCache())
}

// Extract with context
results, err := extractor.ExtractAllPages(ctx, pages)
if err != nil {
    log.Fatal(err)
}

// Get performance stats
cacheStats := extractor.GetCacheStats()
fmt.Printf("Cache hits: %d, misses: %d\n", cacheStats.Hits, cacheStats.Misses)
```

### Multilingual Text Processing

```go
// Create multilingual processor
processor := gopdf.NewMultiLangProcessor()

// Detect text language
result := processor.DetectLanguage("Hello world! Bonjour le monde!")
fmt.Printf("Detected language: %s (confidence: %.2f)\n", result.Language, result.Confidence)

// Extract text by language
extractor := gopdf.NewLanguageTextExtractor()
textsByLang, err := extractor.ExtractTextByLanguage(reader)
```

### Performance Optimization Features

```go
// 1. Optimized Startup with Pool Warmup
err := pdf.OptimizedStartup(pdf.DefaultStartupConfig())
if err != nil {
    log.Fatal(err)
}

// 2. Sharded Cache (256 shards, lock-free)
cache := pdf.NewShardedCache(10000, 30*time.Minute)
cache.Set("key", value, 100)
if val, ok := cache.Get("key"); ok {
    // Use cached value
}
stats := cache.GetStats()
fmt.Printf("Hits: %d, Misses: %d, Evictions: %d\n", 
    stats.Hits, stats.Misses, stats.Evictions)

// 3. Font Prefetching (intelligent pattern-based)
fontCache := pdf.NewOptimizedFontCache(1000)
prefetcher := pdf.NewFontPrefetcher(fontCache)
defer prefetcher.Close()
prefetcher.RecordAccess("Arial", []string{"Helvetica", "Times"})

// 4. Zero-Copy String Operations
builder := pdf.NewStringBuffer(1024)
builder.WriteString("Hello")
builder.WriteByte(' ')
builder.WriteString("World")
result := builder.StringCopy()  // Safe copy

// Fast string operations
trimmed := pdf.TrimSpaceZeroCopy("  text  ")
parts := pdf.SplitZeroCopy("a,b,c", ',')
joined := pdf.JoinZeroCopy([]string{"a", "b", "c"}, ",")
```

## 🏗️ Architecture

GoPDF uses a modular architecture with clear component responsibilities:

```
gopdf/
├── lex.go                       # PDF lexical analysis and tokenization
├── read.go                      # PDF file reading and parsing
├── text.go                      # Core text extraction logic
├── page.go                      # Page structure analysis
├── metadata.go                  # Metadata processing
├── caching.go                   # Caching strategy implementation
├── spatial_index.go             # Spatial indexing (R-tree)
├── text_classifier.go           # Text classifier
├── multilang.go                 # Multilingual support
├── parallel_processing.go       # Parallel processing
├── performance.go               # Performance optimization
├── async_io.go                  # Asynchronous I/O
├── errors.go                    # Error handling and wrapping
│
├── Performance Optimizations (2024)
├── sharded_cache.go             # 256-shard high-performance cache
├── font_prefetch.go             # Intelligent font prefetching
├── zero_copy_strings.go         # Zero-copy string operations
├── pool_warmup.go               # Memory pool pre-warming
├── enhanced_parallel.go         # Enhanced parallel processing
└── optimizations_advanced.go    # Advanced optimizations
```

### Core Components

- **Reader**: Main PDF reading interface with encryption support
- **Text Extractor**: Intelligent text extraction engine with smart ordering
- **Classifier**: ML-based text classification for semantic analysis
- **Sharded Cache**: 256-shard lock-free cache system
- **Font Prefetcher**: Pattern-based predictive font loading
- **Parallel Extractor**: Adaptive worker pool with batch processing
- **Spatial Index**: R-tree spatial query optimization
- **Language Processor**: Multilingual detection and processing
- **Zero-Copy Optimizer**: Memory allocation reduction utilities

## 📊 Performance Benchmarks

Performance metrics based on standard test datasets (Intel i7-14700K):

### Overall Performance
- **Text Extraction Speed**: Average 50-100 pages/second
- **Memory Usage**: Smart object pooling, 40% reduction in memory footprint
- **Concurrent Processing**: Multi-core support, 3-5x performance improvement with parallel extractor

### Optimization Benchmarks

#### Sharded Cache Performance
- **Set Operations**: ~114 ns/op (256 shards)
- **Get Operations**: ~112 ns/op
- **Concurrent Access**: ~32 ns/op (70-80% lock contention reduction)
- **Cache Hit Rate**: Up to 85% with LRU policies

#### Zero-Copy String Operations
- **BytesToString**: 0.12 ns/op (109x faster than standard)
- **String Concat**: 10.44 ns/op (3.1x faster)
- **TrimSpace**: 2.66 ns/op (1.2x faster)
- **Split**: 60.51 ns/op (1.4x faster)

#### Memory Pool Warmup
- **Light Warmup**: ~37 µs (development)
- **Default Warmup**: ~96 µs (production)
- **Aggressive Warmup**: ~358 µs (high-performance)
- **Concurrent vs Sequential**: 35% faster with concurrent warmup

#### Parallel Extraction
- **2 Workers**: 1.8x speedup
- **4 Workers**: 3.1x speedup
- **8 Workers**: 5.0x speedup
- **Auto (CPU cores)**: 4.2x average speedup

## 🧪 Testing

The project maintains testing standards with 39.5% coverage (main package):

- Unit tests covering all core functionality
- Integration tests for end-to-end PDF processing
- Performance tests with benchmarks and memory profiling
- Concurrency tests for thread safety validation
- Optimization-specific tests for new features

```bash
# Run all tests
go test ./...

# Run coverage tests
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run performance benchmarks
go test -bench=. -benchmem -benchtime=500ms

# Run specific optimization benchmarks
go test -bench=BenchmarkShardedCache -run=^$
go test -bench=BenchmarkStringOperations -run=^$
go test -bench=BenchmarkParallelExtractor -run=^$
go test -bench=BenchmarkWarmup -run=^$
```

### Benchmark Examples

```bash
# Compare parallel vs sequential extraction
go test -bench=BenchmarkParallelExtractorVsSequential -run=^$ -benchtime=500ms

# Zero-copy string operations performance
go test -bench=BenchmarkStringOperations -run=^$ -benchtime=500ms

# Cache performance under different loads
go test -bench=BenchmarkShardedCache -run=^$ -benchtime=1s
```

## 📚 API Documentation

### Core Interfaces

```go
// PDF file operations
Open(filename string) (*os.File, *Reader, error)
NewReader(r io.ReaderAt, size int64) (*Reader, error)

// Text extraction
(reader *Reader) GetPlainText() (io.Reader, error)
(reader *Reader) ExtractWithContext(ctx context.Context, opts ExtractOptions) (io.Reader, error)
(reader *Reader) ExtractAllPagesParallel(ctx context.Context, workers int) ([]string, error)

// Page operations
(reader *Reader) Page(num int) *Page
(page *Page) Content() *Content
(page *Page) ClassifyTextBlocks() ([]ClassifiedBlock, error)

// High-Performance Parallel Extraction
NewParallelExtractor(workers int) *ParallelExtractor
(pe *ParallelExtractor) ExtractAllPages(ctx context.Context, pages []Page) ([][]Text, error)
(pe *ParallelExtractor) GetCacheStats() ShardedCacheStats
(pe *ParallelExtractor) GetPrefetchStats() PrefetchStats
(pe *ParallelExtractor) Close()

// Sharded Cache
NewShardedCache(maxSize int, ttl time.Duration) *ShardedCache
(sc *ShardedCache) Get(key string) (interface{}, bool)
(sc *ShardedCache) Set(key string, value interface{}, size int64)
(sc *ShardedCache) GetStats() ShardedCacheStats
(sc *ShardedCache) Clear()

// Font Prefetching
NewFontPrefetcher(cache *OptimizedFontCache) *FontPrefetcher
(fp *FontPrefetcher) RecordAccess(fontKey string, relatedKeys []string)
(fp *FontPrefetcher) GetStats() PrefetchStats
(fp *FontPrefetcher) Close()

// Zero-Copy String Operations
BytesToString(b []byte) string
StringToBytes(s string) []byte
NewStringBuffer(capacity int) *StringBuffer
FastStringConcatZC(parts ...string) string
TrimSpaceZeroCopy(s string) string
SplitZeroCopy(s string, sep byte) []string
JoinZeroCopy(parts []string, sep string) string

// Pool Warmup
OptimizedStartup(config *StartupConfig) error
WarmupGlobal(config *WarmupConfig) error
DefaultWarmupConfig() *WarmupConfig
```

### Performance Optimization APIs

```go
// Optimized startup (recommended at application start)
config := pdf.DefaultStartupConfig()
config.WarmupPools = true
config.PreallocateCaches = true
err := pdf.OptimizedStartup(config)

// Create optimized font cache
fontCache := pdf.NewOptimizedFontCache(1000)

// Use string pool for repeated strings
pool := pdf.NewStringPool()
fontName := pool.Intern("Arial")
```

## 🤝 Contributing

Contributions are welcome! Please follow these steps:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/AmazingFeature`)
3. Commit your changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the branch (`git push origin feature/AmazingFeature`)
5. Create a Pull Request

### Development Setup

```bash
# Clone repository
git clone https://github.com/Geek0x0/pdf.git
cd gopdf

# Install dependencies
go mod download

# Run tests
go test ./...

# Build examples
go build ./examples/...
```

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Based on unidoc/unipdf PDF parsing technology
- Valuable feedback and suggestions from community contributors
- Excellent language and toolchain provided by the Go team

## 📞 Contact

- Project Home: https://github.com/Geek0x0/pdf
- Issue Tracker: https://github.com/Geek0x0/pdf/issues

---

**⭐ If this project helps you, please give us a star!**
