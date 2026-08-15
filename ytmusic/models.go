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
	// VideoType is the InnerTube musicVideoType of the entry (one of the
	// VideoType* constants), or "" when the response carries none — e.g.
	// greyed-out unavailable rows, which have no watch endpoint at all.
	VideoType string `json:"video_type"`
}

// The musicVideoType values InnerTube stamps on playlist entries.
const (
	// VideoTypeATV is a catalog song (audio track).
	VideoTypeATV = "MUSIC_VIDEO_TYPE_ATV"
	// VideoTypeOMV is an official music video.
	VideoTypeOMV = "MUSIC_VIDEO_TYPE_OMV"
	// VideoTypeUGC is a user-generated video.
	VideoTypeUGC = "MUSIC_VIDEO_TYPE_UGC"
	// VideoTypePodcastEpisode is a podcast episode surfaced in a playlist.
	VideoTypePodcastEpisode = "MUSIC_VIDEO_TYPE_PODCAST_EPISODE"
	// VideoTypePrivatelyOwned is a track the account uploaded or migrated
	// (e.g. from Google Play Music); it plays only for its owning account.
	VideoTypePrivatelyOwned = "MUSIC_VIDEO_TYPE_PRIVATELY_OWNED_TRACK"
)

// IsPrivatelyOwned reports whether the track is an uploaded/migrated track
// that only the owning account can stream.
func (t Track) IsPrivatelyOwned() bool {
	return t.VideoType == VideoTypePrivatelyOwned
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
