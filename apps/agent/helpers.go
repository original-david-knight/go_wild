package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/charmbracelet/glamour"
	"github.com/fatih/color"
)

// fileAttachment holds file data to attach to the next message.
type fileAttachment struct {
	data     []byte
	mimeType string
	name     string
}

// imageAttachment is an alias for backward compatibility
type imageAttachment = fileAttachment

// inputResult holds the result of an async readline
type inputResult struct {
	line string
	err  error
}

// agentResult holds the result of running the agent.
type agentResult struct {
	History      []loop.Message
	TokensUsed   int
	ContextLimit bool // True if stopped due to context limit
	FinalText    string
	LastError    string // Last error message from the run, if any
}

// getAgentID returns the agent ID from flag, env var, or default.
func getAgentID() string {
	// Flag takes precedence
	if *agentFlag != "" {
		return strings.ToLower(*agentFlag)
	}
	// Then env var
	if envID := os.Getenv("GOWILD_AGENT_ID"); envID != "" {
		return strings.ToLower(envID)
	}
	// Default to jake
	return "jake"
}

func getHistoryFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home + "/.gowild_agent_history"
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func printWelcome(agentID string) {
	fmt.Println(color.CyanString("╭─────────────────────────────────────╮"))
	fmt.Println(color.CyanString("│") + "       " + color.HiWhiteString("GoWild Agent") + "                 " + color.CyanString("│"))
	fmt.Println(color.CyanString("│") + fmt.Sprintf("   Agent: %-25s  ", agentID) + color.CyanString("│"))
	fmt.Println(color.CyanString("│") + "   Type /help for commands          " + color.CyanString("│"))
	fmt.Println(color.CyanString("╰─────────────────────────────────────╯"))
	fmt.Println()
}

// loadImageFromFile loads an image from a file path.
func loadImageFromFile(path string) (*imageAttachment, error) {
	// Expand ~ to home directory
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, path[2:])
	}

	imgData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Detect MIME type
	mimeType := http.DetectContentType(imgData)
	if !strings.HasPrefix(mimeType, "image/") {
		return nil, fmt.Errorf("file is not an image (detected: %s)", mimeType)
	}

	return &imageAttachment{
		data:     imgData,
		mimeType: mimeType,
		name:     filepath.Base(path),
	}, nil
}

// loadFile loads any file from a file path.
// Supports images, PDFs, text files, and other formats that Gemini can process.
func loadFile(path string) (*fileAttachment, error) {
	// Expand ~ to home directory
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, path[2:])
	}

	fileData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Detect MIME type from content
	mimeType := http.DetectContentType(fileData)

	// Override for common file extensions that DetectContentType doesn't handle well
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".pdf":
		mimeType = "application/pdf"
	case ".md", ".markdown":
		mimeType = "text/markdown"
	case ".json":
		mimeType = "application/json"
	case ".yaml", ".yml":
		mimeType = "application/x-yaml"
	case ".csv":
		mimeType = "text/csv"
	case ".go":
		mimeType = "text/x-go"
	case ".py":
		mimeType = "text/x-python"
	case ".js":
		mimeType = "text/javascript"
	case ".ts":
		mimeType = "text/typescript"
	case ".html":
		mimeType = "text/html"
	case ".css":
		mimeType = "text/css"
	case ".xml":
		mimeType = "application/xml"
	case ".txt":
		mimeType = "text/plain"
	}

	return &fileAttachment{
		data:     fileData,
		mimeType: mimeType,
		name:     filepath.Base(path),
	}, nil
}

