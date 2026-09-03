package claudellm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// usageResponse is the endpoint's answer on 2026-09-03, token-free: the
// account had 5% of its five-hour window used, 52% of the week across
// models, and 92% of the week's Fable-scoped window.
const usageResponse = `{
  "five_hour": {"utilization": 5.0, "resets_at": "2026-09-03T18:29:59.629017+00:00", "limit_dollars": null, "locked_reason": null},
  "seven_day": {"utilization": 52.0, "resets_at": "2026-09-04T09:59:59.629037+00:00", "limit_dollars": null, "locked_reason": null},
  "seven_day_oauth_apps": null, "seven_day_opus": null, "seven_day_sonnet": null,
  "nimbus_quill": {"utilization": 0.0, "resets_at": null},
  "extra_usage": {"is_enabled": false, "monthly_limit": null},
  "limits": [
    {"kind": "session", "group": "session", "percent": 5, "severity": "normal", "resets_at": "2026-09-03T18:29:59.629017+00:00", "scope": null, "is_active": false},
    {"kind": "weekly_all", "group": "weekly", "percent": 52, "severity": "normal", "resets_at": "2026-09-04T09:59:59.629037+00:00", "scope": null, "is_active": false},
    {"kind": "weekly_scoped", "group": "weekly", "percent": 92, "severity": "critical", "resets_at": "2026-09-04T09:59:59.629279+00:00", "scope": {"model": {"id": null, "display_name": "Fable"}, "surface": null}, "is_active": true}
  ],
  "spend": {"used": {"amount_minor": 0, "currency": "USD", "exponent": 2}, "limit": null, "percent": 0},
  "member_dashboard_available": false
}`

func TestParseUsage(t *testing.T) {
	u, err := ParseUsage([]byte(usageResponse))
	if err != nil {
		t.Fatal(err)
	}
	if u.Session.Used != 5 || !u.Session.ResetsAt.Equal(time.Date(2026, 9, 3, 18, 29, 59, 629017000, time.UTC)) {
		t.Fatalf("session: %+v", u.Session)
	}
	if u.Weekly.Used != 52 || !u.Weekly.ResetsAt.Equal(time.Date(2026, 9, 4, 9, 59, 59, 629037000, time.UTC)) {
		t.Fatalf("weekly: %+v", u.Weekly)
	}
	if len(u.Scoped) != 1 || u.Scoped[0].Model != "Fable" || u.Scoped[0].Used != 92 || u.Scoped[0].ResetsAt.IsZero() {
		t.Fatalf("scoped: %+v", u.Scoped)
	}
	if _, err := ParseUsage([]byte(`{"limits": []}`)); err == nil {
		t.Fatal("a response without the windows parsed")
	}
	if _, err := ParseUsage([]byte(`not json`)); err == nil {
		t.Fatal("garbage parsed")
	}
}

func TestUsageReaderPresentsTheToken(t *testing.T) {
	dir := t.TempDir()
	creds := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(creds, []byte(`{"claudeAiOauth":{"accessToken":"sk-ant-oat01-test","refreshToken":"x"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotAuth, gotBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotBeta = r.Header.Get("Authorization"), r.Header.Get("anthropic-beta")
		if gotAuth != "Bearer sk-ant-oat01-test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(usageResponse))
	}))
	defer srv.Close()
	r := UsageReader{CredentialsPath: creds, URL: srv.URL}
	u, err := r.Read(context.Background())
	if err != nil || u.Weekly.Used != 52 {
		t.Fatalf("read: %v %+v", err, u)
	}
	if gotBeta != "oauth-2025-04-20" {
		t.Fatalf("beta header: %q", gotBeta)
	}
	// A refused token is its own error, so the caller can keep its last
	// reading rather than treat the account as unlimited.
	if err := os.WriteFile(creds, []byte(`{"claudeAiOauth":{"accessToken":"stale"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Read(context.Background()); !errors.Is(err, ErrUsageUnauthorized) {
		t.Fatalf("refused token: %v", err)
	}
	// No token at all is an error that names the file.
	if err := os.WriteFile(creds, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Read(context.Background()); err == nil {
		t.Fatal("missing token read")
	}
	if _, err := (UsageReader{CredentialsPath: filepath.Join(dir, "missing"), URL: srv.URL}).Read(context.Background()); err == nil {
		t.Fatal("missing file read")
	}
}
