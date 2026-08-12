// Package gowild_ytmusic is a YouTube Music library client over the private
// InnerTube browse API, the same protocol the music.youtube.com web player
// speaks. It authenticates with a browser cookie (SAPISIDHASH scheme) and
// reads the user's library: playlists and their tracks, podcasts and their
// episodes. Zero non-stdlib dependencies.
package gowild_ytmusic

// Playlist is one library playlist.
type Playlist struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	TrackCount   int    `json:"track_count"`
	ThumbnailURL string `json:"thumbnail_url"`
}

// Track is one playlist entry.
type Track struct {
	VideoID      string `json:"video_id"`
	Title        string `json:"title"`
	Artist       string `json:"artist"`
	Album        string `json:"album"`
	DurationSec  int    `json:"duration_sec"`
	ThumbnailURL string `json:"thumbnail_url"`
}

// Podcast is one library podcast subscription.
type Podcast struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Author       string `json:"author"`
	ThumbnailURL string `json:"thumbnail_url"`
}

// Episode is one podcast episode. Published is the display string InnerTube
// emits ("Aug 5", "3 days ago") — it is not a parseable timestamp.
type Episode struct {
	VideoID      string `json:"video_id"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Published    string `json:"published"`
	DurationSec  int    `json:"duration_sec"`
	ThumbnailURL string `json:"thumbnail_url"`
}
