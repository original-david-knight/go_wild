package feeds

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

// ----------------------------------------------------------------- fixtures

const rssFixture = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example Wire</title>
    <item>
      <title>First   story</title>
      <link>https://wire.example/one</link>
      <guid>wire-guid-1</guid>
      <description>What happened, in a sentence.</description>
      <pubDate>Tue, 04 Aug 2026 08:15:00 +0000</pubDate>
    </item>
    <item>
      <title>Second story</title>
      <link>https://wire.example/two</link>
      <description>No GUID on this one; the link stands in.</description>
      <pubDate>Mon, 3 Aug 2026 21:00:00 -0700</pubDate>
    </item>
    <item>
      <title>Identityless</title>
      <description>No GUID and no link: dropped.</description>
    </item>
  </channel>
</rss>`

const atomFixture = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Example Journal</title>
  <entry>
    <id>urn:journal:alpha</id>
    <title>Alpha entry</title>
    <link rel="self" href="https://journal.example/self/alpha"/>
    <link rel="alternate" href="https://journal.example/alpha"/>
    <summary>The alpha summary.</summary>
    <published>2026-08-04T06:30:00Z</published>
  </entry>
  <entry>
    <id>urn:journal:beta</id>
    <title>Beta entry</title>
    <link href="https://journal.example/beta"/>
    <content>Beta has content, no summary.</content>
    <updated>2026-08-03T12:00:00Z</updated>
  </entry>
</feed>`

// -------------------------------------------------------------------- parse

