package feeds

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

// DefaultTTL is how long an emitted item is remembered. It only has to
// outlive the item's stay in its feed — most feeds hold a day or two of
// entries — because a consumer storing items keyed by (feed, GUID) is the
// second line of defense once the marker expires.
const DefaultTTL = 48 * time.Hour

// maxBody bounds one feed read. A feed bigger than this is not a feed.
const maxBody = 8 << 20

// Fetcher fetches and parses feeds, remembering what it has already emitted.
type Fetcher struct {
	// Client is the HTTP client; nil takes a default with a 20s timeout.
	Client *http.Client
	// Cache is the TTL dedupe store (data.Cache over the consumer's
	// database). Nil disables dedupe: every fetch emits every item.
	Cache *gowild_data.Cache
	// TTL is the seen-marker lifetime; zero takes DefaultTTL.
	TTL time.Duration
}

// Fetch GETs one feed and returns it with only the items not seen within the
// TTL, marking what it emits. The next fetch of an unchanged feed emits none.
func (f *Fetcher) Fetch(ctx context.Context, feedURL string) (*Feed, error) {
	feed, err := f.Probe(ctx, feedURL)
	if err != nil {
		return nil, err
	}
	if f.Cache == nil {
		return feed, nil
	}
	fresh := make([]Item, 0, len(feed.Items))
	for _, item := range feed.Items {
		key := seenKey(item)
		if _, seen := f.Cache.Get(ctx, key); seen {
			continue
		}
		if err := f.Cache.Set(ctx, key, "1", f.ttl()); err != nil {
			// A marker that cannot be written must not suppress the item —
			// the consumer's own store is the second line of dedupe.
			return nil, fmt.Errorf("feed %s: seen-marker: %w", feedURL, err)
		}
		fresh = append(fresh, item)
	}
	feed.Items = fresh
	return feed, nil
}

// Probe GETs and parses a feed without touching the seen-markers — the
// validation a settings screen runs before saving a URL, and the read Fetch
// filters.
func (f *Fetcher) Probe(ctx context.Context, feedURL string) (*Feed, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("feed %s: %w", feedURL, err)
	}
	req.Header.Set("User-Agent", "gowild-feeds/1.0")
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml")

	resp, err := f.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("feed %s: %w", feedURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feed %s: HTTP %d", feedURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("feed %s: %w", feedURL, err)
	}
	return Parse(feedURL, body)
}

func (f *Fetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return defaultClient
}

func (f *Fetcher) ttl() time.Duration {
	if f.TTL > 0 {
		return f.TTL
	}
	return DefaultTTL
}

var defaultClient = &http.Client{Timeout: 20 * time.Second}

// seenKey is the cache key an emitted item is remembered under.
func seenKey(item Item) string {
	return "feeds:seen:" + item.Feed + "|" + item.GUID
}
