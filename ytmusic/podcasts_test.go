package gowild_ytmusic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// theDailyContToken is the real shelf continuation token captured inside
// testdata/podcast_episodes.json.
const theDailyContToken = "4qmFsgJmEiZNUFNQUExkTXJiZ1lmVmwtczE2RF9pVDJCSkNKOTBwV3RUTzFBNBo8ZWg1UVZEcERSMUZwUlVSb1IxSlVRa05PYTAweFRsVkZNbEZVWkVKT2FtZVNBUU1JMlFqYUNBUUlBaEFC"

func podcastFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(raw)
}

// podcastRequestBody decodes a stubbed request's JSON body so tests can route
// on browseId/continuation and assert the request shape.
func podcastRequestBody(t *testing.T, req *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	return body
}

func TestLibraryPodcasts(t *testing.T) {
	var bodies []map[string]any
	c := stubClient(t, &Credentials{Cookie: testCookie}, func(req *http.Request) (*http.Response, error) {
		body := podcastRequestBody(t, req)
		bodies = append(bodies, body)
		if id, _ := navString(body, "browseId"); id == libraryPodcastsBrowseID {
			return jsonResponse(200, podcastFixture(t, "library_podcasts.json")), nil
		}
		if token, _ := navString(body, "continuation"); token == "LIBRARY_PODCASTS_CONT_TOKEN_1" {
			return jsonResponse(200, podcastFixture(t, "library_podcasts_continuation.json")), nil
		}
		t.Errorf("unexpected browse body: %v", body)
		return jsonResponse(200, `{}`), nil
	})

	podcasts, err := c.LibraryPodcasts(context.Background())
	if err != nil {
		t.Fatalf("LibraryPodcasts: %v", err)
	}
	// The fixture grid holds 4 tiles + 1 via continuation; "Add podcast" and
	// the "New episodes" auto-playlist have no MPSP browseId and must be
	// dropped.
	wantIDs := []string{
		"MPSPPLdMrbgYfVl-s16D_iT2BJCJ90pWtTO1A4",
		"MPSPPL4n3AtGdRxJgywUZQYUk3k2-HXIQ_fIHE",
		"MPSPPLq_yj5y9oa26LpUYyN6WrwkiUJawcYKC4",
	}
	if len(podcasts) != len(wantIDs) {
		t.Fatalf("got %d podcasts (%+v); want %d", len(podcasts), podcasts, len(wantIDs))
	}
	for i, want := range wantIDs {
		if podcasts[i].ID != want {
			t.Errorf("podcast[%d].ID = %q; want %q", i, podcasts[i].ID, want)
		}
	}
	first := podcasts[0]
	if first.Title != "The Daily" || first.Author != "New York Times Podcasts" {
		t.Errorf("podcast[0] = %+v; want The Daily / New York Times Podcasts", first)
	}
	if !strings.HasSuffix(first.ThumbnailURL, "large.jpg") {
		t.Errorf("podcast[0].ThumbnailURL = %q; want the largest (last) thumbnail", first.ThumbnailURL)
	}
	if podcasts[2].Title != "This American Life" {
		t.Errorf("continuation podcast = %+v; want This American Life", podcasts[2])
	}

	if len(bodies) != 2 {
		t.Fatalf("browse called %d times; want 2", len(bodies))
	}
	if _, ok := bodies[1]["browseId"]; ok {
		t.Errorf("continuation request must not repeat browseId: %v", bodies[1])
	}
}

func TestLibraryPodcastsEmptyLibrary(t *testing.T) {
	// A subscription-less library renders the column without a gridRenderer.
	body := `{"contents": {"singleColumnBrowseResultsRenderer": {"tabs": [
		{"tabRenderer": {"content": {"sectionListRenderer": {"contents": [
			{"musicShelfRenderer": {"contents": []}}]}}}}]}}}`
	c := stubClient(t, &Credentials{Cookie: testCookie}, func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, body), nil
	})
	podcasts, err := c.LibraryPodcasts(context.Background())
	if err != nil {
		t.Fatalf("LibraryPodcasts on empty library: %v", err)
	}
	if len(podcasts) != 0 {
		t.Errorf("got %+v; want none", podcasts)
	}
}

