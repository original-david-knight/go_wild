package server

import (
	"archive/tar"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	data "github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

var slugRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)

var reservedSlugs = map[string]bool{
	"api": true, "admin": true, "static": true, "health": true,
	"help": true, "paywall": true, "sites": true, "www": true,
	"mail": true, "ftp": true, "blog": true, "app": true,
	"accounts": true, "a": true,
}

// SiteHandlers holds the database reference and storage dir for site endpoints.
type SiteHandlers struct {
	db         gowild_data.Database
	storageDir string
}

// NewSiteHandlers creates site handlers.
func NewSiteHandlers(db gowild_data.Database) *SiteHandlers {
	dir := strings.TrimSpace(os.Getenv("SITES_STORAGE_DIR"))
	if dir == "" {
		dir = "/var/data/sites"
	}
	return &SiteHandlers{db: db, storageDir: dir}
}

// handlePublish handles POST /api/v1/sites/publish (premium auth, multipart).
func (sh *SiteHandlers) handlePublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeBadRequest(w, "method not allowed")
		return
	}

	agentID := GetAgentID(r.Context())
	if agentID == "" {
		writeBadRequest(w, "missing agent ID")
		return
	}

	// Parse multipart (50MB limit)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		writeBadRequest(w, "failed to parse multipart form: "+err.Error())
		return
	}

	slug := strings.TrimSpace(r.FormValue("slug"))
	title := strings.TrimSpace(r.FormValue("title"))

	// Validate slug
	if !slugRegex.MatchString(slug) {
		writeBadRequest(w, "slug must be 3-40 characters, lowercase letters, numbers and hyphens only, cannot start or end with hyphen")
		return
	}
	if reservedSlugs[slug] {
		writeBadRequest(w, fmt.Sprintf("slug %q is reserved", slug))
		return
	}

	// Validate title
	if title == "" {
		writeBadRequest(w, "title is required")
		return
	}
	if len(title) > 200 {
		writeBadRequest(w, "title must be 200 characters or less")
		return
	}

	// Read tar file
	file, _, err := r.FormFile("file")
	if err != nil {
		writeBadRequest(w, "file is required")
		return
	}
	defer file.Close()

	tarData, err := io.ReadAll(file)
	if err != nil {
		writeInternalError(w, "failed to read uploaded file")
		return
	}

	// Check ownership if slug exists
	existing, _ := data.GetAgentSiteUnscoped(r.Context(), sh.db, slug)
	if existing != nil && existing.AgentID != agentID {
		writeBadRequest(w, fmt.Sprintf("slug %q is owned by another agent", slug))
		return
	}

	// Extract tar to storage dir
	siteDir := filepath.Join(sh.storageDir, slug)
	// Remove existing content for idempotent re-publish
	os.RemoveAll(siteDir)
	if err := os.MkdirAll(siteDir, 0755); err != nil {
		writeInternalError(w, "failed to create site directory")
		return
	}

	fileCount, totalSize, err := extractTar(tarData, siteDir)
	if err != nil {
		os.RemoveAll(siteDir)
		writeBadRequest(w, "failed to extract site: "+err.Error())
		return
	}

	// Verify index.html exists
	if _, err := os.Stat(filepath.Join(siteDir, "index.html")); os.IsNotExist(err) {
		os.RemoveAll(siteDir)
		writeBadRequest(w, "site directory must contain index.html at root level")
		return
	}

	// Upsert DB record
	site := &data.AgentSite{
		ID:        slug,
		AgentID:   agentID,
		Title:     title,
		FileCount: fileCount,
		TotalSize: totalSize,
		Status:    "active",
	}
	if err := data.UpsertAgentSiteUnscoped(r.Context(), sh.db, site); err != nil {
		os.RemoveAll(siteDir)
		writeInternalError(w, "failed to save site record: "+err.Error())
		return
	}

	// Build site URL
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") == "" {
		scheme = "http"
	}
	host := r.Host
	siteURL := scheme + "://" + host + "/sites/" + slug + "/"

	writeJSON(w, http.StatusOK, map[string]any{
		"slug":       slug,
		"site_url":   siteURL,
		"file_count": fileCount,
		"total_size": totalSize,
	})
}

