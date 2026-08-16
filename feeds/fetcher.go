package feeds

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

// DefaultTTL is how long an emitted item is remembered. It only has to
// outlive the item's stay in its feed — most feeds hold a day or two of
// entries — because a consumer storing items keyed by (feed, GUID) is the
// second line of defense once the marker expires.
const DefaultTTL = 48 * time.Hour

// maxBody bounds one feed read. A feed bigger than this is not a feed — the
// read looks one byte past the bound and an overrun is an explicit error,
// never a silent truncation handed to the parser as the whole document.
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
	// RejectPrivate hardens the fetch for URLs a user can type: the built
	// client dials through a hook that refuses any resolved address that is
	// not a public global-unicast IP — loopback, RFC1918 private, link-local,
	// unique-local, unspecified, multicast — in both address families. The
	// hook sees the address actually being connected to, per attempt, which
	// is what defeats DNS rebinding, and every redirect hop re-enters the
	// same dialer, so a redirect to a private address dies at dial time. The
	// URL scheme must be http or https at every hop. Ignored when Client is
	// set: a supplied client is trusted as configured.
	RejectPrivate bool
	// MaxRedirects bounds redirect hops for the built client. Zero means 5
	// when RejectPrivate is set, and the stdlib default otherwise.
	MaxRedirects int

	buildOnce sync.Once
	built     *http.Client
}

// Validators are the HTTP cache validators one fetch hands the next, stored
// by the consumer between polls.
type Validators struct {
	ETag         string `json:"etag"`
	LastModified string `json:"last_modified"`
}

func (v Validators) zero() bool { return v.ETag == "" && v.LastModified == "" }

// Fetch GETs one feed and returns it with only the items not seen within the
// TTL, marking what it emits. The next fetch of an unchanged feed emits none.
func (f *Fetcher) Fetch(ctx context.Context, feedURL string) (*Feed, error) {
	feed, err := f.Probe(ctx, feedURL)
	if err != nil {
		return nil, err
	}
	if err := f.filterSeen(ctx, feed); err != nil {
		return nil, err
	}
	return feed, nil
}

// FetchConditional is Fetch riding HTTP cache validators: If-None-Match and
// If-Modified-Since go out when the caller has them. A 304 answers
// (nil, the prior validators, true, nil) without touching seen-markers; a 200
// parses, dedupes exactly as Fetch does, and hands back the response's own
// validators for the next call.
func (f *Fetcher) FetchConditional(ctx context.Context, feedURL string, v Validators) (*Feed, Validators, bool, error) {
	feed, next, notModified, err := f.get(ctx, feedURL, v)
	if err != nil {
		return nil, v, false, err
	}
	if notModified {
		return nil, v, true, nil
	}
	if err := f.filterSeen(ctx, feed); err != nil {
		return nil, v, false, err
	}
	return feed, next, false, nil
}

// Probe GETs and parses a feed without touching the seen-markers — the
// validation a settings screen runs before saving a URL, and the read Fetch
// filters.
func (f *Fetcher) Probe(ctx context.Context, feedURL string) (*Feed, error) {
	feed, _, _, err := f.get(ctx, feedURL, Validators{})
	return feed, err
}

// get is the one GET everything above shares: conditional headers in,
// validators out, the body bound, the response parsed.
func (f *Fetcher) get(ctx context.Context, feedURL string, v Validators) (*Feed, Validators, bool, error) {
	none := Validators{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, none, false, fmt.Errorf("feed %s: %w", feedURL, err)
	}
	if f.RejectPrivate {
		if err := checkScheme(req.URL.Scheme); err != nil {
			return nil, none, false, fmt.Errorf("feed %s: %w", feedURL, err)
		}
	}
	req.Header.Set("User-Agent", "gowild-feeds/1.0")
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml")
	if v.ETag != "" {
		req.Header.Set("If-None-Match", v.ETag)
	}
	if v.LastModified != "" {
		req.Header.Set("If-Modified-Since", v.LastModified)
	}

	resp, err := f.client().Do(req)
	if err != nil {
		return nil, none, false, fmt.Errorf("feed %s: %w", feedURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && !v.zero() {
		return nil, v, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, none, false, fmt.Errorf("feed %s: HTTP %d", feedURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, none, false, fmt.Errorf("feed %s: %w", feedURL, err)
	}
	if len(body) > maxBody {
		return nil, none, false, fmt.Errorf("feed %s: the feed is larger than 8 MB", feedURL)
	}
	feed, err := Parse(feedURL, body)
	if err != nil {
		return nil, none, false, err
	}
	next := Validators{ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}
	return feed, next, false, nil
}

// filterSeen drops the items the cache remembers and marks the rest —
// the dedupe both Fetch and FetchConditional run on a 200.
func (f *Fetcher) filterSeen(ctx context.Context, feed *Feed) error {
	if f.Cache == nil {
		return nil
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
			return fmt.Errorf("feed %s: seen-marker: %w", feed.URL, err)
		}
		fresh = append(fresh, item)
	}
	feed.Items = fresh
	return nil
}

func (f *Fetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	if !f.RejectPrivate && f.MaxRedirects == 0 {
		return defaultClient
	}
	f.buildOnce.Do(func() { f.built = buildClient(f.RejectPrivate, f.MaxRedirects) })
	return f.built
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
