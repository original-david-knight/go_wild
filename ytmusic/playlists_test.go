package gowild_ytmusic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(raw)
}

// fixtureClient serves one canned response body per browse call, in order,
// capturing each decoded request body.
func fixtureClient(t *testing.T, bodies *[]map[string]any, responses ...string) *Client {
	t.Helper()
	calls := 0
	return stubClient(t, &Credentials{Cookie: testCookie}, func(req *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("request body is not JSON: %v", err)
		}
		*bodies = append(*bodies, body)
		if calls >= len(responses) {
			t.Fatalf("unexpected browse call %d; only %d responses stubbed", calls+1, len(responses))
		}
		resp := jsonResponse(200, responses[calls])
		calls++
		return resp, nil
	})
}

func TestLibraryPlaylists(t *testing.T) {
	var bodies []map[string]any
	c := fixtureClient(t, &bodies, fixture(t, "library_playlists.json"))

	got, err := c.LibraryPlaylists(context.Background())
	if err != nil {
		t.Fatalf("LibraryPlaylists: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("browse calls = %d; want 1", len(bodies))
	}
	if id, _ := navString(bodies[0], "browseId"); id != "FEmusic_liked_playlists" {
		t.Errorf("browseId = %q; want FEmusic_liked_playlists", id)
	}

	// The grid's first tile is the synthetic "New playlist" button (no browse
	// target): skipped, not parsed into a bogus entry.
	want := []Playlist{
		{
			ID:           "LM",
			Title:        "Your Likes",
			TrackCount:   0,
			ThumbnailURL: "https://www.gstatic.com/youtube/media/ytm/images/pbg/liked-music-@576.png",
		},
		{
			ID:           "PLQwVIlKxHM6rz0fDJVv_0UlXGEWf-bFys",
			Title:        "Road Trip Mix",
			TrackCount:   42,
			ThumbnailURL: "https://lh3.googleusercontent.com/road-trip-mix-cover=w544-h544-l90-rj",
		},
		{
			ID:           "PL6bPxvf5dW5clc3y9wAoslzqUrmkZ5c-u",
			Title:        "Everything Ever",
			TrackCount:   1554,
			ThumbnailURL: "https://lh3.googleusercontent.com/everything-ever-cover=w544-h544-l90-rj",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LibraryPlaylists =\n%+v\nwant\n%+v", got, want)
	}
}

func TestLibraryPlaylistsUnrecognizedShape(t *testing.T) {
	var bodies []map[string]any
	c := fixtureClient(t, &bodies, `{"contents": {}}`)
	_, err := c.LibraryPlaylists(context.Background())
	if err == nil || !strings.Contains(err.Error(), "grid") {
		t.Fatalf("err = %v; want a descriptive no-grid error", err)
	}
}

// The token inside testdata/playlist_tracks.json, captured live 2026-08-12.
const fixtureContinuationToken = "4qmFsgKHARIkVkxQTDZiUHh2ZjVkVzVjbGMzeTl3QW9zbHpxVXJta1o1Yy11GjplaDVRVkRwRFIzZHBSVVJDUWsxcVp6UlNhMVpHVVZSU1JGSkVZelJPUkVHU0FRTUl1Z1R3QVFBJTNEmgIiUEw2YlB4dmY1ZFc1Y2xjM3k5d0Fvc2x6cVVybWtaNWMtdQ%3D%3D"

func TestPlaylistTracksTwoPages(t *testing.T) {
	var bodies []map[string]any
	c := fixtureClient(t, &bodies,
		fixture(t, "playlist_tracks.json"),
		fixture(t, "playlist_tracks_continuation.json"))

	playlist, tracks, err := c.PlaylistTracks(context.Background(), "PL6bPxvf5dW5clc3y9wAoslzqUrmkZ5c-u")
	if err != nil {
		t.Fatalf("PlaylistTracks: %v", err)
	}

	if len(bodies) != 2 {
		t.Fatalf("browse calls = %d; want 2 (initial page + one continuation)", len(bodies))
	}
	if id, _ := navString(bodies[0], "browseId"); id != "VLPL6bPxvf5dW5clc3y9wAoslzqUrmkZ5c-u" {
		t.Errorf("first browseId = %q; want the VL-prefixed playlist ID", id)
	}
	if token, _ := navString(bodies[1], "continuation"); token != fixtureContinuationToken {
		t.Errorf("continuation token = %q; want the fixture's token", token)
	}
	if _, hasBrowseID := bodies[1]["browseId"]; hasBrowseID {
		t.Error("continuation request must not carry a browseId")
	}

	wantPlaylist := Playlist{
		ID:           "PL6bPxvf5dW5clc3y9wAoslzqUrmkZ5c-u",
		Title:        "Top 1000 - Best Hits ever! 90s 80s 00s 90 80 2000",
		TrackCount:   1554, // "1,554 tracks" run, after a "3.6M views" run
		ThumbnailURL: "https://i.ytimg.com/vi/EPhWR4d3FJQ/hq720.jpg?sqp=-oaymwEKCNUGEN8DIABIWg&rs=AMzJL3l81CMEvWKYHylljsg3Mi_Gy_Qr9A",
	}
	if !reflect.DeepEqual(playlist, wantPlaylist) {
		t.Errorf("playlist =\n%+v\nwant\n%+v", playlist, wantPlaylist)
	}

	if len(tracks) != 7 {
		t.Fatalf("len(tracks) = %d; want 7 (4 from page one + 3 from the continuation)", len(tracks))
	}
	wantFirst := Track{
		VideoID:      "EPhWR4d3FJQ",
		Title:        "Born in the U.S.A. (Live)",
		Artist:       "Bruce Springsteen",
		Album:        "", // video playlist rows carry an empty album column
		DurationSec:  284,
		ThumbnailURL: "https://i.ytimg.com/vi/EPhWR4d3FJQ/hqdefault.jpg?sqp=-oaymwEWCJADEOEBIAQqCggAEOADGC0guwJIWg&rs=AMzJL3mGS7HCTeCB_BUNL0DRXQAG8n3RDQ",
		VideoType:    VideoTypeOMV, // the captured row carries it on the title run's watchEndpoint
	}
	if !reflect.DeepEqual(tracks[0], wantFirst) {
		t.Errorf("tracks[0] =\n%+v\nwant\n%+v", tracks[0], wantFirst)
	}
	if tracks[0].IsPrivatelyOwned() {
		t.Error("tracks[0].IsPrivatelyOwned() = true; an OMV row is not privately owned")
	}
	// Multi-artist rows split names and separators into individual runs.
	if got := tracks[3].Artist; got != "Eurythmics, Annie Lennox & Dave Stewart" {
		t.Errorf("tracks[3].Artist = %q; want joined multi-run artist", got)
	}
	// First track of the continuation page follows the last of page one.
	if tracks[4].VideoID != "yyDUC1LUXSU" || tracks[4].Title != "Blurred Lines (feat. Pharrell & T.I.)" || tracks[4].DurationSec != 272 {
		t.Errorf("tracks[4] = %+v; want first continuation-page track", tracks[4])
	}
	if tracks[6].VideoID != "K8TRRof5V0o" {
		t.Errorf("tracks[6].VideoID = %q; want last continuation-page track", tracks[6].VideoID)
	}
	if tracks[6].VideoType != VideoTypeUGC {
		t.Errorf("tracks[6].VideoType = %q; want the fixture's genuine MUSIC_VIDEO_TYPE_UGC", tracks[6].VideoType)
	}
}

func TestPlaylistTracksKeepsExistingVLPrefix(t *testing.T) {
	var bodies []map[string]any
	c := fixtureClient(t, &bodies,
		fixture(t, "playlist_tracks.json"),
		fixture(t, "playlist_tracks_continuation.json"))

	playlist, _, err := c.PlaylistTracks(context.Background(), "VLPL6bPxvf5dW5clc3y9wAoslzqUrmkZ5c-u")
	if err != nil {
		t.Fatalf("PlaylistTracks: %v", err)
	}
	if id, _ := navString(bodies[0], "browseId"); id != "VLPL6bPxvf5dW5clc3y9wAoslzqUrmkZ5c-u" {
		t.Errorf("browseId = %q; VL prefix must not be doubled", id)
	}
	if playlist.ID != "PL6bPxvf5dW5clc3y9wAoslzqUrmkZ5c-u" {
		t.Errorf("playlist.ID = %q; want the bare playlist ID", playlist.ID)
	}
}

// The pre-2024 response generation: top-level musicDetailHeaderRenderer with
// cropped cover art, single-column musicShelfRenderer tracks, simpleText
// durations, and a populated album column. musicVideoType placement covers
// both real locations: track one carries it on the thumbnail overlay's play
// button, track two only on the title run's watchEndpoint, track three not at
// all (a greyed-out unavailable row has no watch endpoint anywhere).
const detailHeaderPlaylist = `{
  "header": {
    "musicDetailHeaderRenderer": {
      "title": {"runs": [{"text": "Old Shape Mix"}]},
      "secondSubtitle": {"runs": [{"text": "3 songs"}, {"text": " • "}, {"text": "11 minutes"}]},
      "thumbnail": {
        "croppedSquareThumbnailRenderer": {
          "thumbnail": {"thumbnails": [
            {"url": "https://lh3.googleusercontent.com/old-mix=w226-h226-l90-rj", "width": 226, "height": 226},
            {"url": "https://lh3.googleusercontent.com/old-mix=w544-h544-l90-rj", "width": 544, "height": 544}
          ]}
        }
      }
    }
  },
  "contents": {
    "singleColumnBrowseResultsRenderer": {
      "tabs": [{"tabRenderer": {"content": {"sectionListRenderer": {"contents": [{
        "musicShelfRenderer": {
          "contents": [
            {"musicResponsiveListItemRenderer": {
              "thumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [
                {"url": "https://lh3.googleusercontent.com/track-one=w60-h60-l90-rj", "width": 60, "height": 60},
                {"url": "https://lh3.googleusercontent.com/track-one=w120-h120-l90-rj", "width": 120, "height": 120}
              ]}}},
              "overlay": {"musicItemThumbnailOverlayRenderer": {"content": {"musicPlayButtonRenderer": {
                "playNavigationEndpoint": {"watchEndpoint": {
                  "videoId": "dQw4w9WgXcQ",
                  "watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_ATV"}}
                }}
              }}}},
              "playlistItemData": {"playlistSetVideoId": "0AB1", "videoId": "dQw4w9WgXcQ"},
              "flexColumns": [
                {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Never Gonna Give You Up"}]}}},
                {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Rick Astley"}]}}},
                {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Whenever You Need Somebody"}]}}}
              ],
              "fixedColumns": [
                {"musicResponsiveListItemFixedColumnRenderer": {"text": {"simpleText": "3:33"}}}
              ]
            }},
            {"musicResponsiveListItemRenderer": {
              "flexColumns": [
                {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Row without playlistItemData is skipped"}]}}}
              ]
            }},
            {"musicResponsiveListItemRenderer": {
              "playlistItemData": {"videoId": "oHg5SJYRHA0"},
              "flexColumns": [
                {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{
                  "text": "Second Song",
                  "navigationEndpoint": {"watchEndpoint": {
                    "videoId": "oHg5SJYRHA0",
                    "watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_PRIVATELY_OWNED_TRACK"}}
                  }}
                }]}}},
                {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Somebody"}]}}},
                {"musicResponsiveListItemFlexColumnRenderer": {"text": {}}}
              ],
              "fixedColumns": [
                {"musicResponsiveListItemFixedColumnRenderer": {"text": {"runs": [{"text": "4:41"}]}}}
              ]
            }},
            {"musicResponsiveListItemRenderer": {
              "playlistItemData": {"videoId": "xvFZjo5PgG0"},
              "flexColumns": [
                {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Unavailable Song"}]}}},
                {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Nobody"}]}}}
              ],
              "fixedColumns": [
                {"musicResponsiveListItemFixedColumnRenderer": {"text": {"simpleText": "2:57"}}}
              ]
            }}
          ]
        }
      }]}}}}]
    }
  }
}`

func TestPlaylistTracksDetailHeaderShape(t *testing.T) {
	var bodies []map[string]any
	c := fixtureClient(t, &bodies, detailHeaderPlaylist)

	playlist, tracks, err := c.PlaylistTracks(context.Background(), "PLoldshape")
	if err != nil {
		t.Fatalf("PlaylistTracks: %v", err)
	}
	wantPlaylist := Playlist{
		ID:           "PLoldshape",
		Title:        "Old Shape Mix",
		TrackCount:   3,
		ThumbnailURL: "https://lh3.googleusercontent.com/old-mix=w544-h544-l90-rj",
	}
	if !reflect.DeepEqual(playlist, wantPlaylist) {
		t.Errorf("playlist =\n%+v\nwant\n%+v", playlist, wantPlaylist)
	}
	wantTracks := []Track{
		{
			VideoID:      "dQw4w9WgXcQ",
			Title:        "Never Gonna Give You Up",
			Artist:       "Rick Astley",
			Album:        "Whenever You Need Somebody",
			DurationSec:  213,
			ThumbnailURL: "https://lh3.googleusercontent.com/track-one=w120-h120-l90-rj",
			VideoType:    VideoTypeATV, // read from the thumbnail overlay's play button
		},
		{
			VideoID:     "oHg5SJYRHA0",
			Title:       "Second Song",
			Artist:      "Somebody",
			DurationSec: 281,
			VideoType:   VideoTypePrivatelyOwned, // overlay absent: read from the title run's watchEndpoint
		},
		// No watch endpoint anywhere: VideoType stays "", no error.
		{VideoID: "xvFZjo5PgG0", Title: "Unavailable Song", Artist: "Nobody", DurationSec: 177},
	}
	if !reflect.DeepEqual(tracks, wantTracks) {
		t.Errorf("tracks =\n%+v\nwant\n%+v", tracks, wantTracks)
	}
	for i, want := range []bool{false, true, false} {
		if got := tracks[i].IsPrivatelyOwned(); got != want {
			t.Errorf("tracks[%d].IsPrivatelyOwned() = %v; want %v", i, got, want)
		}
	}
}

func TestPlaylistTracksRejectsEmptyID(t *testing.T) {
	c := stubClient(t, &Credentials{Cookie: testCookie}, func(*http.Request) (*http.Response, error) {
		t.Fatal("no request expected for an empty playlist ID")
		return nil, nil
	})
	if _, _, err := c.PlaylistTracks(context.Background(), ""); err == nil {
		t.Fatal("PlaylistTracks(\"\") should error")
	}
}

func TestPlaylistTracksUnrecognizedShape(t *testing.T) {
	var bodies []map[string]any
	c := fixtureClient(t, &bodies, `{"contents": {}}`)
	_, _, err := c.PlaylistTracks(context.Background(), "PLx")
	if err == nil || !strings.Contains(err.Error(), "header") {
		t.Fatalf("err = %v; want a descriptive no-header error", err)
	}
}

func TestPlaylistTracksLegacyContinuationFailsLoudly(t *testing.T) {
	// A shelf paginating via the pre-2024 "continuations" key (query-parameter
	// ctoken scheme) must error rather than silently return a truncated list.
	body := `{
	  "header": {"musicDetailHeaderRenderer": {
	    "title": {"runs": [{"text": "Legacy"}]},
	    "secondSubtitle": {"runs": [{"text": "200 songs"}]}
	  }},
	  "contents": {"singleColumnBrowseResultsRenderer": {"tabs": [{"tabRenderer": {"content": {"sectionListRenderer": {"contents": [{
	    "musicShelfRenderer": {
	      "contents": [{"musicResponsiveListItemRenderer": {
	        "playlistItemData": {"videoId": "abc123XYZ_0"},
	        "flexColumns": [{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Only Track"}]}}}]
	      }}],
	      "continuations": [{"nextContinuationData": {"continuation": "legacy-ctoken"}}]
	    }
	  }]}}}}]}}
	}`
	var bodies []map[string]any
	c := fixtureClient(t, &bodies, body)
	_, _, err := c.PlaylistTracks(context.Background(), "PLlegacy")
	if err == nil || !strings.Contains(err.Error(), "legacy") {
		t.Fatalf("err = %v; want a legacy-continuation error", err)
	}
}
