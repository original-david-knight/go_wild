// Package feeds is a reusable RSS 2.0 / Atom feed fetcher: fetch a feed URL,
// parse it, and emit entries normalized to one shape — feed URL, GUID (or
// link), title, summary, published time.
//
// It is the generalization of tools/reuters_fetch.go's fetch/parse + TTL-cache
// skeleton: the Reuters sitemap schema and article scraping are replaced by
// the two formats the open web actually syndicates in, and the same data.Cache
// TTL pattern dedupes items across polls, so each fetch returns only what is
// new. There is no scheduling here — a consumer's poller decides when to call.
package feeds

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"
)

// Item is one normalized feed entry.
type Item struct {
	// Feed is the feed URL the item came from — half of its identity.
	Feed string `json:"feed"`
	// GUID is the entry's own ID, falling back to its link: the other half of
	// the identity dedupe runs on.
	GUID      string    `json:"guid"`
	Link      string    `json:"link"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary"`
	Published time.Time `json:"published"`
}

// Feed is one parsed document: the feed's own title and its entries.
type Feed struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Items []Item `json:"items"`
}

// Parse reads an RSS 2.0 or Atom document. Malformed XML is an error, never a
// panic; an unrecognized root element is an error naming what was found.
func Parse(feedURL string, body []byte) (*Feed, error) {
	root, err := rootElement(body)
	if err != nil {
		return nil, fmt.Errorf("feed %s: %w", feedURL, err)
	}
	switch root {
	case "rss":
		return parseRSS(feedURL, body)
	case "feed":
		return parseAtom(feedURL, body)
	default:
		return nil, fmt.Errorf("feed %s: root element <%s> is neither RSS 2.0 (<rss>) nor Atom (<feed>)", feedURL, root)
	}
}

// rootElement finds the document's first start element, which is what
// distinguishes the two formats without parsing the whole document twice.
func rootElement(body []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return "", fmt.Errorf("no XML root element")
		}
		if err != nil {
			return "", fmt.Errorf("malformed XML: %w", err)
		}
		if start, ok := token.(xml.StartElement); ok {
			return start.Name.Local, nil
		}
	}
}

type rssDocument struct {
	Channel struct {
		Title string `xml:"title"`
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			GUID        string `xml:"guid"`
			Description string `xml:"description"`
			PubDate     string `xml:"pubDate"`
		} `xml:"item"`
	} `xml:"channel"`
}

func parseRSS(feedURL string, body []byte) (*Feed, error) {
	var doc rssDocument
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("feed %s: malformed RSS: %w", feedURL, err)
	}
	out := &Feed{URL: feedURL, Title: clean(doc.Channel.Title)}
	for _, entry := range doc.Channel.Items {
		item := Item{
			Feed:      feedURL,
			GUID:      strings.TrimSpace(entry.GUID),
			Link:      strings.TrimSpace(entry.Link),
			Title:     clean(entry.Title),
			Summary:   clean(entry.Description),
			Published: parseTime(entry.PubDate),
		}
		if item.GUID == "" {
			item.GUID = item.Link
		}
		// An entry with no GUID and no link has no identity to dedupe on and
		// no destination to open; it is dropped rather than guessed at.
		if item.GUID == "" {
			continue
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

type atomDocument struct {
	Title   string `xml:"title"`
	Entries []struct {
		ID    string `xml:"id"`
		Title string `xml:"title"`
		Links []struct {
			Rel  string `xml:"rel,attr"`
			Href string `xml:"href,attr"`
		} `xml:"link"`
		Summary   string `xml:"summary"`
		Content   string `xml:"content"`
		Published string `xml:"published"`
		Updated   string `xml:"updated"`
	} `xml:"entry"`
}

func parseAtom(feedURL string, body []byte) (*Feed, error) {
	var doc atomDocument
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("feed %s: malformed Atom: %w", feedURL, err)
	}
	out := &Feed{URL: feedURL, Title: clean(doc.Title)}
	for _, entry := range doc.Entries {
		summary := entry.Summary
		if summary == "" {
			summary = entry.Content
		}
		when := entry.Published
		if when == "" {
			when = entry.Updated
		}
		item := Item{
			Feed:      feedURL,
			GUID:      strings.TrimSpace(entry.ID),
			Link:      atomLink(entry.Links),
			Title:     clean(entry.Title),
			Summary:   clean(summary),
			Published: parseTime(when),
		}
		if item.GUID == "" {
			item.GUID = item.Link
		}
		if item.GUID == "" {
			continue
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// atomLink picks the entry's alternate link — the article — preferring an
// explicit rel="alternate" (or no rel, which Atom defines as alternate) over
// rel="self" and friends.
func atomLink(links []struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}) string {
	for _, l := range links {
		if l.Rel == "" || l.Rel == "alternate" {
			return strings.TrimSpace(l.Href)
		}
	}
	if len(links) > 0 {
		return strings.TrimSpace(links[0].Href)
	}
	return ""
}

// timeLayouts are the stamps feeds actually publish: RFC1123 variants for RSS
// pubDate, RFC3339 for Atom, plus the common single-digit-day slips.
var timeLayouts = []string{
	time.RFC1123Z,
	time.RFC1123,
	time.RFC822Z,
	time.RFC822,
	time.RFC3339,
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// parseTime reads a published stamp. An unreadable one is the zero time, not
// an error: a missing date does not make the entry itself worthless.
func parseTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// clean collapses whitespace in feed-supplied text.
func clean(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
