package gowild_ytmusic

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// libraryPodcastsBrowseID is the library page listing the account's podcast
// subscriptions (ytmusicapi get_library_podcasts).
const libraryPodcastsBrowseID = "FEmusic_library_non_music_audio_list"

// podcastBrowsePrefix marks a podcast show page browseId. A show's playlistId
// is the browseId with this prefix stripped; both forms circulate.
const podcastBrowsePrefix = "MPSP"

// LibraryPodcasts lists the account's podcast subscriptions. Synthetic
// library tiles — "Add podcast", the "New episodes" auto-playlist — are
// dropped: they carry no MPSP show page. Returned IDs keep their MPSP prefix
// and feed PodcastEpisodes directly. An account with no subscriptions yields
// an empty list.
func (c *Client) LibraryPodcasts(ctx context.Context) ([]Podcast, error) {
	resp, err := c.browse(ctx, map[string]any{"browseId": libraryPodcastsBrowseID})
	if err != nil {
		return nil, err
	}
	column, ok := navMap(resp, "contents", "singleColumnBrowseResultsRenderer")
	if !ok {
		return nil, fmt.Errorf("ytmusic: library podcasts: response has no contents.singleColumnBrowseResultsRenderer; the library page shape changed")
	}
	grid, ok := findPodcastLibraryGrid(column)
	if !ok {
		// A library without podcast subscriptions renders no grid at all;
		// the column being present but gridless is the empty state.
		return nil, nil
	}
	items, _ := navSlice(grid, "items")
	podcasts := parsePodcastGridItems(items)

	prev := ""
	for node := grid; ; {
		token, ok := podcastContinuationToken(node)
		if !ok {
			break
		}
		if token == prev {
			return nil, fmt.Errorf("ytmusic: library podcasts: continuation token did not advance; aborting pagination")
		}
		prev = token
		cresp, err := c.browse(ctx, map[string]any{"continuation": token})
		if err != nil {
			return nil, err
		}
		node, ok = navMap(cresp, "continuationContents", "gridContinuation")
		if !ok {
			return nil, fmt.Errorf("ytmusic: library podcasts: continuation response has no continuationContents.gridContinuation")
		}
		more, _ := navSlice(node, "items")
		if len(more) == 0 {
			break
		}
		podcasts = append(podcasts, parsePodcastGridItems(more)...)
	}
	return podcasts, nil
}

// PodcastEpisodes fetches a podcast show page — header metadata plus every
// episode, following continuations until exhausted. podcastID may be the
// MPSP-prefixed browseId LibraryPodcasts returns or the bare playlistId; the
// returned Podcast.ID always carries the prefix.
func (c *Client) PodcastEpisodes(ctx context.Context, podcastID string) (Podcast, []Episode, error) {
	if strings.TrimSpace(podcastID) == "" {
		return Podcast{}, nil, fmt.Errorf("ytmusic: podcast episodes: empty podcast ID")
	}
	browseID := podcastID
	if !strings.HasPrefix(browseID, podcastBrowsePrefix) {
		browseID = podcastBrowsePrefix + browseID
	}
	resp, err := c.browse(ctx, map[string]any{"browseId": browseID})
	if err != nil {
		return Podcast{}, nil, err
	}
	twoCol, ok := navMap(resp, "contents", "twoColumnBrowseResultsRenderer")
	if !ok {
		return Podcast{}, nil, fmt.Errorf("ytmusic: podcast episodes: %s: response has no contents.twoColumnBrowseResultsRenderer; wrong podcast ID or the show page shape changed", browseID)
	}
	header, ok := navMap(twoCol, "tabs", 0, "tabRenderer", "content", "sectionListRenderer", "contents", 0, "musicResponsiveHeaderRenderer")
	if !ok {
		return Podcast{}, nil, fmt.Errorf("ytmusic: podcast episodes: %s: show page has no musicResponsiveHeaderRenderer", browseID)
	}
	podcast := Podcast{ID: browseID}
	podcast.Title, ok = navString(header, "title", "runs", 0, "text")
	if !ok {
		return Podcast{}, nil, fmt.Errorf("ytmusic: podcast episodes: %s: show header has no title", browseID)
	}
	podcast.Author, _ = navString(header, "straplineTextOne", "runs", 0, "text")
	podcast.ThumbnailURL = podcastThumbURL(header, "thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails")

	shelf, ok := navMap(twoCol, "secondaryContents", "sectionListRenderer", "contents", 0, "musicShelfRenderer")
	if !ok {
		return Podcast{}, nil, fmt.Errorf("ytmusic: podcast episodes: %s: show page has no episode musicShelfRenderer", browseID)
	}
	contents, _ := navSlice(shelf, "contents")
	episodes := parsePodcastEpisodeItems(contents)

	prev := ""
	for node := shelf; ; {
		token, ok := podcastContinuationToken(node)
		if !ok {
			break
		}
		if token == prev {
			return Podcast{}, nil, fmt.Errorf("ytmusic: podcast episodes: %s: continuation token did not advance; aborting pagination", browseID)
		}
		prev = token
		cresp, err := c.browse(ctx, map[string]any{"continuation": token})
		if err != nil {
			return Podcast{}, nil, err
		}
		node, ok = navMap(cresp, "continuationContents", "musicShelfContinuation")
		if !ok {
			return Podcast{}, nil, fmt.Errorf("ytmusic: podcast episodes: %s: continuation response has no continuationContents.musicShelfContinuation", browseID)
		}
		more, _ := navSlice(node, "contents")
		parsed := parsePodcastEpisodeItems(more)
		if len(parsed) == 0 {
			break
		}
		episodes = append(episodes, parsed...)
	}
	return podcast, episodes, nil
}