func TestParseRSSNormalizesEveryField(t *testing.T) {
	feed, err := Parse("https://wire.example/rss", []byte(rssFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Title != "Example Wire" {
		t.Errorf("feed title = %q", feed.Title)
	}
	if len(feed.Items) != 2 {
		t.Fatalf("items = %d, want 2 (the identityless entry dropped)", len(feed.Items))
	}
	first := feed.Items[0]
	if first.Feed != "https://wire.example/rss" || first.GUID != "wire-guid-1" {
		t.Errorf("identity = %s|%s", first.Feed, first.GUID)
	}
	if first.Title != "First story" {
		t.Errorf("title = %q, want whitespace collapsed", first.Title)
	}
	if first.Summary != "What happened, in a sentence." || first.Link != "https://wire.example/one" {
		t.Errorf("summary/link = %q / %q", first.Summary, first.Link)
	}
	want := time.Date(2026, 8, 4, 8, 15, 0, 0, time.UTC)
	if !first.Published.Equal(want) {
		t.Errorf("published = %v, want %v", first.Published, want)
	}
	second := feed.Items[1]
	if second.GUID != "https://wire.example/two" {
		t.Errorf("GUID fallback = %q, want the link", second.GUID)
	}
	if got := second.Published.UTC(); !got.Equal(time.Date(2026, 8, 4, 4, 0, 0, 0, time.UTC)) {
		t.Errorf("single-digit-day pubDate = %v", got)
	}
}

func TestParseAtomNormalizesEveryField(t *testing.T) {
	feed, err := Parse("https://journal.example/atom", []byte(atomFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Title != "Example Journal" {
		t.Errorf("feed title = %q", feed.Title)
	}
	if len(feed.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(feed.Items))
	}
	alpha := feed.Items[0]
	if alpha.GUID != "urn:journal:alpha" {
		t.Errorf("GUID = %q", alpha.GUID)
	}
	if alpha.Link != "https://journal.example/alpha" {
		t.Errorf("link = %q, want the alternate, not the self link", alpha.Link)
	}
	if alpha.Summary != "The alpha summary." {
		t.Errorf("summary = %q", alpha.Summary)
	}
	if !alpha.Published.Equal(time.Date(2026, 8, 4, 6, 30, 0, 0, time.UTC)) {
		t.Errorf("published = %v", alpha.Published)
	}
	beta := feed.Items[1]
	if beta.Summary != "Beta has content, no summary." {
		t.Errorf("content fallback = %q", beta.Summary)
	}
	if !beta.Published.Equal(time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("updated fallback = %v", beta.Published)
	}
}

func TestParseRejectsWhatItCannotRead(t *testing.T) {
	if _, err := Parse("u", []byte("<rss><channel><item></rss>")); err == nil {
		t.Error("malformed XML parsed without error")
	}
	if _, err := Parse("u", []byte("<html><body>a page</body></html>")); err == nil {
		t.Error("an HTML page parsed as a feed")
	}
	if _, err := Parse("u", []byte("")); err == nil {
		t.Error("an empty body parsed as a feed")
	}
}

// TestParseRefusesDoctypeEntities pins the stdlib property this package
// leans on: encoding/xml never reads a DTD, so a custom entity — the XXE
// vector — is an unresolvable reference and the document is an error, not a
// feed with the entity expanded.
func TestParseRefusesDoctypeEntities(t *testing.T) {
	const internal = `<?xml version="1.0"?>
<!DOCTYPE rss [<!ENTITY sneak "expanded">]>
<rss version="2.0"><channel><title>&sneak;</title><item><guid>g</guid><title>&sneak;</title></item></channel></rss>`
	if _, err := Parse("u", []byte(internal)); err == nil {
		t.Error("a DOCTYPE-declared entity resolved instead of erroring")
	}
	const external = `<?xml version="1.0"?>
<!DOCTYPE rss [<!ENTITY sneak SYSTEM "file:///etc/hostname">]>
<rss version="2.0"><channel><title>&sneak;</title></channel></rss>`
	if _, err := Parse("u", []byte(external)); err == nil {
		t.Error("an external entity resolved instead of erroring")
	}
}

// ------------------------------------------------------------------- dedupe

func memCache(t *testing.T) *gowild_data.Cache {
	t.Helper()
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("open in-memory database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := gowild_data.AddAllTables(db); err != nil {
		t.Fatalf("create tables: %v", err)
	}
	return gowild_data.NewCache(db)
}

// server serves a mutable feed body, so a test can append an entry between
// fetches the way a live feed would.
type server struct {
	body string
	hits int
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.hits++
	w.Header().Set("Content-Type", "application/rss+xml")
	_, _ = w.Write([]byte(s.body))
}

func TestFetchEmitsOnlyWhatIsNew(t *testing.T) {
	srv := &server{body: rssFixture}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	f := &Fetcher{Cache: memCache(t)}
	ctx := context.Background()

	first, err := f.Fetch(ctx, ts.URL)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if len(first.Items) != 2 {
		t.Fatalf("first fetch emitted %d, want 2", len(first.Items))
	}

	// An identical refetch emits nothing: the TTL markers hold.
	second, err := f.Fetch(ctx, ts.URL)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if len(second.Items) != 0 {
		t.Errorf("an unchanged feed re-emitted %d items", len(second.Items))
	}

	// One appended entry is exactly what the next fetch emits.
	srv.body = appendItem(rssFixture, `<item><title>Third story</title><guid>wire-guid-3</guid></item>`)
	third, err := f.Fetch(ctx, ts.URL)
	if err != nil {
		t.Fatalf("third fetch: %v", err)
	}
	if len(third.Items) != 1 || third.Items[0].GUID != "wire-guid-3" {
		t.Errorf("appended entry: emitted %+v", third.Items)
	}
}

func TestExpiredMarkersReadmitAnItem(t *testing.T) {
	srv := &server{body: rssFixture}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	f := &Fetcher{Cache: memCache(t), TTL: 30 * time.Millisecond}
	ctx := context.Background()

	if _, err := f.Fetch(ctx, ts.URL); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	again, err := f.Fetch(ctx, ts.URL)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	// Past the TTL the marker is gone and the items come round again — which
	// is why a consumer keeps its own (feed, GUID)-unique store.
	if len(again.Items) != 2 {
		t.Errorf("expired markers re-emitted %d items, want 2", len(again.Items))
	}
}

func TestFetchWithoutACacheEmitsEverything(t *testing.T) {
	srv := &server{body: rssFixture}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	f := &Fetcher{}
	for range 2 {
		feed, err := f.Fetch(context.Background(), ts.URL)
		if err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if len(feed.Items) != 2 {
			t.Errorf("cacheless fetch emitted %d, want 2", len(feed.Items))
		}
	}
}

func TestProbeNeverMarks(t *testing.T) {
	srv := &server{body: rssFixture}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	f := &Fetcher{Cache: memCache(t)}
	ctx := context.Background()

	probed, err := f.Probe(ctx, ts.URL)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if len(probed.Items) != 2 || probed.Title != "Example Wire" {
		t.Errorf("probe = %q / %d items", probed.Title, len(probed.Items))
	}
	// The probe left no markers: a following fetch still emits everything.
	fetched, err := f.Fetch(ctx, ts.URL)
	if err != nil {
		t.Fatalf("fetch after probe: %v", err)
	}
	if len(fetched.Items) != 2 {
		t.Errorf("a probe consumed the first fetch's items: %d emitted", len(fetched.Items))
	}
}

func TestFetchReportsHTTPFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(ts.Close)
	f := &Fetcher{}
	if _, err := f.Fetch(context.Background(), ts.URL); err == nil {
		t.Error("an HTTP 500 fetch did not error")
	}
}

func appendItem(doc, item string) string {
	const closer = "</channel>"
	return replaceLast(doc, closer, item+closer)
}

func replaceLast(s, old, new string) string {
	i := len(s) - len(old)
	for ; i >= 0; i-- {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	return s
}
