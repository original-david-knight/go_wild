package server

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func setupSitesTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	srv, db := setupTestServer(t)

	// Register site table
	if err := db.AddTable(data.AgentSite{}); err != nil {
		t.Fatalf("Failed to add AgentSite table: %v", err)
	}

	// Use temp dir for file storage
	storageDir := t.TempDir()
	srv.sites.storageDir = storageDir

	return srv, storageDir
}

// makeSiteTar creates a tar archive with the given files.
// Files is a map of path → content.
func makeSiteTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("failed to write tar header for %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write tar content for %s: %v", name, err)
		}
	}
	tw.Close()
	return buf.Bytes()
}

// makeSiteTarWithDockerWrapper creates a tar with a top-level directory wrapper
// (like Docker's CopyFromContainer produces).
func makeSiteTarWithDockerWrapper(t *testing.T, wrapperDir string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Write top-level directory entry
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeDir,
		Name:     wrapperDir + "/",
		Mode:     0755,
	}); err != nil {
		t.Fatalf("failed to write wrapper dir header: %v", err)
	}

	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: wrapperDir + "/" + name,
			Mode: 0644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("failed to write tar header for %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write tar content for %s: %v", name, err)
		}
	}
	tw.Close()
	return buf.Bytes()
}

func makeMultipartSiteRequest(t *testing.T, makeReq func(string, string, []byte) *http.Request, slug, title string, tarData []byte) *http.Request {
	t.Helper()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	writer.WriteField("slug", slug)
	writer.WriteField("title", title)

	part, err := writer.CreateFormFile("file", "site.tar")
	if err != nil {
		t.Fatalf("failed to create file part: %v", err)
	}
	part.Write(tarData)
	writer.Close()

	body := buf.Bytes()
	req := makeReq("POST", "/api/v1/sites/publish", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestSitePublishSuccess(t *testing.T) {
	srv, storageDir := setupSitesTestServer(t)
	handler := srv.handler()
	_, makeReq := makePremiumAgent(t, srv)

	tarData := makeSiteTar(t, map[string]string{
		"index.html": "<html><body>Hello</body></html>",
		"app.js":     "console.log('hi')",
	})

	req := makeMultipartSiteRequest(t, makeReq, "my-site", "My Site", tarData)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["slug"] != "my-site" {
		t.Errorf("slug = %v, want my-site", resp["slug"])
	}
	siteURL, _ := resp["site_url"].(string)
	if siteURL == "" {
		t.Error("expected site_url in response")
	}
	if resp["file_count"] != float64(2) {
		t.Errorf("file_count = %v, want 2", resp["file_count"])
	}

	// Verify files were extracted
	indexPath := filepath.Join(storageDir, "my-site", "index.html")
	content, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("index.html not written: %v", err)
	}
	if string(content) != "<html><body>Hello</body></html>" {
		t.Errorf("index.html content = %q", string(content))
	}
}

func TestSitePublishWithDockerWrapper(t *testing.T) {
	srv, storageDir := setupSitesTestServer(t)
	handler := srv.handler()
	_, makeReq := makePremiumAgent(t, srv)

	tarData := makeSiteTarWithDockerWrapper(t, "mysite", map[string]string{
		"index.html": "<html>Docker wrapped</html>",
		"style.css":  "body { color: red }",
	})

	req := makeMultipartSiteRequest(t, makeReq, "docker-site", "Docker Site", tarData)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify files were extracted with wrapper stripped
	indexPath := filepath.Join(storageDir, "docker-site", "index.html")
	content, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("index.html not written: %v", err)
	}
	if string(content) != "<html>Docker wrapped</html>" {
		t.Errorf("index.html content = %q", string(content))
	}
}

