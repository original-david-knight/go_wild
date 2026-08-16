// Package feeds is a reusable RSS 2.0 / Atom feed fetcher: fetch a feed URL,
// parse it, and emit entries normalized to one shape — feed URL, GUID (or
// link), title, summary, published time, and, where the feed carries them,
// the podcast fields: enclosure, duration, artwork, chapters.
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
	"strconv"
	"strings"
	"time"
)

// The namespaces podcast feeds actually declare. Struct tags must carry the
// URIs literally, so these constants exist for the record: the itunes DTD in
// both scheme spellings, and the podcastindex namespace in its published form
// and the older GitHub-document form early feeds still use.
const (
	nsITunes    = "http://www.itunes.com/dtds/podcast-1.0.dtd"
	nsITunesTLS = "https://www.itunes.com/dtds/podcast-1.0.dtd"
	nsPodcast   = "https://podcastindex.org/namespace/1.0"
	nsPodcastGH = "https://github.com/Podcastindex-org/podcast-namespace/blob/main/docs/1.0.md"
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
	// EnclosureURL is the attached media file — for a podcast, the audio.
	EnclosureURL string `json:"enclosure_url"`
	// EnclosureType is the enclosure's declared MIME type.
	EnclosureType string `json:"enclosure_type"`
	// EnclosureBytes is the enclosure's declared size; 0 when absent or junk.
	EnclosureBytes int64 `json:"enclosure_bytes"`
	// DurationSeconds is itunes:duration normalized to seconds — bare
	// seconds, MM:SS, or HH:MM:SS; anything else is 0.
	DurationSeconds int `json:"duration_seconds"`
	// ArtworkURL is the entry's own itunes:image; empty means the consumer
	// falls back to the feed's.
	ArtworkURL string `json:"artwork_url"`
	// ChaptersURL and ChaptersType are the podcast:chapters document and its
	// declared MIME type.
	ChaptersURL  string `json:"chapters_url"`
	ChaptersType string `json:"chapters_type"`
}

// Feed is one parsed document: the feed's own title and its entries.
type Feed struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// ArtworkURL is the channel itunes:image, else the channel image URL
	// (RSS <image><url>, Atom <logo>).
	ArtworkURL string `json:"artwork_url"`
	Items      []Item `json:"items"`
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

// itunesImage is the itunes:image element: artwork as an href attribute.
type itunesImage struct {
	Href string `xml:"href,attr"`
}

// podcastChapters is podcast:chapters: an external chapters document URL and
// its MIME type.
type podcastChapters struct {
	URL  string `xml:"url,attr"`
	Type string `xml:"type,attr"`
}

// enclosure is the RSS <enclosure> element. Length stays a string until
// parseBytes reads it, so a junk length degrades to 0 instead of failing the
// whole document.
type enclosure struct {
	URL    string `xml:"url,attr"`
	Type   string `xml:"type,attr"`
	Length string `xml:"length,attr"`
}

// Field-order note, load-bearing throughout the document structs below: the
// xml decoder gives a child element to the FIRST field whose name matches, and
// a tag without a namespace matches that local name in ANY namespace. So every
// namespaced field (itunes:title, itunes:image, …) is declared before the
// plain field sharing its local name — the namespaced ones absorb their own
// elements, and the plain field is left holding only the un-namespaced one
// instead of whichever appeared last in the document.