// handleList handles GET /api/v1/sites/list (premium auth).
func (sh *SiteHandlers) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "method not allowed")
		return
	}

	agentID := GetAgentID(r.Context())
	if agentID == "" {
		writeBadRequest(w, "missing agent ID")
		return
	}

	sites, err := data.ListAgentSitesUnscoped(r.Context(), sh.db, agentID)
	if err != nil {
		writeInternalError(w, "failed to list sites: "+err.Error())
		return
	}

	result := make([]map[string]any, len(sites))
	for i, s := range sites {
		result[i] = map[string]any{
			"slug":       s.ID,
			"title":      s.Title,
			"file_count": s.FileCount,
			"total_size": s.TotalSize,
			"status":     s.Status,
			"created_at": s.CreatedAt,
			"updated_at": s.UpdatedAt,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"sites": result})
}

// HandleServeStatic serves static files from published sites.
// Route: GET /sites/{slug}/ or /sites/{slug}/{path...}
func (sh *SiteHandlers) HandleServeStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "method not allowed")
		return
	}

	// Parse slug and path from URL: /sites/{slug}/{path...}
	trimmed := strings.TrimPrefix(r.URL.Path, "/sites/")
	if trimmed == "" {
		writeNotFound(w, "missing site slug")
		return
	}

	parts := strings.SplitN(trimmed, "/", 2)
	slug := parts[0]
	subPath := ""
	if len(parts) == 2 {
		subPath = parts[1]
	}

	// Default to index.html
	if subPath == "" || strings.HasSuffix(subPath, "/") {
		subPath += "index.html"
	}

	// Validate slug format
	if !slugRegex.MatchString(slug) {
		writeNotFound(w, "invalid site slug")
		return
	}

	// Check site exists and is active
	site, err := data.GetAgentSiteUnscoped(r.Context(), sh.db, slug)
	if err != nil || site == nil || site.Status != "active" {
		writeNotFound(w, "site not found")
		return
	}

	// Path traversal protection
	siteDir := filepath.Join(sh.storageDir, slug)
	cleanPath := filepath.Clean(subPath)
	if strings.Contains(cleanPath, "..") {
		writeNotFound(w, "invalid path")
		return
	}
	fullPath := filepath.Join(siteDir, cleanPath)

	// Verify path stays within site directory
	if !strings.HasPrefix(fullPath, siteDir+string(filepath.Separator)) && fullPath != siteDir {
		writeNotFound(w, "invalid path")
		return
	}

	// Check file exists
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		writeNotFound(w, "file not found")
		return
	}

	// Set Content-Type based on extension
	ext := filepath.Ext(fullPath)
	contentType := mime.TypeByExtension(ext)
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	// Cache control: cache non-HTML assets
	if ext != ".html" && ext != ".htm" {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}

	http.ServeFile(w, r, fullPath)
}

// extractTar reads tar data, strips the top-level Docker directory wrapper,
// and extracts files with path traversal protection.
func extractTar(tarData []byte, destDir string) (fileCount int, totalSize int64, err error) {
	tr := tar.NewReader(strings.NewReader(string(tarData)))

	// Docker's CopyFromContainer wraps files in a top-level directory.
	// We detect and strip it.
	var topLevelDir string
	firstEntry := true

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, 0, fmt.Errorf("failed to read tar: %w", err)
		}

		name := header.Name

		// Detect top-level directory wrapper from Docker
		if firstEntry {
			firstEntry = false
			// If the first entry is a directory, it's likely Docker's wrapper
			if header.Typeflag == tar.TypeDir {
				topLevelDir = strings.TrimSuffix(name, "/") + "/"
				continue
			}
		}

		// Strip top-level directory if detected
		if topLevelDir != "" {
			if !strings.HasPrefix(name, topLevelDir) {
				continue
			}
			name = strings.TrimPrefix(name, topLevelDir)
		}

		if name == "" || name == "." {
			continue
		}

		// Path traversal protection
		cleanName := filepath.Clean(name)
		if strings.Contains(cleanName, "..") {
			continue
		}

		targetPath := filepath.Join(destDir, cleanName)
		if !strings.HasPrefix(targetPath, destDir+string(filepath.Separator)) && targetPath != destDir {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return 0, 0, fmt.Errorf("failed to create directory %s: %w", cleanName, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return 0, 0, fmt.Errorf("failed to create parent dir for %s: %w", cleanName, err)
			}
			f, err := os.Create(targetPath)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to create file %s: %w", cleanName, err)
			}
			n, err := io.Copy(f, tr)
			f.Close()
			if err != nil {
				return 0, 0, fmt.Errorf("failed to write file %s: %w", cleanName, err)
			}
			fileCount++
			totalSize += n
		}
	}

	return fileCount, totalSize, nil
}
