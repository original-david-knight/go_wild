package gowild_ytmusic

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParseRawHeaders turns a DevTools "Copy request headers" blob into
// Credentials. Both header styles the copy produces are accepted: classic
// "Name: value" lines and HTTP/2 lowercase names with pseudo-header lines
// (":authority: ...", skipped). Header names match case-insensitively, and
// repeated Cookie lines are rejoined the way HTTP/2 splits them. The Cookie
// header is required and must carry a SAPISID; X-Goog-AuthUser is optional.
func ParseRawHeaders(raw string) (*Credentials, error) {
	var cookies []string
	authUser := ""
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			// Request line ("POST /... HTTP/1.1") or stray text.
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		switch {
		case strings.EqualFold(name, "cookie"):
			if value != "" {
				cookies = append(cookies, value)
			}
		case strings.EqualFold(name, "x-goog-authuser"):
			authUser = value
		}
	}
	if len(cookies) == 0 {
		return nil, fmt.Errorf("ytmusic: no Cookie header in pasted headers; in DevTools, use \"Copy request headers\" on an authenticated music.youtube.com request")
	}
	cookie := strings.Join(cookies, "; ")
	// extractSAPISID's error already tells the user to re-copy from a
	// logged-in music.youtube.com request.
	if _, err := extractSAPISID(cookie); err != nil {
		return nil, err
	}
	return &Credentials{Cookie: cookie, AuthUser: authUser}, nil
}

// LoadCredentials reads Credentials saved by SaveCredentials.
func LoadCredentials(path string) (*Credentials, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ytmusic: load credentials: %w", err)
	}
	var c Credentials
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("ytmusic: load credentials %s: %w", path, err)
	}
	return &c, nil
}

// SaveCredentials writes Credentials as indented JSON. The cookie is a live
// session secret, so the file is never partial or group-readable: the parent
// directory is created 0700, the bytes land in a same-directory 0600 temp
// file, and a rename moves it into place.
func SaveCredentials(path string, c *Credentials) error {
	if c == nil {
		return fmt.Errorf("ytmusic: save credentials %s: nil credentials", path)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("ytmusic: save credentials %s: %w", path, err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("ytmusic: save credentials %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("ytmusic: save credentials %s: %w", path, err)
	}
	tmpName := tmp.Name()
	// A no-op once the rename lands; cleanup on every failure path.
	defer os.Remove(tmpName)

	// CreateTemp's 0600 is subject to umask; pin the exact mode.
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("ytmusic: save credentials %s: chmod: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("ytmusic: save credentials %s: write: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("ytmusic: save credentials %s: close: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("ytmusic: save credentials %s: %w", path, err)
	}
	return nil
}