func TestSitePublishMissingIndex(t *testing.T) {
	srv, _ := setupSitesTestServer(t)
	handler := srv.handler()
	_, makeReq := makePremiumAgent(t, srv)

	tarData := makeSiteTar(t, map[string]string{
		"app.js":    "console.log('no index')",
		"style.css": "body {}",
	})

	req := makeMultipartSiteRequest(t, makeReq, "no-index", "No Index", tarData)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSitePublishSlugValidation(t *testing.T) {
	srv, _ := setupSitesTestServer(t)
	handler := srv.handler()
	_, makeReq := makePremiumAgent(t, srv)

	tarData := makeSiteTar(t, map[string]string{"index.html": "<html></html>"})

	tests := []struct {
		name string
		slug string
	}{
		{"too short", "ab"},
		{"uppercase", "MyApp"},
		{"spaces", "my app"},
		{"starts with hyphen", "-myapp"},
		{"ends with hyphen", "myapp-"},
		{"special chars", "my_app!"},
		{"reserved slug", "admin"},
		{"reserved slug api", "api"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := makeMultipartSiteRequest(t, makeReq, tc.slug, "Test", tarData)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestSitePublishRequiresPremium(t *testing.T) {
	srv, _ := setupSitesTestServer(t)
	handler := srv.handler()

	// Use a non-premium agent
	body := []byte("dummy")
	req, _, _ := makeSignedRequest(t, "POST", "/api/v1/sites/publish", body)
	req.Header.Set("Content-Type", "multipart/form-data")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusPaymentRequired {
		t.Errorf("expected 402, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSitePublishIdempotent(t *testing.T) {
	srv, storageDir := setupSitesTestServer(t)
	handler := srv.handler()
	_, makeReq := makePremiumAgent(t, srv)

	// First publish
	tarData1 := makeSiteTar(t, map[string]string{
		"index.html": "<html>v1</html>",
	})
	req := makeMultipartSiteRequest(t, makeReq, "idempotent", "Idempotent Site", tarData1)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first publish: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Re-publish with different content
	tarData2 := makeSiteTar(t, map[string]string{
		"index.html": "<html>v2</html>",
		"new.js":     "new file",
	})
	req = makeMultipartSiteRequest(t, makeReq, "idempotent", "Idempotent Site v2", tarData2)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("re-publish: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify new content
	content, err := os.ReadFile(filepath.Join(storageDir, "idempotent", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "<html>v2</html>" {
		t.Errorf("expected v2, got %q", string(content))
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["file_count"] != float64(2) {
		t.Errorf("file_count = %v, want 2", resp["file_count"])
	}
}

func TestSitePublishSlugOwnership(t *testing.T) {
	srv, _ := setupSitesTestServer(t)
	handler := srv.handler()

	// Agent 1 publishes
	_, makeReq1 := makePremiumAgent(t, srv)
	tarData := makeSiteTar(t, map[string]string{"index.html": "<html>agent1</html>"})
	req := makeMultipartSiteRequest(t, makeReq1, "owned-slug", "Agent1 Site", tarData)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("agent1 publish: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Agent 2 tries to take the same slug
	_, makeReq2 := makePremiumAgent(t, srv)
	req = makeMultipartSiteRequest(t, makeReq2, "owned-slug", "Agent2 Site", tarData)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("agent2 should be rejected: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSiteServeStatic(t *testing.T) {
	srv, storageDir := setupSitesTestServer(t)
	handler := srv.handler()
	_, makeReq := makePremiumAgent(t, srv)

	// Publish a site
	tarData := makeSiteTar(t, map[string]string{
		"index.html": "<html><body>Served!</body></html>",
	})
	req := makeMultipartSiteRequest(t, makeReq, "servable", "Servable", tarData)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("publish: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify files are on disk (test may have extracted them)
	indexContent := "<html><body>Served!</body></html>"
	indexPath := filepath.Join(storageDir, "servable", "index.html")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Fatalf("index.html not found at %s", indexPath)
	}

	// GET /sites/servable/ should return index.html
	req = httptest.NewRequest("GET", "/sites/servable/", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("serve index: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(indexContent)) {
		t.Errorf("expected index content, got %q", w.Body.String())
	}
}

func TestSiteServeStaticAsset(t *testing.T) {
	srv, _ := setupSitesTestServer(t)
	handler := srv.handler()
	_, makeReq := makePremiumAgent(t, srv)

	// Publish a site with JS and CSS
	tarData := makeSiteTar(t, map[string]string{
		"index.html": "<html></html>",
		"app.js":     "console.log('hello')",
		"style.css":  "body { margin: 0 }",
	})
	req := makeMultipartSiteRequest(t, makeReq, "assets-site", "Assets", tarData)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("publish: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Serve JS file
	req = httptest.NewRequest("GET", "/sites/assets-site/app.js", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("serve JS: expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !containsAny(ct, "javascript", "text/javascript", "application/javascript") {
		t.Errorf("JS Content-Type = %q", ct)
	}

	// Serve CSS file
	req = httptest.NewRequest("GET", "/sites/assets-site/style.css", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("serve CSS: expected 200, got %d", w.Code)
	}
	ct = w.Header().Get("Content-Type")
	if !containsAny(ct, "css", "text/css") {
		t.Errorf("CSS Content-Type = %q", ct)
	}
}

func TestSiteServePathTraversal(t *testing.T) {
	srv, _ := setupSitesTestServer(t)
	handler := srv.handler()
	_, makeReq := makePremiumAgent(t, srv)

	// Publish a site
	tarData := makeSiteTar(t, map[string]string{
		"index.html": "<html></html>",
	})
	req := makeMultipartSiteRequest(t, makeReq, "traversal-test", "Traversal", tarData)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("publish: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Try path traversal attacks
	attacks := []string{
		"/sites/traversal-test/../../../etc/passwd",
		"/sites/traversal-test/..%2F..%2F..%2Fetc%2Fpasswd",
		"/sites/traversal-test/%2e%2e/%2e%2e/etc/passwd",
	}

	for _, path := range attacks {
		req = httptest.NewRequest("GET", path, nil)
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			t.Errorf("path traversal should be blocked: %s returned 200", path)
		}
	}
}

func TestSiteServeNonexistent(t *testing.T) {
	srv, _ := setupSitesTestServer(t)
	handler := srv.handler()

	req := httptest.NewRequest("GET", "/sites/nonexistent/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// containsAny checks if s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if bytes.Contains([]byte(s), []byte(sub)) {
			return true
		}
	}
	return false
}
