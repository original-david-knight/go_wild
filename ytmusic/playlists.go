package gowild_ytmusic

import (
	"context"
	"fmt"
	"strings"
)

// LibraryPlaylists lists the playlists in the account's library. The library
// grid's synthetic "New playlist" tile carries no browse target and is
// skipped; every real entry becomes a Playlist with the "VL" browse prefix
// stripped from its ID.
func (c *Client) LibraryPlaylists(ctx context.Context) ([]Playlist, error) {
	resp, err := c.browse(ctx, map[string]any{"browseId": "FEmusic_liked_playlists"})
	if err != nil {
		return nil, err
	}
	grid, ok := libraryGrid(resp)
	if !ok {
		return nil, fmt.Errorf("ytmusic: library playlists: no playlist grid in the FEmusic_liked_playlists response; the library layout may have changed")
	}
	items, _ := navSlice(grid, "items")

	var playlists []Playlist
	err = c.followContinuations(ctx, items, grid, func(item map[string]any) {
		renderer, ok := navMap(item, "musicTwoRowItemRenderer")
		if !ok {
			return
		}
		if p, ok := parseGridPlaylist(renderer); ok {
			playlists = append(playlists, p)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("ytmusic: library playlists: %w", err)
	}
	return playlists, nil
}

// PlaylistTracks fetches one playlist's metadata and its complete track list,
// following continuations until the server stops returning them. playlistID
// is accepted with or without the "VL" browse prefix.
func (c *Client) PlaylistTracks(ctx context.Context, playlistID string) (Playlist, []Track, error) {
	if playlistID == "" {
		return Playlist{}, nil, fmt.Errorf("ytmusic: playlist tracks: empty playlist ID")
	}
	browseID := playlistID
	if !strings.HasPrefix(browseID, "VL") {
		browseID = "VL" + browseID
	}
	resp, err := c.browse(ctx, map[string]any{"browseId": browseID})
	if err != nil {
		return Playlist{}, nil, err
	}

	header, ok := playlistHeader(resp)
	if !ok {
		return Playlist{}, nil, fmt.Errorf("ytmusic: playlist %s: response has no recognizable header (neither musicResponsiveHeaderRenderer nor musicDetailHeaderRenderer)", playlistID)
	}
	playlist := parsePlaylistHeader(header)
	playlist.ID = strings.TrimPrefix(browseID, "VL")

	shelf, ok := playlistShelf(resp)
	if !ok {
		return Playlist{}, nil, fmt.Errorf("ytmusic: playlist %s: response has no track shelf (neither musicPlaylistShelfRenderer nor musicShelfRenderer)", playlistID)
	}
	items, _ := navSlice(shelf, "contents")

	var tracks []Track
	err = c.followContinuations(ctx, items, shelf, func(item map[string]any) {
		renderer, ok := navMap(item, "musicResponsiveListItemRenderer")
		if !ok {
			return
		}
		if t, ok := parseShelfTrack(renderer); ok {
			tracks = append(tracks, t)
		}
	})
	if err != nil {
		return Playlist{}, nil, fmt.Errorf("ytmusic: playlist %s: %w", playlistID, err)
	}
	// The header count is display text and can be absent (e.g. "Your Likes");
	// the assembled list is then the authoritative count.
	if playlist.TrackCount == 0 {
		playlist.TrackCount = len(tracks)
	}
	return playlist, tracks, nil
}

// followContinuations feeds every item of every page to visit. The current
// protocol appends a continuationItemRenderer as the last item of a page; its
// token is resent as {"continuation": token} and the next page arrives under
// onResponseReceivedActions. renderer is the containing shelf/grid, checked
// for the legacy pre-2024 "continuations" key: that scheme paginates via URL
// query parameters this client does not speak, and truncating silently would
// hide it.
func (c *Client) followContinuations(ctx context.Context, items []any, renderer map[string]any, visit func(map[string]any)) error {
	if _, legacy := navSlice(renderer, "continuations"); legacy {
		if _, ok := continuationToken(items); !ok {
			return fmt.Errorf("response paginates via legacy nextContinuationData query parameters, which this client does not support")
		}
	}
	for page := 1; ; page++ {
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				visit(m)
			}
		}
		token, ok := continuationToken(items)
		if !ok {
			return nil
		}
		resp, err := c.browse(ctx, map[string]any{"continuation": token})
		if err != nil {
			return fmt.Errorf("continuation page %d: %w", page+1, err)
		}
		items, ok = navSlice(resp, "onResponseReceivedActions", 0, "appendContinuationItemsAction", "continuationItems")
		if !ok {
			return fmt.Errorf("continuation page %d: response carries no appendContinuationItemsAction items", page+1)
		}
	}
}

