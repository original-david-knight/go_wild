package tools

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ContentEmitter is the interface for emitting rich content to the frontend.
type ContentEmitter interface {
	Image(data []byte, mimeType, alt string)
	SVG(svg, alt string)
	Audio(data []byte, mimeType, alt string)
}

// ContentTools provides tools for showing images, SVG, and audio to the user.
type ContentTools struct {
	emitter ContentEmitter
}

// NewContentTools creates a new ContentTools with the given emitter.
func NewContentTools(emitter ContentEmitter) *ContentTools {
	return &ContentTools{emitter: emitter}
}

// ShowImageInput defines the input for showing an image file to the user.
type ShowImageInput struct {
	Path string `json:"path" description:"Path to the image file to display to the user (PNG, JPEG, GIF, WebP)" required:"true"`
	Alt  string `json:"alt,omitempty" description:"Caption or description of the image"`
}

// ShowImageTool reads an image file and displays it to the user in the chat.
// The image is also sent to the model for visual understanding.
func (t *ContentTools) ShowImageTool(ctx context.Context, input ShowImageInput) (*loop.ToolResult, error) {
	if input.Path == "" {
		return loop.NewErrorResult("path is required"), nil
	}

	path := input.Path
	if !filepath.IsAbs(path) {
		path = filepath.Clean(filepath.Join("/data", path))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return loop.NewErrorResult(fmt.Sprintf("file not found: %s", path)), nil
		}
		return loop.NewErrorResult(fmt.Sprintf("failed to read file: %v", err)), nil
	}

	mimeType := detectImageMIME(path, data)
	if !strings.HasPrefix(mimeType, "image/") {
		return loop.NewErrorResult(fmt.Sprintf("not an image file: %s (detected: %s)", path, mimeType)), nil
	}

	// Emit to the frontend for the user to see
	t.emitter.Image(data, mimeType, input.Alt)

	// Also return to the model so it can see the image
	return loop.NewSuccessResultWithImage(map[string]any{
		"displayed": true,
		"path":      path,
		"mime_type": mimeType,
		"size":      len(data),
	}, data, mimeType), nil
}

// ShowSVGInput defines the input for showing SVG content to the user.
type ShowSVGInput struct {
	SVG string `json:"svg" description:"SVG markup to display to the user" required:"true"`
	Alt string `json:"alt,omitempty" description:"Caption or description of the SVG"`
}

// ShowSVGTool displays SVG content to the user in the chat.
func (t *ContentTools) ShowSVGTool(ctx context.Context, input ShowSVGInput) (*loop.ToolResult, error) {
	if input.SVG == "" {
		return loop.NewErrorResult("svg is required"), nil
	}

	// Emit to the frontend
	t.emitter.SVG(input.SVG, input.Alt)

	return loop.NewSuccessResult(map[string]any{
		"displayed": true,
		"size":      len(input.SVG),
	}), nil
}

// ShowAudioInput defines the input for showing an audio file to the user.
type ShowAudioInput struct {
	Path string `json:"path" description:"Path to the audio file to play (MP3, WAV, OGG, M4A, FLAC, AAC, WebM)" required:"true"`
	Alt  string `json:"alt,omitempty" description:"Caption or description of the audio"`
}

// ShowAudioTool reads an audio file and displays a player to the user in the chat.
func (t *ContentTools) ShowAudioTool(ctx context.Context, input ShowAudioInput) (*loop.ToolResult, error) {
	if input.Path == "" {
		return loop.NewErrorResult("path is required"), nil
	}

	path := input.Path
	if !filepath.IsAbs(path) {
		path = filepath.Clean(filepath.Join("/data", path))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return loop.NewErrorResult(fmt.Sprintf("file not found: %s", path)), nil
		}
		return loop.NewErrorResult(fmt.Sprintf("failed to read file: %v", err)), nil
	}

	mimeType := detectAudioMIME(path, data)
	if !strings.HasPrefix(mimeType, "audio/") {
		return loop.NewErrorResult(fmt.Sprintf("not an audio file: %s (detected: %s)", path, mimeType)), nil
	}

	// Emit to the frontend for the user to hear
	t.emitter.Audio(data, mimeType, input.Alt)

	return loop.NewSuccessResult(map[string]any{
		"displayed": true,
		"path":      path,
		"mime_type": mimeType,
		"size":      len(data),
	}), nil
}

// detectImageMIME detects the MIME type of image data, preferring extension-based detection.
func detectImageMIME(path string, data []byte) string {
	// Try extension first for common image types
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	}
	// Fall back to content sniffing
	return http.DetectContentType(data)
}

// detectAudioMIME detects the MIME type of audio data, preferring extension-based detection.
func detectAudioMIME(path string, data []byte) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	case ".opus":
		return "audio/opus"
	case ".m4a":
		return "audio/mp4"
	case ".flac":
		return "audio/flac"
	case ".aac":
		return "audio/aac"
	case ".webm":
		return "audio/webm"
	}
	return http.DetectContentType(data)
}
