# GoPDF - PDF Processing Library for Go

A Go library for reading and extracting content from PDF files.

## Features

- ✅ PDF file reading and parsing
- ✅ Text extraction (plain text and styled text)
- ✅ Smart text ordering for multi-column layouts
- ✅ Semantic text classification (titles, paragraphs, lists, etc.)
- ✅ Metadata extraction (title, author, dates, keywords)
- ✅ Page structure analysis (rows, columns)
- ✅ **Performance optimization** (object pooling, lazy loading, streaming)
- ✅ Character encoding support (UTF-16, PDFDocEncoding, WinAnsi, MacRoman, etc.)
- ✅ Compression format support (Flate, LZW, ASCII85, RunLength)
- ✅ Encrypted PDF support (RC4, AES)
- ✅ Document outline extraction
- ✅ Concurrent text extraction with context support
- ✅ Font caching for improved performance
- ✅ Unified extraction interface with Builder pattern

## Installation

```bash
go get -u github.com/yourusername/gopdf
```