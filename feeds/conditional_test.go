package feeds

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// conditionalServer serves a mutable feed with cache validators and records
// what each request carried, so a test can watch the conditional handshake.
type conditionalServer struct {
	body         string
	etag         string
	lastModified string
	sentINM      []string // If-None-Match per request
	sentIMS      []string // If-Modified-Since per request
}

func (s *conditionalServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.sentINM = append(s.sentINM, r.Header.Get("If-None-Match"))
	s.sentIMS = append(s.sentIMS, r.Header.Get("If-Modified-Since"))
	if inm := r.Header.Get("If-None-Match"); inm != "" && inm == s.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml")
	w.Header().Set("ETag", s.etag)
	w.Header().Set("Last-Modified", s.lastModified)
	_, _ = w.Write([]byte(s.body))
}

func TestFetchConditionalHandshake(t *testing.T) {
	srv := &conditionalServer{
		body:         rssFixture,
		etag:         `"v1"`,
		lastModified: "Tue, 04 Aug 2026 08:15:00 GMT",
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	f := &Fetcher{Cache: memCache(t)}
	ctx := context.Background()

	// First call has nothing to validate with: a plain 200, validators
	// captured from the response, all items emitted.
	feed, v, notModified, err := f.FetchConditional(ctx, ts.URL, Validators{})
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if notModified {
		t.Fatal("a validator-less fetch answered notModified")
	}
	if srv.sentINM[0] != "" || srv.sentIMS[0] != "" {
		t.Errorf("validator-less request sent If-None-Match %q / If-Modified-Since %q", srv.sentINM[0], srv.sentIMS[0])
	}
	if v.ETag != `"v1"` || v.LastModified != "Tue, 04 Aug 2026 08:15:00 GMT" {
		t.Errorf("captured validators = %+v", v)
	}
	if len(feed.Items) != 2 {
		t.Errorf("first fetch emitted %d items, want 2", len(feed.Items))
	}

	// Second call carries the validators; the unchanged feed answers 304 and
	// the caller keeps what it had.
	feed, next, notModified, err := f.FetchConditional(ctx, ts.URL, v)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if !notModified {
		t.Fatal("an unchanged feed did not answer notModified")
	}
	if feed != nil {
		t.Errorf("a 304 returned a feed: %+v", feed)
	}
	if next != v {
		t.Errorf("a 304 replaced the validators: %+v", next)
	}
	if srv.sentINM[1] != `"v1"` || srv.sentIMS[1] != v.LastModified {
		t.Errorf("conditional request sent If-None-Match %q / If-Modified-Since %q", srv.sentINM[1], srv.sentIMS[1])
	}

	// The feed changes: a 200 with a new ETag, and the seen-markers from the
	// first fetch hold, so only the appended entry is emitted.
	srv.body = appendItem(rssFixture, `<item><title>Third story</title><guid>wire-guid-3</guid></item>`)
	srv.etag = `"v2"`
	feed, next, notModified, err = f.FetchConditional(ctx, ts.URL, next)
	if err != nil {
		t.Fatalf("third fetch: %v", err)
	}
	if notModified {
		t.Fatal("a changed feed answered notModified")
	}
	if next.ETag != `"v2"` {
		t.Errorf("validators after the change = %+v", next)
	}
	if len(feed.Items) != 1 || feed.Items[0].GUID != "wire-guid-3" {
		t.Errorf("changed feed emitted %+v, want only the appended entry", feed.Items)
	}
}