// findPodcastLibraryGrid locates the podcast gridRenderer in a library
// response: under some tab's sectionListRenderer contents, occasionally
// wrapped in an itemSectionRenderer.
func findPodcastLibraryGrid(column map[string]any) (map[string]any, bool) {
	tabs, _ := navSlice(column, "tabs")
	for _, tab := range tabs {
		sections, ok := navSlice(tab, "tabRenderer", "content", "sectionListRenderer", "contents")
		if !ok {
			continue
		}
		for _, section := range sections {
			if wrapped, ok := navMap(section, "itemSectionRenderer", "contents", 0); ok {
				section = wrapped
			}
			if grid, ok := navMap(section, "gridRenderer"); ok {
				return grid, true
			}
		}
	}
	return nil, false
}

func parsePodcastGridItems(items []any) []Podcast {
	podcasts := make([]Podcast, 0, len(items))
	for _, item := range items {
		if p, ok := parsePodcastGridItem(item); ok {
			podcasts = append(podcasts, p)
		}
	}
	return podcasts
}

func parsePodcastGridItem(item any) (Podcast, bool) {
	r, ok := navMap(item, "musicTwoRowItemRenderer")
	if !ok {
		return Podcast{}, false
	}
	id, ok := navString(r, "title", "runs", 0, "navigationEndpoint", "browseEndpoint", "browseId")
	if !ok || !strings.HasPrefix(id, podcastBrowsePrefix) {
		// Synthetic tiles ("Add podcast", the "New episodes" auto-playlist)
		// navigate elsewhere than an MPSP show page.
		return Podcast{}, false
	}
	p := Podcast{ID: id}
	p.Title, _ = navString(r, "title", "runs", 0, "text")
	p.Author, _ = navString(r, "subtitle", "runs", 0, "text")
	p.ThumbnailURL = podcastThumbURL(r, "thumbnailRenderer", "musicThumbnailRenderer", "thumbnail", "thumbnails")
	return p, true
}

func parsePodcastEpisodeItems(items []any) []Episode {
	episodes := make([]Episode, 0, len(items))
	for _, item := range items {
		if e, ok := parsePodcastEpisodeItem(item); ok {
			episodes = append(episodes, e)
		}
	}
	return episodes
}

func parsePodcastEpisodeItem(item any) (Episode, bool) {
	r, ok := navMap(item, "musicMultiRowListItemRenderer")
	if !ok {
		return Episode{}, false
	}
	var e Episode
	e.VideoID, ok = navString(r, "onTap", "watchEndpoint", "videoId")
	if !ok {
		// Some items surface the watch endpoint only on the thumbnail play
		// button.
		e.VideoID, ok = navString(r, "overlay", "musicItemThumbnailOverlayRenderer", "content", "musicPlayButtonRenderer", "playNavigationEndpoint", "watchEndpoint", "videoId")
	}
	if !ok || e.VideoID == "" {
		return Episode{}, false
	}
	e.Title, _ = navString(r, "title", "runs", 0, "text")
	if runs, ok := navSlice(r, "description", "runs"); ok {
		var b strings.Builder
		for _, run := range runs {
			s, _ := navString(run, "text")
			b.WriteString(s)
		}
		e.Description = b.String()
	}
	// Subtitle reads "<views> • <date>" (the views run is absent on some
	// shows); the display date is always the final run.
	if runs, ok := navSlice(r, "subtitle", "runs"); ok && len(runs) > 0 {
		e.Published, _ = navString(runs[len(runs)-1], "text")
	}
	if runs, ok := navSlice(r, "playbackProgress", "musicPlaybackProgressRenderer", "durationText", "runs"); ok && len(runs) > 0 {
		// runs are ["<separator>", "<duration>"]; the duration is last.
		text, _ := navString(runs[len(runs)-1], "text")
		e.DurationSec = parsePodcastDurationText(text)
	}
	e.ThumbnailURL = podcastThumbURL(r, "thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails")
	return e, true
}

// podcastContinuationToken pulls the next-page token from a shelf or grid
// node. InnerTube accepts it back as a {"continuation": token} browse body.
func podcastContinuationToken(node map[string]any) (string, bool) {
	token, ok := navString(node, "continuations", 0, "nextContinuationData", "continuation")
	return token, ok && token != ""
}

// podcastThumbURL returns the URL of the largest thumbnail — InnerTube orders
// them ascending — under path, or "" when absent.
func podcastThumbURL(v any, path ...any) string {
	thumbs, ok := navSlice(v, path...)
	if !ok || len(thumbs) == 0 {
		return ""
	}
	url, _ := navString(thumbs[len(thumbs)-1], "url")
	return url
}

// parsePodcastDurationText converts the worded duration display podcast pages
// use — "1 hr 12 min", "23 min", "45 sec" — to whole seconds, falling back to
// the colon form ("1:02:11") music durations use. Unreadable text is 0, not
// an error: a missing duration does not make the episode itself worthless.
func parsePodcastDurationText(text string) int {
	text = strings.TrimSpace(text)
	if strings.Contains(text, ":") {
		return parseDurationText(text)
	}
	total := 0
	pending := -1
	matched := false
	for _, field := range strings.Fields(text) {
		if n, err := strconv.Atoi(field); err == nil {
			if pending >= 0 {
				return 0
			}
			pending = n
			continue
		}
		unit := 0
		switch strings.TrimSuffix(field, "s") {
		case "hr", "hour":
			unit = 3600
		case "min", "minute":
			unit = 60
		case "sec", "second":
			unit = 1
		default:
			return 0
		}
		if pending < 0 {
			return 0
		}
		total += pending * unit
		pending = -1
		matched = true
	}
	if pending >= 0 || !matched {
		return 0
	}
	return total
}
