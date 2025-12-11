package util

import (
	"fmt"
	"strings"
)

// FileType represents the file type enum
type FileType string

// Base Enums/Consts
const (
	FileTypeAAC            FileType = "AAC"
	FileTypeCSV            FileType = "CSV"
	FileTypeDOC            FileType = "DOC"
	FileTypeDOCX           FileType = "DOCX"
	FileTypeFLAC           FileType = "FLAC"
	FileTypeFLV            FileType = "FLV"
	FileTypeGIF            FileType = "GIF"
	FileTypeGoogleDoc      FileType = "GOOGLE_DOC"
	FileTypeGoogleDrawings FileType = "GOOGLE_DRAWINGS"
	FileTypeGoogleSheets   FileType = "GOOGLE_SHEETS"
	FileTypeGoogleSlides   FileType = "GOOGLE_SLIDES"
	FileTypeHEIC           FileType = "HEIC"
	FileTypeHEIF           FileType = "HEIF"
	FileTypeHTML           FileType = "HTML"
	FileTypeJPEG           FileType = "JPEG"
	FileTypeJSON           FileType = "JSON"
	FileTypeM4A            FileType = "M4A"
	FileTypeM4V            FileType = "M4V"
	FileTypeMOV            FileType = "MOV"
	FileTypeMP3            FileType = "MP3"
	FileTypeMP4            FileType = "MP4"
	FileTypeMPEG           FileType = "MPEG"
	FileTypeMPEGPS         FileType = "MPEGPS"
	FileTypeMPG            FileType = "MPG"
	FileTypeMPA            FileType = "MPA"
	FileTypeMPGA           FileType = "MPGA"
	FileTypeOGG            FileType = "OGG"
	FileTypeOPUS           FileType = "OPUS"
	FileTypePDF            FileType = "PDF"
	FileTypePCM            FileType = "PCM"
	FileTypePNG            FileType = "PNG"
	FileTypePPT            FileType = "PPT"
	FileTypePPTX           FileType = "PPTX"
	FileTypeRTF            FileType = "RTF"
	FileTypeThreeGPP       FileType = "3GPP"
	FileTypeTXT            FileType = "TXT"
	FileTypeWAV            FileType = "WAV"
	FileTypeWEBM           FileType = "WEBM"
	FileTypeWEBP           FileType = "WEBP"
	FileTypeWMV            FileType = "WMV"
	FileTypeXLS            FileType = "XLS"
	FileTypeXLSX           FileType = "XLSX"
)

// FileExtensions maps FileType to their possible file extensions
var FileExtensions = map[FileType][]string{
	FileTypeAAC:            {"aac"},
	FileTypeCSV:            {"csv"},
	FileTypeDOC:            {"doc"},
	FileTypeDOCX:           {"docx"},
	FileTypeFLAC:           {"flac"},
	FileTypeFLV:            {"flv"},
	FileTypeGIF:            {"gif"},
	FileTypeGoogleDoc:      {"gdoc"},
	FileTypeGoogleDrawings: {"gdraw"},
	FileTypeGoogleSheets:   {"gsheet"},
	FileTypeGoogleSlides:   {"gslides"},
	FileTypeHEIC:           {"heic"},
	FileTypeHEIF:           {"heif"},
	FileTypeHTML:           {"html", "htm"},
	FileTypeJPEG:           {"jpeg", "jpg"},
	FileTypeJSON:           {"json"},
	FileTypeM4A:            {"m4a"},
	FileTypeM4V:            {"m4v"},
	FileTypeMOV:            {"mov"},
	FileTypeMP3:            {"mp3"},
	FileTypeMP4:            {"mp4"},
	FileTypeMPEG:           {"mpeg"},
	FileTypeMPEGPS:         {"mpegps"},
	FileTypeMPG:            {"mpg"},
	FileTypeMPA:            {"mpa"},
	FileTypeMPGA:           {"mpga"},
	FileTypeOGG:            {"ogg"},
	FileTypeOPUS:           {"opus"},
	FileTypePDF:            {"pdf"},
	FileTypePCM:            {"pcm"},
	FileTypePNG:            {"png"},
	FileTypePPT:            {"ppt"},
	FileTypePPTX:           {"pptx"},
	FileTypeRTF:            {"rtf"},
	FileTypeThreeGPP:       {"3gpp"},
	FileTypeTXT:            {"txt"},
	FileTypeWAV:            {"wav"},
	FileTypeWEBM:           {"webm"},
	FileTypeWEBP:           {"webp"},
	FileTypeWMV:            {"wmv"},
	FileTypeXLS:            {"xls"},
	FileTypeXLSX:           {"xlsx"},
}

