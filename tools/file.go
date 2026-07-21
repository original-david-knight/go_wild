package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ReadFileInput defines the input for reading a file.
type ReadFileInput struct {
	Path      string `json:"path" description:"Path to the file to read (relative to /data or absolute)" required:"true"`
	StartLine int    `json:"start_line" description:"Line number to start reading from (1-indexed, default: 1)" required:"false"`
	Limit     int    `json:"limit" description:"Maximum number of lines to read (default: all)" required:"false"`
}

// WriteFileInput defines the input for writing a file.
type WriteFileInput struct {
	Path    string `json:"path" description:"Path to the file to write (relative to /data or absolute)" required:"true"`
	Content string `json:"content" description:"Content to write to the file" required:"true"`
}

// EditFileInput defines the input for editing a file.
type EditFileInput struct {
	Path       string `json:"path" description:"Path to the file to edit (relative to /data or absolute)" required:"true"`
	OldContent string `json:"old_content" description:"Exact content to find and replace (must match exactly including whitespace)" required:"true"`
	NewContent string `json:"new_content" description:"New content to replace old_content with" required:"true"`
	ReplaceAll bool   `json:"replace_all" description:"If true, replace all occurrences. If false (default), replace only first occurrence" required:"false"`
}

// ListFilesInput defines the input for listing files.
type ListFilesInput struct {
	Path      string `json:"path" description:"Directory path to list (relative to /data or absolute, default: /data)" required:"false"`
	Recursive bool   `json:"recursive" description:"If true, list files recursively" required:"false"`
	Pattern   string `json:"pattern" description:"Glob pattern to filter files (e.g., '*.go', '*.py')" required:"false"`
}

// FileTools provides file operation tools.
// Only available when running inside a container (sandboxed environment).
type FileTools struct{}

// NewFileTools creates a new FileTools instance.
// Returns nil if not running in a container.
func NewFileTools() *FileTools {
	if !IsInContainer() {
		return nil
	}
	return &FileTools{}
}

// resolvePath resolves a path relative to /data if not absolute.
func resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join("/data", path))
}

// ReadFileTool reads the contents of a file.
func (f *FileTools) ReadFileTool(ctx context.Context, input ReadFileInput) (*loop.ToolResult, error) {
	if input.Path == "" {
		return loop.NewErrorResult("path is required"), nil
	}

	path := resolvePath(input.Path)

	// Check if file exists
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return loop.NewErrorResult(fmt.Sprintf("file not found: %s", path)), nil
		}
		return loop.NewErrorResult(fmt.Sprintf("failed to stat file: %v", err)), nil
	}

	if info.IsDir() {
		return loop.NewErrorResult(fmt.Sprintf("%s is a directory, not a file", path)), nil
	}

	// Read the file
	content, err := os.ReadFile(path)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to read file: %v", err)), nil
	}

	// Split into lines for line-based operations
	lines := strings.Split(string(content), "\n")
	totalLines := len(lines)

	// Apply start_line and limit if specified
	startLine := input.StartLine
	if startLine <= 0 {
		startLine = 1
	}
	if startLine > totalLines {
		return loop.NewSuccessResult(map[string]any{
			"path":        path,
			"content":     "",
			"total_lines": totalLines,
			"start_line":  startLine,
			"end_line":    startLine,
			"message":     fmt.Sprintf("start_line %d exceeds total lines %d", startLine, totalLines),
		}), nil
	}

	endLine := totalLines
	if input.Limit > 0 {
		endLine = startLine + input.Limit - 1
		if endLine > totalLines {
			endLine = totalLines
		}
	}

	// Extract requested lines (1-indexed)
	selectedLines := lines[startLine-1 : endLine]

	// Add line numbers for context
	var numberedLines []string
	for i, line := range selectedLines {
		lineNum := startLine + i
		numberedLines = append(numberedLines, fmt.Sprintf("%4d | %s", lineNum, line))
	}

	result := map[string]any{
		"path":        path,
		"content":     strings.Join(selectedLines, "\n"),
		"numbered":    strings.Join(numberedLines, "\n"),
		"total_lines": totalLines,
		"start_line":  startLine,
		"end_line":    endLine,
		"size_bytes":  info.Size(),
	}

	// Add truncation note if applicable
	if endLine < totalLines {
		result["truncated"] = true
		result["remaining_lines"] = totalLines - endLine
	}

	return loop.NewSuccessResult(result), nil
}

// WriteFileTool writes content to a file.
func (f *FileTools) WriteFileTool(ctx context.Context, input WriteFileInput) (*loop.ToolResult, error) {
	if input.Path == "" {
		return loop.NewErrorResult("path is required"), nil
	}

	path := resolvePath(input.Path)

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to create directory %s: %v", dir, err)), nil
	}

	// Check if file exists (for reporting)
	existed := false
	if _, err := os.Stat(path); err == nil {
		existed = true
	}

	// Write the file
	if err := os.WriteFile(path, []byte(input.Content), 0644); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to write file: %v", err)), nil
	}

	lines := strings.Count(input.Content, "\n") + 1
	if input.Content == "" {
		lines = 0
	}

	return loop.NewSuccessResult(map[string]any{
		"success":     true,
		"path":        path,
		"size_bytes":  len(input.Content),
		"lines":       lines,
		"created":     !existed,
		"overwritten": existed,
	}), nil
}

