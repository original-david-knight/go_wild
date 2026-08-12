package gowild_ytmusic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// StreamInfo is one resolved audio stream: a directly playable googlevideo
// URL and the metadata a player needs to schedule around it.
type StreamInfo struct {
	URL         string    `json:"url"`
	MIME        string    `json:"mime"`
	ExpiresAt   time.Time `json:"expires_at"`
	DurationSec float64   `json:"duration_sec"`
	ABR         float64   `json:"abr"`
}

// videoIDRe is the exact shape of a YouTube video ID. Anything else is
// rejected before it can reach a command line.
var videoIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// Resolver turns video IDs into streamable URLs by shelling out to yt-dlp.
// It is deliberately decoupled from the InnerTube Client: stream resolution
// is yt-dlp's problem, and this wrapper only bounds and interprets it.
type Resolver struct {
	ytDlpPath   string
	cookiesFile string
	timeout     time.Duration
	sem         chan struct{}
}

// ResolverOption configures a Resolver at construction.
type ResolverOption func(*Resolver)

// WithCookiesFile passes a Netscape-format cookies file to yt-dlp
// (--cookies), which age-restricted and premium-quality streams require.
func WithCookiesFile(path string) ResolverOption {
	return func(r *Resolver) { r.cookiesFile = path }
}

// WithResolveTimeout bounds one yt-dlp invocation (default 60s).
func WithResolveTimeout(d time.Duration) ResolverOption {
	return func(r *Resolver) {
		if d <= 0 {
			panic(fmt.Sprintf("ytmusic: WithResolveTimeout(%v): duration must be positive", d))
		}
		r.timeout = d
	}
}

// WithMaxConcurrent caps simultaneous yt-dlp processes (default 2).
func WithMaxConcurrent(n int) ResolverOption {
	return func(r *Resolver) {
		if n < 1 {
			panic(fmt.Sprintf("ytmusic: WithMaxConcurrent(%d): need at least 1", n))
		}
		r.sem = make(chan struct{}, n)
	}
}

// NewResolver builds a Resolver around the yt-dlp binary at ytDlpPath.
func NewResolver(ytDlpPath string, opts ...ResolverOption) *Resolver {
	r := &Resolver{
		ytDlpPath: ytDlpPath,
		timeout:   60 * time.Second,
		sem:       make(chan struct{}, 2),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Resolve runs yt-dlp for one video ID and returns the best audio stream.
// It waits for a concurrency slot first (abandoning the wait if ctx ends),
// then holds yt-dlp to the resolve timeout.
func (r *Resolver) Resolve(ctx context.Context, videoID string) (*StreamInfo, error) {
	// The ID is spliced into a command line, so it is validated before it is
	// used for anything at all.
	if !videoIDRe.MatchString(videoID) {
		return nil, fmt.Errorf("ytmusic: resolve: invalid video ID %q (want exactly 11 characters of [A-Za-z0-9_-])", videoID)
	}
	if r.ytDlpPath == "" {
		return nil, fmt.Errorf("ytmusic: resolve %s: empty yt-dlp path", videoID)
	}

	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return nil, fmt.Errorf("ytmusic: resolve %s: waiting for a yt-dlp slot: %w", videoID, ctx.Err())
	}

	// The timeout covers the yt-dlp run, not time spent queued for a slot.
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	args := []string{
		"-f", "bestaudio[ext=m4a]/bestaudio",
		"--no-playlist",
		"--no-warnings",
		"-j",
		"https://music.youtube.com/watch?v=" + videoID,
	}
	if r.cookiesFile != "" {
		args = append(args, "--cookies", r.cookiesFile)
	}

	cmd := exec.CommandContext(ctx, r.ytDlpPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// A killed yt-dlp can leave a child process holding the output pipes;
	// WaitDelay bounds how long that can stall Run's return after ctx ends.
	cmd.WaitDelay = 5 * time.Second

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("ytmusic: resolve %s: yt-dlp: %w: %s", videoID, ctxErr, stderrText(&stderr))
		}
		return nil, fmt.Errorf("ytmusic: resolve %s: yt-dlp: %w: %s", videoID, err, stderrText(&stderr))
	}

	var info map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return nil, fmt.Errorf("ytmusic: resolve %s: decode yt-dlp JSON: %w", videoID, err)
	}

	// -j with -f merges the chosen format's fields to the top level, but some
	// yt-dlp versions only report them under requested_downloads.
	streamURL, ok := navString(info, "url")
	if !ok || streamURL == "" {
		streamURL, _ = navString(info, "requested_downloads", 0, "url")
	}
	if streamURL == "" {
		return nil, fmt.Errorf("ytmusic: resolve %s: yt-dlp JSON carries no stream url (neither top-level nor requested_downloads[0])", videoID)
	}
	ext, ok := navString(info, "ext")
	if !ok || ext == "" {
		ext, _ = navString(info, "requested_downloads", 0, "ext")
	}
	duration, _ := info["duration"].(float64)
	abr, _ := info["abr"].(float64)

	return &StreamInfo{
		URL:         streamURL,
		MIME:        mimeForExt(ext),
		ExpiresAt:   expiryFromURL(streamURL, time.Now()),
		DurationSec: duration,
		ABR:         abr,
	}, nil
}

// stderrText compresses yt-dlp's stderr into one error-message-sized line so
// failures stay diagnosable without dumping pages of progress noise.
func stderrText(buf *bytes.Buffer) string {
	s := strings.Join(strings.Fields(buf.String()), " ")
	if len(s) > 1000 {
		s = s[:1000] + "..."
	}
	if s == "" {
		return "(no stderr)"
	}
	return s
}

// mimeForExt maps yt-dlp's container ext to a playable MIME type. audio/mpeg
// is the last resort for exts this never expects: a wrong-but-audio type lets
// a player attempt playback instead of refusing outright.
func mimeForExt(ext string) string {
	switch ext {
	case "m4a":
		return "audio/mp4"
	case "webm", "opus":
		return "audio/webm"
	default:
		return "audio/mpeg"
	}
}

// expiryFromURL reads the stream's own expiry: googlevideo playback URLs
// carry an `expire` query parameter in unix seconds. When the parameter is
// missing or unreadable the stream still expires — YouTube issues playback
// URLs valid for roughly six hours — so now+1h stands in as a conservative
// floor. The constant is deliberately not configurable: it models the
// service's known behavior, not a caller preference.
func expiryFromURL(streamURL string, now time.Time) time.Time {
	if u, err := url.Parse(streamURL); err == nil {
		if raw := u.Query().Get("expire"); raw != "" {
			if secs, err := strconv.ParseInt(raw, 10, 64); err == nil && secs > 0 {
				return time.Unix(secs, 0)
			}
		}
	}
	return now.Add(time.Hour)
}
