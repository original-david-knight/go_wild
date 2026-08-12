package gowild_ytmusic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const testCookie = "VISITOR_INFO1_LIVE=abc; __Secure-3PAPISID=testsapisid; PREF=f1"

func TestExtractSAPISID(t *testing.T) {
	if got, err := extractSAPISID(testCookie); err != nil || got != "testsapisid" {
		t.Fatalf("extractSAPISID = %q, %v; want testsapisid, nil", got, err)
	}
	if got, err := extractSAPISID("SAPISID=legacy; PREF=f1"); err != nil || got != "legacy" {
		t.Fatalf("extractSAPISID fallback = %q, %v; want legacy, nil", got, err)
	}
	if _, err := extractSAPISID("PREF=f1; VISITOR_INFO1_LIVE=abc"); err == nil {
		t.Fatal("extractSAPISID without either cookie name should error")
	}
}

func TestSAPISIDHash(t *testing.T) {
	// sha1("1700000000 testsapisid https://music.youtube.com"), computed
	// independently with sha1sum.
	want := "SAPISIDHASH 1700000000_f4115ca88d67a648ff621771c417aacbbce2e6b3"
	if got := sapisidHash("testsapisid", time.Unix(1700000000, 0)); got != want {
		t.Fatalf("sapisidHash = %q; want %q", got, want)
	}
}

func TestNewClientValidates(t *testing.T) {
	if _, err := NewClient(nil); err == nil {
		t.Fatal("NewClient(nil) should error")
	}
	if _, err := NewClient(&Credentials{Cookie: "PREF=f1"}); err == nil {
		t.Fatal("NewClient with a SAPISID-less cookie should error")
	}
	if _, err := NewClient(&Credentials{Cookie: testCookie}); err != nil {
		t.Fatalf("NewClient with a valid cookie: %v", err)
	}
}

// roundTripperFunc adapts a function into a stub http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func stubClient(t *testing.T, creds *Credentials, rt roundTripperFunc) *Client {
	t.Helper()
	c, err := NewClient(creds, WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestBrowseRequestShape(t *testing.T) {
	creds := &Credentials{Cookie: testCookie, AuthUser: "2"}
	var captured *http.Request
	var capturedBody []byte
	c := stubClient(t, creds, func(req *http.Request) (*http.Response, error) {
		captured = req
		var err error
		capturedBody, err = io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		return jsonResponse(200, `{"contents": {}}`), nil
	})

	resp, err := c.browse(context.Background(), map[string]any{"browseId": "FEmusic_liked_playlists"})
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if _, ok := resp["contents"]; !ok {
		t.Errorf("browse response not passed through: %v", resp)
	}

	if captured.Method != http.MethodPost {
		t.Errorf("method = %s; want POST", captured.Method)
	}
	if got := captured.URL.String(); got != "https://music.youtube.com/youtubei/v1/browse?alt=json" {
		t.Errorf("URL = %s", got)
	}
	headerWant := map[string]string{
		"Cookie":          testCookie,
		"Origin":          "https://music.youtube.com",
		"X-Origin":        "https://music.youtube.com",
		"X-Goog-Authuser": "2",
		"Content-Type":    "application/json",
	}
	for name, want := range headerWant {
		if got := captured.Header.Get(name); got != want {
			t.Errorf("header %s = %q; want %q", name, got, want)
		}
	}
	if auth := captured.Header.Get("Authorization"); !strings.HasPrefix(auth, "SAPISIDHASH ") {
		t.Errorf("Authorization = %q; want SAPISIDHASH prefix", auth)
	}
	if ua := captured.Header.Get("User-Agent"); ua == "" {
		t.Error("User-Agent header missing")
	}

	var body map[string]any
	if err := json.Unmarshal(capturedBody, &body); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if name, ok := navString(body, "context", "client", "clientName"); !ok || name != "WEB_REMIX" {
		t.Errorf("context.client.clientName = %q, %v; want WEB_REMIX", name, ok)
	}
	if v, ok := navString(body, "context", "client", "clientVersion"); !ok || v != clientVersion {
		t.Errorf("context.client.clientVersion = %q, %v; want %q", v, ok, clientVersion)
	}
	if id, ok := navString(body, "browseId"); !ok || id != "FEmusic_liked_playlists" {
		t.Errorf("browseId = %q, %v; request-specific keys must survive the context merge", id, ok)
	}
}

func TestBrowseDefaultAuthUser(t *testing.T) {
	var got string
	c := stubClient(t, &Credentials{Cookie: testCookie}, func(req *http.Request) (*http.Response, error) {
		got = req.Header.Get("X-Goog-Authuser")
		return jsonResponse(200, `{}`), nil
	})
	if _, err := c.browse(context.Background(), map[string]any{"browseId": "x"}); err != nil {
		t.Fatalf("browse: %v", err)
	}
	if got != "0" {
		t.Errorf("X-Goog-AuthUser = %q; want default 0", got)
	}
}

func TestBrowseErrorPayloadAuth(t *testing.T) {
	body := `{"error": {"code": 401, "message": "Request is missing required authentication credential.", "status": "UNAUTHENTICATED"}}`
	c := stubClient(t, &Credentials{Cookie: testCookie}, func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, body), nil
	})
	_, err := c.browse(context.Background(), map[string]any{"browseId": "x"})
	if !errors.Is(err, ErrAuthExpired) {
		t.Fatalf("200-with-error-payload code 401: err = %v; want ErrAuthExpired", err)
	}
}

func TestBrowseHTTP403(t *testing.T) {
	c := stubClient(t, &Credentials{Cookie: testCookie}, func(*http.Request) (*http.Response, error) {
		return jsonResponse(403, `{"error": {"code": 403, "message": "Forbidden"}}`), nil
	})
	_, err := c.browse(context.Background(), map[string]any{"browseId": "x"})
	if !errors.Is(err, ErrAuthExpired) {
		t.Fatalf("HTTP 403: err = %v; want ErrAuthExpired", err)
	}
}

func TestBrowseNonAuthErrors(t *testing.T) {
	c := stubClient(t, &Credentials{Cookie: testCookie}, func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"error": {"code": 500, "message": "Internal error encountered."}}`), nil
	})
	_, err := c.browse(context.Background(), map[string]any{"browseId": "x"})
	if err == nil || errors.Is(err, ErrAuthExpired) {
		t.Fatalf("error payload code 500: err = %v; want a non-auth error", err)
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "Internal error") {
		t.Errorf("error should carry payload code and message, got: %v", err)
	}

	c = stubClient(t, &Credentials{Cookie: testCookie}, func(*http.Request) (*http.Response, error) {
		return jsonResponse(503, `upstream unavailable`), nil
	})
	_, err = c.browse(context.Background(), map[string]any{"browseId": "x"})
	if err == nil || errors.Is(err, ErrAuthExpired) {
		t.Fatalf("HTTP 503: err = %v; want a non-auth error", err)
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error should carry the HTTP status, got: %v", err)
	}
}
