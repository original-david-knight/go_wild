package gowild_ytmusic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rawHeaderBlob mimics Chrome DevTools "Copy request headers" on an HTTP/2
// browse request: pseudo-header lines, lowercase names, scrubbed cookie
// values.
const rawHeaderBlob = `:authority: music.youtube.com
:method: POST
:path: /youtubei/v1/browse?alt=json
:scheme: https
accept: */*
accept-encoding: gzip, deflate, br, zstd
accept-language: en-US,en;q=0.9
authorization: SAPISIDHASH 1723400000_0123456789abcdef0123456789abcdef01234567
content-length: 1216
content-type: application/json
cookie: VISITOR_INFO1_LIVE=xY9zW8vU7tS; __Secure-3PAPISID=abc123DEF456ghi78/9jklMNOpq; __Secure-3PSID=g.a000scrubbed-sid-value; PREF=f6=40000000&tz=America.New_York; SIDCC=scrubbed-sidcc
origin: https://music.youtube.com
referer: https://music.youtube.com/library/playlists
user-agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36
x-goog-authuser: 0
x-origin: https://music.youtube.com`

func TestParseRawHeaders(t *testing.T) {
	creds, err := ParseRawHeaders(rawHeaderBlob)
	if err != nil {
		t.Fatalf("ParseRawHeaders: %v", err)
	}
	wantCookie := "VISITOR_INFO1_LIVE=xY9zW8vU7tS; __Secure-3PAPISID=abc123DEF456ghi78/9jklMNOpq; __Secure-3PSID=g.a000scrubbed-sid-value; PREF=f6=40000000&tz=America.New_York; SIDCC=scrubbed-sidcc"
	if creds.Cookie != wantCookie {
		t.Errorf("Cookie = %q; want %q", creds.Cookie, wantCookie)
	}
	if creds.AuthUser != "0" {
		t.Errorf("AuthUser = %q; want 0", creds.AuthUser)
	}
	if got, err := extractSAPISID(creds.Cookie); err != nil || got != "abc123DEF456ghi78/9jklMNOpq" {
		t.Errorf("extractSAPISID(parsed cookie) = %q, %v", got, err)
	}
	if _, err := NewClient(creds); err != nil {
		t.Errorf("NewClient on parsed credentials: %v", err)
	}
}

func TestParseRawHeadersClassic(t *testing.T) {
	// HTTP/1.1-style copy: request line, canonical header casing, CRLF.
	raw := "POST /youtubei/v1/browse?alt=json HTTP/1.1\r\n" +
		"Host: music.youtube.com\r\n" +
		"Cookie: SAPISID=legacy456/sapisid; PREF=f1\r\n" +
		"X-Goog-AuthUser: 2\r\n" +
		"User-Agent: Mozilla/5.0\r\n"
	creds, err := ParseRawHeaders(raw)
	if err != nil {
		t.Fatalf("ParseRawHeaders: %v", err)
	}
	if creds.Cookie != "SAPISID=legacy456/sapisid; PREF=f1" {
		t.Errorf("Cookie = %q", creds.Cookie)
	}
	if creds.AuthUser != "2" {
		t.Errorf("AuthUser = %q; want 2", creds.AuthUser)
	}
}

func TestParseRawHeadersJoinsSplitCookies(t *testing.T) {
	// HTTP/2 permits splitting the cookie into one line per pair.
	raw := "cookie: VISITOR_INFO1_LIVE=abc\ncookie: __Secure-3PAPISID=abc123split"
	creds, err := ParseRawHeaders(raw)
	if err != nil {
		t.Fatalf("ParseRawHeaders: %v", err)
	}
	if creds.Cookie != "VISITOR_INFO1_LIVE=abc; __Secure-3PAPISID=abc123split" {
		t.Errorf("Cookie = %q; split cookie lines must be rejoined", creds.Cookie)
	}
}

func TestParseRawHeadersMissingCookie(t *testing.T) {
	raw := ":authority: music.youtube.com\nx-goog-authuser: 0\ncontent-type: application/json"
	_, err := ParseRawHeaders(raw)
	if err == nil {
		t.Fatal("ParseRawHeaders without a Cookie header should error")
	}
	if !strings.Contains(err.Error(), "Cookie") || !strings.Contains(err.Error(), "music.youtube.com") {
		t.Errorf("error should name the missing Cookie header and where to get it, got: %v", err)
	}
}

func TestParseRawHeadersMissingSAPISID(t *testing.T) {
	// A cookie from a logged-out session: no __Secure-3PAPISID, no SAPISID.
	raw := "cookie: VISITOR_INFO1_LIVE=xY9zW8vU7tS; PREF=f6=40000000\nx-goog-authuser: 0"
	_, err := ParseRawHeaders(raw)
	if err == nil {
		t.Fatal("ParseRawHeaders with a SAPISID-less cookie should error")
	}
	if !strings.Contains(err.Error(), "music.youtube.com") {
		t.Errorf("error should tell the user to re-copy from an authenticated music.youtube.com request, got: %v", err)
	}
}

func TestSaveLoadCredentialsRoundtrip(t *testing.T) {
	// The parent directory does not exist yet; SaveCredentials creates it.
	path := filepath.Join(t.TempDir(), "gowild", "ytmusic", "credentials.json")
	want := &Credentials{Cookie: testCookie, AuthUser: "2"}
	if err := SaveCredentials(path, want); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat saved file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o; want 600", got)
	}

	got, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if *got != *want {
		t.Errorf("roundtrip = %+v; want %+v", got, want)
	}

	// The temp file must not survive a successful save.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "credentials.json" {
		t.Errorf("directory should hold only credentials.json, got %v", entries)
	}

	// Overwriting an existing file goes through the same rename.
	want.AuthUser = "3"
	if err := SaveCredentials(path, want); err != nil {
		t.Fatalf("SaveCredentials overwrite: %v", err)
	}
	if got, err := LoadCredentials(path); err != nil || got.AuthUser != "3" {
		t.Errorf("after overwrite: %+v, %v", got, err)
	}
}

func TestLoadCredentialsErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.json")
	if _, err := LoadCredentials(missing); err == nil || !strings.Contains(err.Error(), missing) {
		t.Errorf("missing file: error should carry the path, got: %v", err)
	}

	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := LoadCredentials(bad); err == nil || !strings.Contains(err.Error(), bad) {
		t.Errorf("malformed JSON: error should carry the path, got: %v", err)
	}
}

func TestSaveCredentialsNil(t *testing.T) {
	if err := SaveCredentials(filepath.Join(t.TempDir(), "c.json"), nil); err == nil {
		t.Fatal("SaveCredentials(nil) should error")
	}
}