// FileMimeTypes maps FileType to their MIME types
var FileMimeTypes = map[FileType]string{
	FileTypeAAC:            "audio/aac",
	FileTypeCSV:            "text/csv",
	FileTypeDOC:            "application/msword",
	FileTypeDOCX:           "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	FileTypeFLAC:           "audio/flac",
	FileTypeFLV:            "video/x-flv",
	FileTypeGIF:            "image/gif",
	FileTypeGoogleDoc:      "application/vnd.google-apps.document",
	FileTypeGoogleDrawings: "application/vnd.google-apps.drawing",
	FileTypeGoogleSheets:   "application/vnd.google-apps.spreadsheet",
	FileTypeGoogleSlides:   "application/vnd.google-apps.presentation",
	FileTypeHEIC:           "image/heic",
	FileTypeHEIF:           "image/heif",
	FileTypeHTML:           "text/html",
	FileTypeJPEG:           "image/jpeg",
	FileTypeJSON:           "application/json",
	FileTypeM4A:            "audio/x-m4a",
	FileTypeM4V:            "video/x-m4v",
	FileTypeMOV:            "video/quicktime",
	FileTypeMP3:            "audio/mpeg",
	FileTypeMP4:            "video/mp4",
	FileTypeMPEG:           "video/mpeg",
	FileTypeMPEGPS:         "video/mpegps",
	FileTypeMPG:            "video/mpg",
	FileTypeMPA:            "audio/m4a",
	FileTypeMPGA:           "audio/mpga",
	FileTypeOGG:            "audio/ogg",
	FileTypeOPUS:           "audio/opus",
	FileTypePDF:            "application/pdf",
	FileTypePCM:            "audio/pcm",
	FileTypePNG:            "image/png",
	FileTypePPT:            "application/vnd.ms-powerpoint",
	FileTypePPTX:           "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	FileTypeRTF:            "application/rtf",
	FileTypeThreeGPP:       "video/3gpp",
	FileTypeTXT:            "text/plain",
	FileTypeWAV:            "audio/wav",
	FileTypeWEBM:           "video/webm",
	FileTypeWEBP:           "image/webp",
	FileTypeWMV:            "video/wmv",
	FileTypeXLS:            "application/vnd.ms-excel",
	FileTypeXLSX:           "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
}

// Util Functions

// GetFileExtensionFromMimeType returns the file extension for a given MIME type
func GetFileExtensionFromMimeType(mimeType string) (string, error) {
	mimeTypeLower := strings.ToLower(mimeType)
	for fileType, mime := range FileMimeTypes {
		if strings.ToLower(mime) == mimeTypeLower {
			return FileExtensions[fileType][0], nil
		}
	}
	return "", fmt.Errorf("unknown extension for mime type: %s", mimeType)
}

// GetFileTypeFromExtension returns the FileType for a given extension
func GetFileTypeFromExtension(extension string) (FileType, error) {
	extLower := strings.ToLower(extension)
	for fileType, extensions := range FileExtensions {
		for _, ext := range extensions {
			if extLower == ext {
				return fileType, nil
			}
		}
	}
	return "", fmt.Errorf("unknown file type for extension: %s", extension)
}

// GetFileExtensionForFileType returns the primary extension for a FileType
func GetFileExtensionForFileType(fileType FileType) string {
	return FileExtensions[fileType][0]
}