// continuationToken pulls the next-page token from a page's trailing
// continuationItemRenderer. The token sits either directly under
// continuationEndpoint.continuationCommand or nested in a
// commandExecutorCommand list alongside unrelated commands.
func continuationToken(items []any) (string, bool) {
	if len(items) == 0 {
		return "", false
	}
	last := items[len(items)-1]
	endpoint, ok := navMap(last, "continuationItemRenderer", "continuationEndpoint")
	if !ok {
		return "", false
	}
	if token, ok := navString(endpoint, "continuationCommand", "token"); ok && token != "" {
		return token, true
	}
	commands, _ := navSlice(endpoint, "commandExecutorCommand", "commands")
	for _, command := range commands {
		if request, _ := navString(command, "continuationCommand", "request"); request != "CONTINUATION_REQUEST_TYPE_BROWSE" {
			continue
		}
		if token, ok := navString(command, "continuationCommand", "token"); ok && token != "" {
			return token, true
		}
	}
	return "", false
}

// libraryGrid locates the gridRenderer holding the library's playlist tiles.
// The section list entry wraps it in an itemSectionRenderer in current
// responses and holds it directly in older ones; non-premium accounts also
// differ in tab count, so every tab is scanned.
func libraryGrid(resp map[string]any) (map[string]any, bool) {
	tabs, ok := navSlice(resp, "contents", "singleColumnBrowseResultsRenderer", "tabs")
	if !ok {
		return nil, false
	}
	for _, tab := range tabs {
		sections, ok := navSlice(tab, "tabRenderer", "content", "sectionListRenderer", "contents")
		if !ok {
			continue
		}
		for _, section := range sections {
			if grid, ok := navMap(section, "gridRenderer"); ok {
				return grid, true
			}
			inner, _ := navSlice(section, "itemSectionRenderer", "contents")
			for _, entry := range inner {
				if grid, ok := navMap(entry, "gridRenderer"); ok {
					return grid, true
				}
			}
		}
	}
	return nil, false
}

// parseGridPlaylist reads one musicTwoRowItemRenderer library tile. A tile
// without a browse target is not a playlist (the "New playlist" button) and
// reports false.
func parseGridPlaylist(renderer map[string]any) (Playlist, bool) {
	browseID, ok := navString(renderer, "title", "runs", 0, "navigationEndpoint", "browseEndpoint", "browseId")
	if !ok || browseID == "" {
		browseID, ok = navString(renderer, "navigationEndpoint", "browseEndpoint", "browseId")
		if !ok || browseID == "" {
			return Playlist{}, false
		}
	}
	title, _ := navString(renderer, "title", "runs", 0, "text")
	subtitleRuns, _ := navSlice(renderer, "subtitle", "runs")
	return Playlist{
		ID:           strings.TrimPrefix(browseID, "VL"),
		Title:        title,
		TrackCount:   trackCountFromRuns(subtitleRuns),
		ThumbnailURL: largestThumbnail(renderer, "thumbnailRenderer", "musicThumbnailRenderer", "thumbnail", "thumbnails"),
	}, true
}

// playlistHeader finds the header renderer in either response generation:
// current two-column pages carry a musicResponsiveHeaderRenderer as the first
// tab section, older pages a top-level header.musicDetailHeaderRenderer. Owned
// playlists wrap either in a musicEditablePlaylistDetailHeaderRenderer.
func playlistHeader(resp map[string]any) (map[string]any, bool) {
	var nodes []map[string]any
	if node, ok := navMap(resp, "contents", "twoColumnBrowseResultsRenderer", "tabs", 0, "tabRenderer", "content", "sectionListRenderer", "contents", 0); ok {
		nodes = append(nodes, node)
	}
	if node, ok := navMap(resp, "header"); ok {
		nodes = append(nodes, node)
	}
	for _, node := range nodes {
		if inner, ok := navMap(node, "musicEditablePlaylistDetailHeaderRenderer", "header"); ok {
			node = inner
		}
		if header, ok := navMap(node, "musicResponsiveHeaderRenderer"); ok {
			return header, true
		}
		if header, ok := navMap(node, "musicDetailHeaderRenderer"); ok {
			return header, true
		}
	}
	return nil, false
}

