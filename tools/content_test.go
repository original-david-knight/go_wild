package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// mockEmitter captures calls to Image and SVG.
type mockEmitter struct {
	imageData []byte
	imageMIME string
	imageAlt  string
	svgData   string
	svgAlt    string
	audioData []byte
	audioMIME string
	audioAlt  string
}

func (m *mockEmitter) Image(data []byte, mimeType, alt string) {
	m.imageData = data
	m.imageMIME = mimeType
	m.imageAlt = alt
}

func (m *mockEmitter) SVG(svg, alt string) {
	m.svgData = svg
	m.svgAlt = alt
}

func (m *mockEmitter) Audio(data []byte, mimeType, alt string) {
	m.audioData = data
	m.audioMIME = mimeType
	m.audioAlt = alt
}

func TestShowImageTool_EmptyPath(t *testing.T) {
	em := &mockEmitter{}
	ct := NewContentTools(em)

	result, err := ct.ShowImageTool(context.Background(), ShowImageInput{Path: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for empty path")
	}
}

func TestShowImageTool_NotFound(t *testing.T) {
	em := &mockEmitter{}
	ct := NewContentTools(em)

	result, err := ct.ShowImageTool(context.Background(), ShowImageInput{Path: "/nonexistent/photo.png"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for nonexistent file")
	}
}

func TestShowImageTool_Success(t *testing.T) {
	em := &mockEmitter{}
	ct := NewContentTools(em)

	// Create a temp PNG file (minimal PNG header)
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "test.png")
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	os.WriteFile(pngPath, pngHeader, 0644)

	result, err := ct.ShowImageTool(context.Background(), ShowImageInput{
		Path: pngPath,
		Alt:  "test image",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}

	// Verify emitter was called
	if em.imageMIME != "image/png" {
		t.Errorf("expected image/png, got %s", em.imageMIME)
	}
	if em.imageAlt != "test image" {
		t.Errorf("expected alt 'test image', got %q", em.imageAlt)
	}
	if len(em.imageData) != len(pngHeader) {
		t.Errorf("expected %d bytes, got %d", len(pngHeader), len(em.imageData))
	}
}

func TestShowImageTool_RelativePath(t *testing.T) {
	em := &mockEmitter{}
	ct := NewContentTools(em)

	// Relative paths should be resolved to /data/...
	result, err := ct.ShowImageTool(context.Background(), ShowImageInput{Path: "photo.png"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fail because /data/photo.png doesn't exist, but shouldn't panic
	if result.Success {
		t.Error("expected failure for nonexistent relative path")
	}
}

func TestShowImageTool_NotAnImage(t *testing.T) {
	em := &mockEmitter{}
	ct := NewContentTools(em)

	// Create a text file with .txt extension
	dir := t.TempDir()
	txtPath := filepath.Join(dir, "test.txt")
	os.WriteFile(txtPath, []byte("hello world"), 0644)

	result, err := ct.ShowImageTool(context.Background(), ShowImageInput{Path: txtPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for non-image file")
	}
}

func TestShowSVGTool_Empty(t *testing.T) {
	em := &mockEmitter{}
	ct := NewContentTools(em)

	result, err := ct.ShowSVGTool(context.Background(), ShowSVGInput{SVG: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for empty SVG")
	}
}

func TestShowSVGTool_Success(t *testing.T) {
	em := &mockEmitter{}
	ct := NewContentTools(em)

	svg := `<svg xmlns="http://www.w3.org/2000/svg"><circle r="10"/></svg>`
	result, err := ct.ShowSVGTool(context.Background(), ShowSVGInput{
		SVG: svg,
		Alt: "circle",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}

	if em.svgData != svg {
		t.Error("SVG data mismatch")
	}
	if em.svgAlt != "circle" {
		t.Errorf("expected alt 'circle', got %q", em.svgAlt)
	}
}

func TestShowAudioTool_EmptyPath(t *testing.T) {
	em := &mockEmitter{}
	ct := NewContentTools(em)

	result, err := ct.ShowAudioTool(context.Background(), ShowAudioInput{Path: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for empty path")
	}
}

func TestShowAudioTool_NotFound(t *testing.T) {
	em := &mockEmitter{}
	ct := NewContentTools(em)

	result, err := ct.ShowAudioTool(context.Background(), ShowAudioInput{Path: "/nonexistent/audio.mp3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for nonexistent file")
	}
}

func TestShowAudioTool_Success(t *testing.T) {
	em := &mockEmitter{}
	ct := NewContentTools(em)

	dir := t.TempDir()
	audioPath := filepath.Join(dir, "test.mp3")
	audioBytes := []byte{0x49, 0x44, 0x33, 0x03, 0x00} // ID3 header prefix
	os.WriteFile(audioPath, audioBytes, 0644)

	result, err := ct.ShowAudioTool(context.Background(), ShowAudioInput{
		Path: audioPath,
		Alt:  "test audio",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}

	if em.audioMIME != "audio/mpeg" {
		t.Errorf("expected audio/mpeg, got %s", em.audioMIME)
	}
	if em.audioAlt != "test audio" {
		t.Errorf("expected alt 'test audio', got %q", em.audioAlt)
	}
	if len(em.audioData) != len(audioBytes) {
		t.Errorf("expected %d bytes, got %d", len(audioBytes), len(em.audioData))
	}
}

func TestShowAudioTool_RelativePath(t *testing.T) {
	em := &mockEmitter{}
	ct := NewContentTools(em)

	result, err := ct.ShowAudioTool(context.Background(), ShowAudioInput{Path: "audio.mp3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for nonexistent relative path")
	}
}

func TestShowAudioTool_NotAudio(t *testing.T) {
	em := &mockEmitter{}
	ct := NewContentTools(em)

	dir := t.TempDir()
	txtPath := filepath.Join(dir, "test.txt")
	os.WriteFile(txtPath, []byte("hello world"), 0644)

	result, err := ct.ShowAudioTool(context.Background(), ShowAudioInput{Path: txtPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for non-audio file")
	}
}

func TestDetectImageMIME_ContentSniffing(t *testing.T) {
	// Test with unknown extension but valid image data
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	got := detectImageMIME("file.unknown", pngHeader)
	if got != "image/png" {
		t.Errorf("expected image/png from content sniffing, got %s", got)
	}
}
