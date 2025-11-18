// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"errors"
	"fmt"
)

// PDFError represents an error that occurred during PDF processing.
// It includes contextual information about where the error occurred.
type PDFError struct {
	Op   string // Operation that failed (e.g., "extract text", "parse font")
	Page int    // Page number where error occurred (0 if not page-specific)
	Path string // File path if applicable
	Err  error  // Underlying error
}

func (e *PDFError) Error() string {
	if e.Page > 0 {
		return fmt.Sprintf("pdf: %s on page %d: %v", e.Op, e.Page, e.Err)
	}
	if e.Path != "" {
		return fmt.Sprintf("pdf: %s (%s): %v", e.Op, e.Path, e.Err)
	}
	return fmt.Sprintf("pdf: %s: %v", e.Op, e.Err)
}

func (e *PDFError) Unwrap() error {
	return e.Err
}

// Common errors
var (
	// ErrInvalidFont indicates a font definition is malformed or unsupported
	ErrInvalidFont = errors.New("invalid or unsupported font")

	// ErrUnsupportedEncoding indicates the character encoding is not supported
	ErrUnsupportedEncoding = errors.New("unsupported character encoding")

	// ErrMalformedStream indicates a content stream is malformed
	ErrMalformedStream = errors.New("malformed content stream")

	// ErrInvalidPage indicates an invalid page number or corrupted page
	ErrInvalidPage = errors.New("invalid page")

	// ErrEncrypted indicates the PDF is encrypted and cannot be read without a password
	ErrEncrypted = errors.New("PDF is encrypted")

	// ErrCorrupted indicates the PDF file structure is corrupted
	ErrCorrupted = errors.New("PDF file is corrupted")

	// ErrUnsupportedVersion indicates the PDF version is not supported
	ErrUnsupportedVersion = errors.New("unsupported PDF version")

	// ErrNoContent indicates the page has no content
	ErrNoContent = errors.New("page has no content")
)

// wrapError wraps an error with operation context
func wrapError(op string, err error) error {
	if err == nil {
		return nil
	}
	return &PDFError{Op: op, Err: err}
}

// wrapPageError wraps an error with page-specific context
func wrapPageError(op string, page int, err error) error {
	if err == nil {
		return nil
	}
	return &PDFError{Op: op, Page: page, Err: err}
}