// parsePlaylistHeader reads title, track count and cover art. The caller owns
// the ID (it is the browse target, not header content).
func parsePlaylistHeader(header map[string]any) Playlist {
	title, _ := navString(header, "title", "runs", 0, "text")
	// secondSubtitle runs like ["3.6M views", " • ", "1,554 tracks", " • ",
	// "112+ hours"]; the count run's position varies with whether views are
	// shown, so every run is tried.
	runs, _ := navSlice(header, "secondSubtitle", "runs")
	thumb := largestThumbnail(header, "thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails")
	if thumb == "" {
		// musicDetailHeaderRenderer crops its cover art.
		thumb = largestThumbnail(header, "thumbnail", "croppedSquareThumbnailRenderer", "thumbnail", "thumbnails")
	}
	return Playlist{
		Title:        title,
		TrackCount:   trackCountFromRuns(runs),
		ThumbnailURL: thumb,
	}
}

// playlistShelf finds the renderer holding the track rows: under
// secondaryContents on current two-column pages, under the first tab on older
// single-column ones; either generation may name it musicPlaylistShelfRenderer
// or musicShelfRenderer.
func playlistShelf(resp map[string]any) (map[string]any, bool) {
	for _, sectionsPath := range [][]any{
		{"contents", "twoColumnBrowseResultsRenderer", "secondaryContents", "sectionListRenderer", "contents"},
		{"contents", "singleColumnBrowseResultsRenderer", "tabs", 0, "tabRenderer", "content", "sectionListRenderer", "contents"},
	} {
		sections, ok := navSlice(resp, sectionsPath...)
		if !ok {
			continue
		}
		for _, section := range sections {
			if shelf, ok := navMap(section, "musicPlaylistShelfRenderer"); ok {
				return shelf, true
			}
			if shelf, ok := navMap(section, "musicShelfRenderer"); ok {
				return shelf, true
			}
		}
	}
	return nil, false
}

// parseShelfTrack reads one musicResponsiveListItemRenderer track row. A row
// without a playlistItemData.videoId has nothing to play and reports false.
func parseShelfTrack(renderer map[string]any) (Track, bool) {
	videoID, ok := navString(renderer, "playlistItemData", "videoId")
	if !ok || videoID == "" {
		return Track{}, false
	}
	return Track{
		VideoID:      videoID,
		Title:        flexColumnText(renderer, 0),
		Artist:       flexColumnText(renderer, 1),
		Album:        flexColumnText(renderer, 2),
		DurationSec:  parseDurationText(fixedColumnText(renderer, 0)),
		ThumbnailURL: largestThumbnail(renderer, "thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails"),
	}, true
}

// flexColumnText joins every run of flex column i: artist columns emit each
// artist and the separators between them (", ", " & ") as separate runs.
func flexColumnText(renderer map[string]any, i int) string {
	runs, ok := navSlice(renderer, "flexColumns", i, "musicResponsiveListItemFlexColumnRenderer", "text", "runs")
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, run := range runs {
		text, _ := navString(run, "text")
		b.WriteString(text)
	}
	return b.String()
}

// fixedColumnText reads fixed column i, which encodes its text as either
// simpleText or a single run depending on response vintage.
func fixedColumnText(renderer map[string]any, i int) string {
	if s, ok := navString(renderer, "fixedColumns", i, "musicResponsiveListItemFixedColumnRenderer", "text", "simpleText"); ok {
		return s
	}
	s, _ := navString(renderer, "fixedColumns", i, "musicResponsiveListItemFixedColumnRenderer", "text", "runs", 0, "text")
	return s
}

// largestThumbnail returns the URL of the last entry of a thumbnails array,
// which InnerTube orders smallest to largest.
func largestThumbnail(v any, path ...any) string {
	thumbs, ok := navSlice(v, path...)
	if !ok || len(thumbs) == 0 {
		return ""
	}
	url, _ := navString(thumbs[len(thumbs)-1], "url")
	return url
}

// trackCountFromRuns scans display runs for a "<n> songs" / "<n> tracks"
// entry and parses the comma-grouped count. 0 means no run carried a count —
// not an error, some playlists (e.g. auto playlists) simply do not show one.
func trackCountFromRuns(runs []any) int {
	for _, run := range runs {
		text, ok := navString(run, "text")
		if !ok {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) < 2 {
			continue
		}
		switch strings.ToLower(fields[1]) {
		case "song", "songs", "track", "tracks":
		default:
			continue
		}
		n := 0
		valid := false
		for _, r := range strings.ReplaceAll(fields[0], ",", "") {
			if r < '0' || r > '9' {
				valid = false
				break
			}
			n = n*10 + int(r-'0')
			valid = true
		}
		if valid {
			return n
		}
	}
	return 0
}