type rssDocument struct {
	Channel struct {
		Title          string      `xml:"title"`
		Description    string      `xml:"description"`
		ITunesImage    itunesImage `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd image"`
		ITunesImageTLS itunesImage `xml:"https://www.itunes.com/dtds/podcast-1.0.dtd image"`
		Image          struct {
			URL string `xml:"url"`
		} `xml:"image"`
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	ITunesTitle    string          `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd title"`
	ITunesTitleTLS string          `xml:"https://www.itunes.com/dtds/podcast-1.0.dtd title"`
	Title          string          `xml:"title"`
	Link           string          `xml:"link"`
	GUID           string          `xml:"guid"`
	Description    string          `xml:"description"`
	PubDate        string          `xml:"pubDate"`
	Enclosure      enclosure       `xml:"enclosure"`
	Duration       string          `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd duration"`
	DurationTLS    string          `xml:"https://www.itunes.com/dtds/podcast-1.0.dtd duration"`
	ITunesImage    itunesImage     `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd image"`
	ITunesImageTLS itunesImage     `xml:"https://www.itunes.com/dtds/podcast-1.0.dtd image"`
	Chapters       podcastChapters `xml:"https://podcastindex.org/namespace/1.0 chapters"`
	ChaptersGH     podcastChapters `xml:"https://github.com/Podcastindex-org/podcast-namespace/blob/main/docs/1.0.md chapters"`
}

func parseRSS(feedURL string, body []byte) (*Feed, error) {
	var doc rssDocument
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("feed %s: malformed RSS: %w", feedURL, err)
	}
	ch := doc.Channel
	out := &Feed{
		URL:         feedURL,
		Title:       clean(ch.Title),
		Description: clean(ch.Description),
		ArtworkURL:  strings.TrimSpace(firstNonEmpty(ch.ITunesImage.Href, ch.ITunesImageTLS.Href, ch.Image.URL)),
	}
	for _, entry := range ch.Items {
		item := Item{
			Feed:            feedURL,
			GUID:            strings.TrimSpace(entry.GUID),
			Link:            strings.TrimSpace(entry.Link),
			Title:           clean(firstNonEmpty(entry.Title, entry.ITunesTitle, entry.ITunesTitleTLS)),
			Summary:         clean(entry.Description),
			Published:       parseTime(entry.PubDate),
			EnclosureURL:    strings.TrimSpace(entry.Enclosure.URL),
			EnclosureType:   strings.TrimSpace(entry.Enclosure.Type),
			EnclosureBytes:  parseBytes(entry.Enclosure.Length),
			DurationSeconds: parseDuration(firstNonEmpty(entry.Duration, entry.DurationTLS)),
			ArtworkURL:      strings.TrimSpace(firstNonEmpty(entry.ITunesImage.Href, entry.ITunesImageTLS.Href)),
		}
		setChapters(&item, entry.Chapters, entry.ChaptersGH)
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
	Title          string      `xml:"title"`
	Subtitle       string      `xml:"subtitle"`
	ITunesImage    itunesImage `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd image"`
	ITunesImageTLS itunesImage `xml:"https://www.itunes.com/dtds/podcast-1.0.dtd image"`
	Logo           string      `xml:"logo"`
	Entries        []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID               string          `xml:"id"`
	ITunesTitle      string          `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd title"`
	ITunesTitleTLS   string          `xml:"https://www.itunes.com/dtds/podcast-1.0.dtd title"`
	Title            string          `xml:"title"`
	Links            []atomLinkElem  `xml:"link"`
	ITunesSummary    string          `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd summary"`
	ITunesSummaryTLS string          `xml:"https://www.itunes.com/dtds/podcast-1.0.dtd summary"`
	Summary          string          `xml:"summary"`
	Content          string          `xml:"content"`
	Published        string          `xml:"published"`
	Updated          string          `xml:"updated"`
	Duration         string          `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd duration"`
	DurationTLS      string          `xml:"https://www.itunes.com/dtds/podcast-1.0.dtd duration"`
	ITunesImage      itunesImage     `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd image"`
	ITunesImageTLS   itunesImage     `xml:"https://www.itunes.com/dtds/podcast-1.0.dtd image"`
	Chapters         podcastChapters `xml:"https://podcastindex.org/namespace/1.0 chapters"`
	ChaptersGH       podcastChapters `xml:"https://github.com/Podcastindex-org/podcast-namespace/blob/main/docs/1.0.md chapters"`
}

// atomLinkElem is one Atom <link>, which doubles as the enclosure carrier
// (rel="enclosure" with href/type/length).
type atomLinkElem struct {
	Rel    string `xml:"rel,attr"`
	Href   string `xml:"href,attr"`
	Type   string `xml:"type,attr"`
	Length string `xml:"length,attr"`
}

func parseAtom(feedURL string, body []byte) (*Feed, error) {
	var doc atomDocument
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("feed %s: malformed Atom: %w", feedURL, err)
	}
	out := &Feed{
		URL:         feedURL,
		Title:       clean(doc.Title),
		Description: clean(doc.Subtitle),
		ArtworkURL:  strings.TrimSpace(firstNonEmpty(doc.ITunesImage.Href, doc.ITunesImageTLS.Href, doc.Logo)),
	}
	for _, entry := range doc.Entries {
		summary := firstNonEmpty(entry.Summary, entry.Content, entry.ITunesSummary, entry.ITunesSummaryTLS)
		when := entry.Published
		if when == "" {
			when = entry.Updated
		}
		item := Item{
			Feed:            feedURL,
			GUID:            strings.TrimSpace(entry.ID),
			Link:            atomLink(entry.Links),
			Title:           clean(firstNonEmpty(entry.Title, entry.ITunesTitle, entry.ITunesTitleTLS)),
			Summary:         clean(summary),
			Published:       parseTime(when),
			DurationSeconds: parseDuration(firstNonEmpty(entry.Duration, entry.DurationTLS)),
			ArtworkURL:      strings.TrimSpace(firstNonEmpty(entry.ITunesImage.Href, entry.ITunesImageTLS.Href)),
		}
		if enc := atomEnclosure(entry.Links); enc != nil {
			item.EnclosureURL = strings.TrimSpace(enc.Href)
			item.EnclosureType = strings.TrimSpace(enc.Type)
			item.EnclosureBytes = parseBytes(enc.Length)
		}
		setChapters(&item, entry.Chapters, entry.ChaptersGH)
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
func atomLink(links []atomLinkElem) string {
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

// atomEnclosure finds the entry's rel="enclosure" link, if any.
func atomEnclosure(links []atomLinkElem) *atomLinkElem {
	for i := range links {
		if links[i].Rel == "enclosure" {
			return &links[i]
		}
	}
	return nil
}

// setChapters takes whichever podcast:chapters namespace variant carried a
// URL; a URL-less chapters element is nothing to store.
func setChapters(item *Item, candidates ...podcastChapters) {
	for _, c := range candidates {
		if strings.TrimSpace(c.URL) != "" {
			item.ChaptersURL = strings.TrimSpace(c.URL)
			item.ChaptersType = strings.TrimSpace(c.Type)
			return
		}
	}
}

// firstNonEmpty returns the first candidate with any non-space content —
// how the namespace-variant fields collapse back to one value.
func firstNonEmpty(candidates ...string) string {
	for _, c := range candidates {
		if strings.TrimSpace(c) != "" {
			return c
		}
	}
	return ""
}

// parseDuration reads itunes:duration in its three published forms — bare
// seconds, MM:SS, HH:MM:SS. Anything else is 0, not an error: a missing or
// garbled duration does not make the episode worthless.
func parseDuration(raw string) int {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	numbers := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0
		}
		numbers[i] = n
	}
	switch len(numbers) {
	case 1:
		return numbers[0]
	case 2: // MM:SS — minutes unbounded, seconds a real clock component
		if numbers[1] > 59 {
			return 0
		}
		return numbers[0]*60 + numbers[1]
	case 3: // HH:MM:SS
		if numbers[1] > 59 || numbers[2] > 59 {
			return 0
		}
		return numbers[0]*3600 + numbers[1]*60 + numbers[2]
	}
	return 0
}

// parseBytes reads an enclosure length attribute; absent or junk is 0.
func parseBytes(raw string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
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
