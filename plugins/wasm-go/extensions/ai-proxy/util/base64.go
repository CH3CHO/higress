package util

import (
	"strings"
)

// Base64ImageInfo holds the parsed components of a base64 encoded image data URL
type Base64ImageInfo struct {
	Data     string // base64 encoded data (without the data URL prefix)
	MimeType string // e.g., "image/jpeg", "image/png"
	Format   string // e.g., "jpeg", "png"
}

// ParseBase64Image parses a base64 encoded image data URL and returns its components.
// Format: data:[<media-type>][;base64],<data>
//
// This function corresponds to _parse_base64_image in Python's litellm:
// litellm/litellm_core_utils/prompt_templates/factory.py
//
// The function requires ";base64" to be present in the header to extract MIME type.
// If ";base64" is not found, default values (image/jpeg) are used.
//
// Returns nil if the input is not a valid data URL.
func ParseBase64Image(dataURL string) *Base64ImageInfo {
	// Check for data: prefix
	if len(dataURL) < 5 || dataURL[:5] != "data:" {
		return nil
	}

	// Find the comma separator
	commaIdx := strings.Index(dataURL, ",")
	if commaIdx == -1 {
		return nil
	}

	// Extract header and data
	// header is the part between "data:" and ","
	header := dataURL[5:commaIdx]
	data := dataURL[commaIdx+1:]

	// Default values (same as Python's litellm)
	mimeType := "image/jpeg"
	format := "jpeg"

	// Check for ;base64 marker (matching Python's regex: r"data:(.*?);base64")
	// Only extract MIME type if ;base64 is present
	base64Idx := strings.Index(header, ";base64")
	if base64Idx != -1 {
		// Extract MIME type from the part before ;base64
		mediaType := header[:base64Idx]
		if mediaType != "" {
			mimeType = mediaType
			// Extract format from MIME type (e.g., "image/jpeg" -> "jpeg")
			if slashIdx := strings.LastIndex(mediaType, "/"); slashIdx != -1 {
				format = mediaType[slashIdx+1:]
			}
		}
	}

	return &Base64ImageInfo{
		Data:     data,
		MimeType: mimeType,
		Format:   format,
	}
}