// GetFileMimeTypeForFileType returns the MIME type for a FileType
func GetFileMimeTypeForFileType(fileType FileType) string {
	return FileMimeTypes[fileType]
}

// GetFileMimeTypeFromExtension returns the MIME type for a given extension
func GetFileMimeTypeFromExtension(extension string) (string, error) {
	fileType, err := GetFileTypeFromExtension(extension)
	if err != nil {
		return "", err
	}
	return GetFileMimeTypeForFileType(fileType), nil
}

// FileType Type Groupings (Videos, Images, etc)

// ImageFileTypes contains all image file types
var ImageFileTypes = map[FileType]bool{
	FileTypePNG:  true,
	FileTypeJPEG: true,
	FileTypeGIF:  true,
	FileTypeWEBP: true,
	FileTypeHEIC: true,
	FileTypeHEIF: true,
}

// IsImageFileType checks if a FileType is an image
func IsImageFileType(fileType FileType) bool {
	return ImageFileTypes[fileType]
}

// VideoFileTypes contains all video file types
var VideoFileTypes = map[FileType]bool{
	FileTypeMOV:      true,
	FileTypeMP4:      true,
	FileTypeMPEG:     true,
	FileTypeM4V:      true,
	FileTypeFLV:      true,
	FileTypeMPEGPS:   true,
	FileTypeMPG:      true,
	FileTypeWEBM:     true,
	FileTypeWMV:      true,
	FileTypeThreeGPP: true,
}

// IsVideoFileType checks if a FileType is a video
func IsVideoFileType(fileType FileType) bool {
	return VideoFileTypes[fileType]
}

// AudioFileTypes contains all audio file types
var AudioFileTypes = map[FileType]bool{
	FileTypeAAC:  true,
	FileTypeFLAC: true,
	FileTypeMP3:  true,
	FileTypeMPA:  true,
	FileTypeMPGA: true,
	FileTypeOPUS: true,
	FileTypePCM:  true,
	FileTypeWAV:  true,
}

// IsAudioFileType checks if a FileType is audio
func IsAudioFileType(fileType FileType) bool {
	return AudioFileTypes[fileType]
}

// TextFileTypes contains all text file types
var TextFileTypes = map[FileType]bool{
	FileTypeCSV:  true,
	FileTypeHTML: true,
	FileTypeRTF:  true,
	FileTypeTXT:  true,
}

// IsTextFileType checks if a FileType is text
func IsTextFileType(fileType FileType) bool {
	return TextFileTypes[fileType]
}

// Other FileType Groupings

// Gemini15AcceptedFileTypes contains accepted file types for GEMINI 1.5 through Vertex AI
// https://cloud.google.com/vertex-ai/generative-ai/docs/multimodal/send-multimodal-prompts#gemini-send-multimodal-samples-images-nodejs
var Gemini15AcceptedFileTypes = map[FileType]bool{
	// Image
	FileTypePNG:  true,
	FileTypeJPEG: true,
	FileTypeWEBP: true,
	// Audio
	FileTypeAAC:  true,
	FileTypeFLAC: true,
	FileTypeMP3:  true,
	FileTypeMPA:  true,
	FileTypeMPEG: true,
	FileTypeMPGA: true,
	FileTypeOPUS: true,
	FileTypePCM:  true,
	FileTypeWAV:  true,
	FileTypeWEBM: true,
	// Video
	FileTypeFLV:      true,
	FileTypeMOV:      true,
	FileTypeMPEGPS:   true,
	FileTypeMPG:      true,
	FileTypeMP4:      true,
	FileTypeWMV:      true,
	FileTypeThreeGPP: true,
	// PDF
	FileTypePDF: true,
	FileTypeTXT: true,
}

// IsGemini15AcceptedFileType checks if a FileType is accepted by Gemini 1.5
func IsGemini15AcceptedFileType(fileType FileType) bool {
	return Gemini15AcceptedFileTypes[fileType]
}