func TestLibraryPodcastsUnexpectedShape(t *testing.T) {
	c := stubClient(t, &Credentials{Cookie: testCookie}, func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"contents": {}}`), nil
	})
	_, err := c.LibraryPodcasts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "singleColumnBrowseResultsRenderer") {
		t.Fatalf("err = %v; want a loud shape error naming the missing renderer", err)
	}
}

func TestPodcastEpisodes(t *testing.T) {
	var bodies []map[string]any
	c := stubClient(t, &Credentials{Cookie: testCookie}, func(req *http.Request) (*http.Response, error) {
		body := podcastRequestBody(t, req)
		bodies = append(bodies, body)
		if id, _ := navString(body, "browseId"); id == "MPSPPLdMrbgYfVl-s16D_iT2BJCJ90pWtTO1A4" {
			return jsonResponse(200, podcastFixture(t, "podcast_episodes.json")), nil
		}
		if token, _ := navString(body, "continuation"); token == theDailyContToken {
			return jsonResponse(200, podcastFixture(t, "podcast_episodes_continuation.json")), nil
		}
		t.Errorf("unexpected browse body: %v", body)
		return jsonResponse(200, `{}`), nil
	})

	// Bare playlistId: the MPSP prefix must be added on the wire and kept on
	// the returned ID.
	podcast, episodes, err := c.PodcastEpisodes(context.Background(), "PLdMrbgYfVl-s16D_iT2BJCJ90pWtTO1A4")
	if err != nil {
		t.Fatalf("PodcastEpisodes: %v", err)
	}
	if podcast.ID != "MPSPPLdMrbgYfVl-s16D_iT2BJCJ90pWtTO1A4" {
		t.Errorf("podcast.ID = %q; want the MPSP-prefixed browseId", podcast.ID)
	}
	if podcast.Title != "The Daily" || podcast.Author != "New York Times Podcasts" {
		t.Errorf("podcast header = %+v; want The Daily / New York Times Podcasts", podcast)
	}
	if !strings.Contains(podcast.ThumbnailURL, "podcasts_artwork") {
		t.Errorf("podcast.ThumbnailURL = %q; want show artwork", podcast.ThumbnailURL)
	}

	if len(episodes) != 7 {
		t.Fatalf("got %d episodes; want 4 + 3 via continuation", len(episodes))
	}
	first := episodes[0]
	if first.VideoID != "YVr6_FPheV0" {
		t.Errorf("episode[0].VideoID = %q", first.VideoID)
	}
	if first.Title != "All the President’s Planes" {
		t.Errorf("episode[0].Title = %q", first.Title)
	}
	// Subtitle runs are ["4.3K views", " • ", "2h ago"]; Published is the
	// final run, never the view count.
	if first.Published != "2h ago" {
		t.Errorf("episode[0].Published = %q; want 2h ago", first.Published)
	}
	if first.DurationSec != 23*60 {
		t.Errorf("episode[0].DurationSec = %d; want %d (from \"23 min\")", first.DurationSec, 23*60)
	}
	if !strings.HasPrefix(first.Description, "When President Trump left Turkey") {
		t.Errorf("episode[0].Description = %q", first.Description)
	}
	if !strings.Contains(first.ThumbnailURL, "YVr6_FPheV0") {
		t.Errorf("episode[0].ThumbnailURL = %q; want the episode thumbnail", first.ThumbnailURL)
	}

	fromCont := episodes[4]
	if fromCont.VideoID != "Ce1RVnjhzkA" || fromCont.Published != "Feb 12" || fromCont.DurationSec != 30*60 {
		t.Errorf("episode[4] (first continuation item) = %+v", fromCont)
	}
	if last := episodes[6]; last.VideoID != "6yqj_6h_miI" || last.DurationSec != 40*60 {
		t.Errorf("episode[6] = %+v", last)
	}

	if len(bodies) != 2 {
		t.Fatalf("browse called %d times; want 2", len(bodies))
	}
	if _, ok := bodies[1]["browseId"]; ok {
		t.Errorf("continuation request must not repeat browseId: %v", bodies[1])
	}
}

func TestPodcastEpisodesOverlayVideoIDFallback(t *testing.T) {
	body := `{"contents": {"twoColumnBrowseResultsRenderer": {
		"tabs": [{"tabRenderer": {"content": {"sectionListRenderer": {"contents": [
			{"musicResponsiveHeaderRenderer": {
				"title": {"runs": [{"text": "Show"}]},
				"straplineTextOne": {"runs": [{"text": "Author"}]}}}]}}}}],
		"secondaryContents": {"sectionListRenderer": {"contents": [
			{"musicShelfRenderer": {"contents": [
				{"musicMultiRowListItemRenderer": {
					"title": {"runs": [{"text": "No onTap"}]},
					"overlay": {"musicItemThumbnailOverlayRenderer": {"content":
						{"musicPlayButtonRenderer": {"playNavigationEndpoint":
							{"watchEndpoint": {"videoId": "fallback123"}}}}}}}},
				{"musicMultiRowListItemRenderer": {
					"title": {"runs": [{"text": "No watch endpoint at all"}]}}}
			]}}]}}}}}`
	c := stubClient(t, &Credentials{Cookie: testCookie}, func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, body), nil
	})
	podcast, episodes, err := c.PodcastEpisodes(context.Background(), "MPSPPLx")
	if err != nil {
		t.Fatalf("PodcastEpisodes: %v", err)
	}
	if podcast.Title != "Show" || podcast.Author != "Author" {
		t.Errorf("podcast = %+v", podcast)
	}
	// The endpoint-less item is unplayable and skipped; the overlay one keeps
	// its videoId.
	if len(episodes) != 1 || episodes[0].VideoID != "fallback123" {
		t.Fatalf("episodes = %+v; want exactly the overlay-fallback item", episodes)
	}
}

func TestPodcastEpisodesStuckContinuation(t *testing.T) {
	shelf := `{"contents": [{"musicMultiRowListItemRenderer": {
			"title": {"runs": [{"text": "Ep"}]},
			"onTap": {"watchEndpoint": {"videoId": "v1"}}}}],
		"continuations": [{"nextContinuationData": {"continuation": "SAME_TOKEN"}}]}`
	page := `{"contents": {"twoColumnBrowseResultsRenderer": {
		"tabs": [{"tabRenderer": {"content": {"sectionListRenderer": {"contents": [
			{"musicResponsiveHeaderRenderer": {"title": {"runs": [{"text": "Show"}]}}}]}}}}],
		"secondaryContents": {"sectionListRenderer": {"contents": [
			{"musicShelfRenderer": ` + shelf + `}]}}}}}`
	cont := `{"continuationContents": {"musicShelfContinuation": ` + shelf + `}}`
	c := stubClient(t, &Credentials{Cookie: testCookie}, func(req *http.Request) (*http.Response, error) {
		body := podcastRequestBody(t, req)
		if _, ok := body["browseId"]; ok {
			return jsonResponse(200, page), nil
		}
		return jsonResponse(200, cont), nil
	})
	_, _, err := c.PodcastEpisodes(context.Background(), "MPSPPLx")
	if err == nil || !strings.Contains(err.Error(), "did not advance") {
		t.Fatalf("err = %v; want loud abort on a repeating continuation token", err)
	}
}

func TestPodcastEpisodesEmptyID(t *testing.T) {
	c := stubClient(t, &Credentials{Cookie: testCookie}, func(*http.Request) (*http.Response, error) {
		t.Error("no request should be sent for an empty podcast ID")
		return jsonResponse(200, `{}`), nil
	})
	if _, _, err := c.PodcastEpisodes(context.Background(), "  "); err == nil {
		t.Fatal("PodcastEpisodes with empty ID should error")
	}
}

func TestParsePodcastDurationText(t *testing.T) {
	cases := map[string]int{
		"23 min":      1380,
		"59 min":      3540,
		"1 hr 12 min": 4320,
		"1 hr":        3600,
		"2 hrs":       7200,
		"45 sec":      45,
		"1 hr 2 min":  3720,
		"3:25":        205,
		"1:02:11":     3731,
		"":            0,
		" • ":         0,
		"Played":      0,
		"12":          0,
		"min":         0,
	}
	for text, want := range cases {
		if got := parsePodcastDurationText(text); got != want {
			t.Errorf("parsePodcastDurationText(%q) = %d; want %d", text, got, want)
		}
	}
}
