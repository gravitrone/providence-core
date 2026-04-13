package direct

import "strings"

// classifyPDFError checks if an API error is PDF-related and returns a
// user-friendly message with actionable hints. Returns empty string if
// not a PDF error.
func classifyPDFError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())

	if strings.Contains(msg, "pdf_too_large") || strings.Contains(msg, "pdf too large") ||
		strings.Contains(msg, "pdf exceeds") || strings.Contains(msg, "document_too_large") {
		return "PDF too large - try specifying page range with pages param (e.g. pages: \"1-10\")."
	}
	if strings.Contains(msg, "password") && strings.Contains(msg, "pdf") {
		return "PDF is password protected - remove the password and retry."
	}
	if strings.Contains(msg, "invalid_pdf") || strings.Contains(msg, "invalid pdf") ||
		strings.Contains(msg, "could not process pdf") || strings.Contains(msg, "pdf_parse") {
		return "Invalid or corrupted PDF - ensure the file is a valid PDF document."
	}
	if strings.Contains(msg, "too_many_pages") || strings.Contains(msg, "page_limit") {
		return "PDF has too many pages - try specifying a smaller page range with pages param."
	}

	return ""
}

// classifyMultiImageError checks if an API error relates to image dimension
// limits in multi-image requests (2000px limit) and returns a user-friendly
// message. Returns empty string if not a multi-image dimension error.
func classifyMultiImageError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())

	if (strings.Contains(msg, "2000") || strings.Contains(msg, "dimension")) &&
		strings.Contains(msg, "image") && strings.Contains(msg, "multiple") {
		return "Image too large for multi-image request (2000px limit). Try with a single image or resize."
	}
	if strings.Contains(msg, "max_images") || strings.Contains(msg, "too many images") {
		return "Too many images in request. Reduce the number of attached images."
	}

	return ""
}

// classifyContentError runs all content-type error classifiers (PDF, image,
// multi-image) and returns the first matching user-friendly message.
// Returns empty string if no classifier matches.
func classifyContentError(err error) string {
	if msg := classifyPDFError(err); msg != "" {
		return msg
	}
	if msg := classifyMultiImageError(err); msg != "" {
		return msg
	}
	return ""
}