// loadImageFromClipboard loads an image from the system clipboard.
// Supports Wayland (wl-paste) and X11 (xclip).
func loadImageFromClipboard() (*imageAttachment, error) {
	var imgData []byte
	var err error

	// Try wl-paste first (Wayland - used by Ghostty)
	imgData, err = exec.Command("wl-paste", "-t", "image/png").Output()
	if err == nil && len(imgData) > 0 {
		return &imageAttachment{
			data:     imgData,
			mimeType: "image/png",
			name:     "clipboard.png",
		}, nil
	}

	// Try xclip (X11)
	imgData, err = exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o").Output()
	if err == nil && len(imgData) > 0 {
		return &imageAttachment{
			data:     imgData,
			mimeType: "image/png",
			name:     "clipboard.png",
		}, nil
	}

	// Check if there's any image type in clipboard
	var buf bytes.Buffer
	cmd := exec.Command("wl-paste", "--list-types")
	cmd.Stdout = &buf
	if cmd.Run() == nil {
		types := buf.String()
		if strings.Contains(types, "image/") {
			// Try to get whatever image format is available
			for _, imgType := range []string{"image/png", "image/jpeg", "image/gif", "image/webp"} {
				imgData, err = exec.Command("wl-paste", "-t", imgType).Output()
				if err == nil && len(imgData) > 0 {
					ext := strings.TrimPrefix(imgType, "image/")
					return &imageAttachment{
						data:     imgData,
						mimeType: imgType,
						name:     "clipboard." + ext,
					}, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("no image found in clipboard (install wl-paste for Wayland or xclip for X11)")
}

// completeFilePath provides file path completion for readline.
func completeFilePath(line string) []string {
	// Extract the path being typed (after the command)
	parts := strings.Fields(line)
	var prefix string
	if len(parts) > 1 {
		prefix = parts[len(parts)-1]
	}

	// Expand ~ to home directory
	expandedPrefix := prefix
	if strings.HasPrefix(prefix, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			expandedPrefix = filepath.Join(home, prefix[2:])
		}
	} else if prefix == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return []string{home + "/"}
		}
	}

	// If empty, list current directory
	if expandedPrefix == "" {
		expandedPrefix = "."
	}

	// Get directory and file prefix
	dir := filepath.Dir(expandedPrefix)
	filePrefix := filepath.Base(expandedPrefix)

	// If the path ends with /, list that directory
	if strings.HasSuffix(prefix, "/") || strings.HasSuffix(prefix, string(filepath.Separator)) {
		dir = expandedPrefix
		filePrefix = ""
	}

	// Read directory
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var completions []string
	for _, entry := range entries {
		name := entry.Name()
		// Skip hidden files unless prefix starts with .
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(filePrefix, ".") {
			continue
		}

		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(filePrefix)) {
			fullPath := filepath.Join(dir, name)
			// Re-add ~ prefix if it was there
			if strings.HasPrefix(prefix, "~/") {
				home, _ := os.UserHomeDir()
				fullPath = "~/" + strings.TrimPrefix(fullPath, home+"/")
			}
			if entry.IsDir() {
				fullPath += "/"
			}
			completions = append(completions, fullPath)
		}
	}

	return completions
}

// renderMarkdown renders markdown text for terminal display.
func renderMarkdown(text string) string {
	// Split text by mermaid blocks, render each part appropriately
	mermaidRe := regexp.MustCompile("(?s)(```mermaid\\s*\n.*?```)")
	parts := mermaidRe.Split(text, -1)
	mermaidMatches := mermaidRe.FindAllString(text, -1)

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStylePath("dark"), // Use fixed dark style to avoid terminal escape sequence queries
		glamour.WithWordWrap(100),
	)
	if err != nil {
		// Fall back to just rendering mermaid blocks
		return renderMermaidBlocks(text)
	}

	var result strings.Builder
	for i, part := range parts {
		// Render non-mermaid part with glamour
		if strings.TrimSpace(part) != "" {
			rendered, err := renderer.Render(part)
			if err != nil {
				result.WriteString(part)
			} else {
				result.WriteString(rendered)
			}
		}

		// Add mermaid diagram if there's one after this part
		if i < len(mermaidMatches) {
			// Extract mermaid content and render
			innerRe := regexp.MustCompile("(?s)```mermaid\\s*\n(.*?)```")
			inner := innerRe.FindStringSubmatch(mermaidMatches[i])
			if len(inner) >= 2 {
				result.WriteString(renderMermaid(strings.TrimSpace(inner[1])))
			}
		}
	}

	return result.String()
}
