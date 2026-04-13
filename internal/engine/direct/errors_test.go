package direct

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyPDFError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{"nil error", nil, ""},
		{"pdf too large", errors.New("pdf_too_large: document exceeds limit"), "page range"},
		{"pdf too large phrase", errors.New("pdf too large for processing"), "page range"},
		{"password protected", errors.New("password protected pdf cannot be read"), "password"},
		{"invalid pdf", errors.New("invalid_pdf: could not parse"), "Invalid or corrupted"},
		{"could not process", errors.New("could not process pdf file"), "Invalid or corrupted"},
		{"too many pages", errors.New("too_many_pages in document"), "smaller page range"},
		{"unrelated error", errors.New("connection timeout"), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyPDFError(tt.err)
			if tt.contains == "" {
				assert.Empty(t, result)
			} else {
				assert.Contains(t, result, tt.contains)
			}
		})
	}
}

func TestClassifyMultiImageError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{"nil error", nil, ""},
		{"multi image dimension", errors.New("image dimensions exceed 2000px limit for multiple images"), "single image or resize"},
		{"too many images", errors.New("max_images exceeded"), "Reduce the number"},
		{"unrelated", errors.New("rate limit"), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyMultiImageError(tt.err)
			if tt.contains == "" {
				assert.Empty(t, result)
			} else {
				assert.Contains(t, result, tt.contains)
			}
		})
	}
}

func TestClassifyContentError(t *testing.T) {
	// PDF errors should be caught first.
	result := classifyContentError(errors.New("pdf_too_large"))
	assert.Contains(t, result, "page range")

	// Multi-image errors should be caught.
	result = classifyContentError(errors.New("max_images exceeded"))
	assert.Contains(t, result, "Reduce")

	// Non-content errors return empty.
	result = classifyContentError(errors.New("network timeout"))
	assert.Empty(t, result)
}
