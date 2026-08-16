package feeds

import (
	"testing"
)

// ----------------------------------------------------------------- fixtures

// podcastRSSFixture exercises every podcast field in one RSS document: both
// podcastindex namespace spellings, itunes artwork at channel and item level,
// the RSS <image> fallback losing to itunes:image, an itunes:title that must
// not clobber the plain title, and the three duration forms plus a garbled
// one across the items.
const podcastRSSFixture = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"
     xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"
     xmlns:podcast="https://podcastindex.org/namespace/1.0"
     xmlns:oldpodcast="https://github.com/Podcastindex-org/podcast-namespace/blob/main/docs/1.0.md">
  <channel>
    <title>Example Show</title>
    <description>A show about examples.</description>
    <itunes:image href="https://show.example/cover.jpg"/>
    <image><url>https://show.example/rss-image.png</url></image>
    <item>
      <title>Episode One</title>
      <itunes:title>1: Episode One</itunes:title>
      <link>https://show.example/1</link>
      <guid>show-ep-1</guid>
      <description>The first episode.</description>
      <pubDate>Tue, 04 Aug 2026 08:15:00 +0000</pubDate>
      <enclosure url="https://cdn.example/ep1.mp3" type="audio/mpeg" length="52428800"/>
      <itunes:duration>1:02:03</itunes:duration>
      <itunes:image href="https://show.example/ep1.jpg"/>
      <podcast:chapters url="https://show.example/ep1.chapters.json" type="application/json+chapters"/>
    </item>
    <item>
      <title>Episode Two</title>
      <guid>show-ep-2</guid>
      <enclosure url="https://cdn.example/ep2.mp3" type="audio/mpeg" length="not-a-number"/>
      <itunes:duration>45:10</itunes:duration>
      <oldpodcast:chapters url="https://show.example/ep2.chapters.json" type="application/json+chapters"/>
    </item>
    <item>
      <title>Episode Three</title>
      <guid>show-ep-3</guid>
      <itunes:duration>1800</itunes:duration>
    </item>
    <item>
      <title>Episode Four</title>
      <guid>show-ep-4</guid>
      <itunes:duration>about an hour</itunes:duration>
    </item>
  </channel>
</rss>`

// podcastRSSTLSFixture pins the https spelling of the itunes namespace, which
// real feeds do publish.
const podcastRSSTLSFixture = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:itunes="https://www.itunes.com/dtds/podcast-1.0.dtd">
  <channel>
    <title>Secure Show</title>
    <itunes:image href="https://secure.example/cover.jpg"/>
    <item>
      <guid>secure-ep-1</guid>
      <title>Only episode</title>
      <itunes:duration>90</itunes:duration>
      <itunes:image href="https://secure.example/ep.jpg"/>
    </item>
  </channel>
</rss>`

// podcastRSSFallbackFixture has no itunes artwork, so the channel image URL
// carries the feed artwork.
const podcastRSSFallbackFixture = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Plain Show</title>
    <image><url>https://plain.example/logo.png</url></image>
    <item><guid>plain-1</guid><title>An episode</title></item>
  </channel>
</rss>`

// podcastAtomFixture is the Atom shape of the same ground: subtitle as the
// description, itunes:image beating <logo>, the enclosure riding a
// rel="enclosure" link, and the podcast fields on the entry.
const podcastAtomFixture = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"
      xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"
      xmlns:podcast="https://podcastindex.org/namespace/1.0">
  <title>Atom Show</title>
  <subtitle>An Atom-syndicated show.</subtitle>
  <logo>https://atom.example/logo.png</logo>
  <itunes:image href="https://atom.example/cover.jpg"/>
  <entry>
    <id>urn:atomshow:1</id>
    <title>Atom episode</title>
    <link rel="alternate" href="https://atom.example/1"/>
    <link rel="enclosure" href="https://cdn.example/atom1.mp3" type="audio/mpeg" length="1048576"/>
    <summary>The Atom episode.</summary>
    <published>2026-08-04T06:30:00Z</published>
    <itunes:duration>02:30</itunes:duration>
    <itunes:image href="https://atom.example/1.jpg"/>
    <podcast:chapters url="https://atom.example/1.chapters.json" type="application/json+chapters"/>
  </entry>
</feed>`

// podcastAtomLogoFixture has no itunes artwork, so <logo> carries it.
const podcastAtomLogoFixture = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Logo Show</title>
  <logo>https://logo.example/logo.png</logo>
  <entry><id>urn:logoshow:1</id><title>An entry</title></entry>