// EditFileTool edits a file by replacing content.
func (f *FileTools) EditFileTool(ctx context.Context, input EditFileInput) (*loop.ToolResult, error) {
	if input.Path == "" {
		return loop.NewErrorResult("path is required"), nil
	}
	if input.OldContent == "" {
		return loop.NewErrorResult("old_content is required"), nil
	}

	path := resolvePath(input.Path)

	// Read current content
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return loop.NewErrorResult(fmt.Sprintf("file not found: %s", path)), nil
		}
		return loop.NewErrorResult(fmt.Sprintf("failed to read file: %v", err)), nil
	}

	contentStr := string(content)

	// Check if old_content exists
	count := strings.Count(contentStr, input.OldContent)
	if count == 0 {
		// Provide helpful context for debugging
		previewLen := 100
		if len(input.OldContent) < previewLen {
			previewLen = len(input.OldContent)
		}
		return loop.NewErrorResult(fmt.Sprintf(
			"old_content not found in file. Search string preview: %q",
			input.OldContent[:previewLen],
		)), nil
	}

	// Replace content
	var newContent string
	var replacements int

	if input.ReplaceAll {
		newContent = strings.ReplaceAll(contentStr, input.OldContent, input.NewContent)
		replacements = count
	} else {
		newContent = strings.Replace(contentStr, input.OldContent, input.NewContent, 1)
		replacements = 1
	}

	// Write back
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to write file: %v", err)), nil
	}

	// Calculate line numbers where changes occurred
	linesBefore := strings.Count(contentStr[:strings.Index(contentStr, input.OldContent)], "\n") + 1

	return loop.NewSuccessResult(map[string]any{
		"success":          true,
		"path":             path,
		"replacements":     replacements,
		"first_occurrence": linesBefore,
		"size_before":      len(content),
		"size_after":       len(newContent),
	}), nil
}

// ListFilesTool lists files in a directory.
func (f *FileTools) ListFilesTool(ctx context.Context, input ListFilesInput) (*loop.ToolResult, error) {
	path := input.Path
	if path == "" {
		path = "/data"
	}
	path = resolvePath(path)

	// Check if directory exists
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return loop.NewErrorResult(fmt.Sprintf("directory not found: %s", path)), nil
		}
		return loop.NewErrorResult(fmt.Sprintf("failed to stat path: %v", err)), nil
	}

	if !info.IsDir() {
		return loop.NewErrorResult(fmt.Sprintf("%s is not a directory", path)), nil
	}

	var files []map[string]any
	var totalSize int64

	walkFn := func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		// Skip the root directory itself
		if filePath == path {
			return nil
		}

		// Apply pattern filter if specified
		if input.Pattern != "" {
			matched, err := filepath.Match(input.Pattern, filepath.Base(filePath))
			if err != nil || !matched {
				if info.IsDir() && !input.Recursive {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Calculate relative path
		relPath, _ := filepath.Rel(path, filePath)

		fileInfo := map[string]any{
			"name": relPath,
			"type": "file",
			"size": info.Size(),
		}

		if info.IsDir() {
			fileInfo["type"] = "directory"
		} else {
			totalSize += info.Size()
		}

		files = append(files, fileInfo)

		// If not recursive, skip subdirectories
		if !input.Recursive && info.IsDir() && filePath != path {
			return filepath.SkipDir
		}

		return nil
	}

	if input.Recursive {
		err = filepath.Walk(path, walkFn)
	} else {
		entries, err := os.ReadDir(path)
		if err != nil {
			return loop.NewErrorResult(fmt.Sprintf("failed to read directory: %v", err)), nil
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			walkFn(filepath.Join(path, entry.Name()), info, nil)
		}
	}

	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to list files: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"path":       path,
		"files":      files,
		"count":      len(files),
		"total_size": totalSize,
		"pattern":    input.Pattern,
		"recursive":  input.Recursive,
	}), nil
}

// DescribeTool implements ToolProvider for tool descriptions.
func (f *FileTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"read_file":  "Read the contents of a file. Supports reading specific line ranges with start_line and limit parameters. Returns content with line numbers for easy reference. Paths are relative to /data unless absolute.",
		"write_file": "Create or overwrite a file. Creates parent directories automatically. Use this to create new files or completely replace existing ones. For surgical edits, use edit_file instead. Paths are relative to /data unless absolute.",
		"edit_file":  "Edit a file by replacing specific content. Finds old_content (must match exactly including whitespace) and replaces it with new_content. Use replace_all=true to replace all occurrences. More efficient and safer than rewriting entire files. Paths are relative to /data unless absolute.",
		"list_files": "List files in a directory. Supports recursive listing and glob pattern filtering. Returns file names, types, and sizes. Default path is /data.",
	}
	return descriptions[name]
}