</feed>`

// -------------------------------------------------------------------- tests

func TestParseRSSPodcastFields(t *testing.T) {
	feed, err := Parse("https://show.example/rss", []byte(podcastRSSFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Description != "A show about examples." {
		t.Errorf("feed description = %q", feed.Description)
	}
	if feed.ArtworkURL != "https://show.example/cover.jpg" {
		t.Errorf("feed artwork = %q, want the itunes:image over the RSS <image>", feed.ArtworkURL)
	}
	if len(feed.Items) != 4 {
		t.Fatalf("items = %d, want 4", len(feed.Items))
	}

	one := feed.Items[0]
	if one.Title != "Episode One" {
		t.Errorf("title = %q, want the plain <title>, not itunes:title", one.Title)
	}
	if one.EnclosureURL != "https://cdn.example/ep1.mp3" || one.EnclosureType != "audio/mpeg" {
		t.Errorf("enclosure = %q / %q", one.EnclosureURL, one.EnclosureType)
	}
	if one.EnclosureBytes != 52428800 {
		t.Errorf("enclosure bytes = %d", one.EnclosureBytes)
	}
	if one.DurationSeconds != 3723 {
		t.Errorf("HH:MM:SS duration = %d, want 3723", one.DurationSeconds)
	}
	if one.ArtworkURL != "https://show.example/ep1.jpg" {
		t.Errorf("item artwork = %q", one.ArtworkURL)
	}
	if one.ChaptersURL != "https://show.example/ep1.chapters.json" || one.ChaptersType != "application/json+chapters" {
		t.Errorf("chapters = %q / %q", one.ChaptersURL, one.ChaptersType)
	}

	two := feed.Items[1]
	if two.DurationSeconds != 2710 {
		t.Errorf("MM:SS duration = %d, want 2710", two.DurationSeconds)
	}
	if two.EnclosureBytes != 0 {
		t.Errorf("junk enclosure length = %d, want 0", two.EnclosureBytes)
	}
	if two.ChaptersURL != "https://show.example/ep2.chapters.json" {
		t.Errorf("github-namespace chapters = %q", two.ChaptersURL)
	}
	if two.ArtworkURL != "" {
		t.Errorf("artworkless item = %q, want empty for the consumer to fall back on", two.ArtworkURL)
	}

	if got := feed.Items[2].DurationSeconds; got != 1800 {
		t.Errorf("bare-seconds duration = %d, want 1800", got)
	}
	if got := feed.Items[3].DurationSeconds; got != 0 {
		t.Errorf("garbled duration = %d, want 0", got)
	}
}

func TestParseRSSAcceptsTLSITunesNamespace(t *testing.T) {
	feed, err := Parse("https://secure.example/rss", []byte(podcastRSSTLSFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.ArtworkURL != "https://secure.example/cover.jpg" {
		t.Errorf("feed artwork = %q", feed.ArtworkURL)
	}
	ep := feed.Items[0]
	if ep.DurationSeconds != 90 || ep.ArtworkURL != "https://secure.example/ep.jpg" {
		t.Errorf("https-namespaced item = %d s / %q", ep.DurationSeconds, ep.ArtworkURL)
	}
}

func TestParseRSSArtworkFallsBackToChannelImage(t *testing.T) {
	feed, err := Parse("https://plain.example/rss", []byte(podcastRSSFallbackFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.ArtworkURL != "https://plain.example/logo.png" {
		t.Errorf("feed artwork = %q, want the <image><url> fallback", feed.ArtworkURL)
	}
}

func TestParseAtomPodcastFields(t *testing.T) {
	feed, err := Parse("https://atom.example/feed", []byte(podcastAtomFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Description != "An Atom-syndicated show." {
		t.Errorf("feed description = %q", feed.Description)
	}
	if feed.ArtworkURL != "https://atom.example/cover.jpg" {
		t.Errorf("feed artwork = %q, want itunes:image over <logo>", feed.ArtworkURL)
	}
	if len(feed.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(feed.Items))
	}
	ep := feed.Items[0]
	if ep.Link != "https://atom.example/1" {
		t.Errorf("link = %q, want the alternate, not the enclosure", ep.Link)
	}
	if ep.EnclosureURL != "https://cdn.example/atom1.mp3" || ep.EnclosureType != "audio/mpeg" || ep.EnclosureBytes != 1048576 {
		t.Errorf("enclosure = %q / %q / %d", ep.EnclosureURL, ep.EnclosureType, ep.EnclosureBytes)
	}
	if ep.DurationSeconds != 150 {
		t.Errorf("MM:SS duration = %d, want 150", ep.DurationSeconds)
	}
	if ep.ArtworkURL != "https://atom.example/1.jpg" {
		t.Errorf("item artwork = %q", ep.ArtworkURL)
	}
	if ep.ChaptersURL != "https://atom.example/1.chapters.json" || ep.ChaptersType != "application/json+chapters" {
		t.Errorf("chapters = %q / %q", ep.ChaptersURL, ep.ChaptersType)
	}
}

func TestParseAtomArtworkFallsBackToLogo(t *testing.T) {
	feed, err := Parse("https://logo.example/feed", []byte(podcastAtomLogoFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.ArtworkURL != "https://logo.example/logo.png" {
		t.Errorf("feed artwork = %q, want the <logo> fallback", feed.ArtworkURL)
	}
}

func TestParseDurationForms(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"1800", 1800},       // bare seconds
		{"0", 0},             //
		{"02:30", 150},       // MM:SS
		{"90:00", 5400},      // minutes past an hour are still minutes
		{"1:02:03", 3723},    // HH:MM:SS
		{" 10:00 ", 600},     // padded
		{"", 0},              // missing
		{"about an hour", 0}, // words
		{"-30", 0},           // negative
		{"1:2:3:4", 0},       // too many parts
		{"10:75", 0},         // 75 seconds is not a clock component
		{"1:99:00", 0},       //
		{"12.5", 0},          // fractional is not one of the three forms
	}
	for _, c := range cases {
		if got := parseDuration(c.raw); got != c.want {
			t.Errorf("parseDuration(%q) = %d, want %d", c.raw, got, c.want)
		}
	}
}
