# ytmusic

A Go client for a YouTube Music **library**: playlists and their tracks, podcast
subscriptions and their episodes, read over the private InnerTube browse API — the same
protocol music.youtube.com itself speaks — plus a stream resolver that turns video IDs
into playable audio URLs by shelling out to yt-dlp. The browse client has zero non-stdlib
dependencies.

Module `github.com/original-david-knight/go_wild/ytmusic`, consumed via a local `replace`
directive (never published, never fetched from a proxy).

## Authentication

Cookie-based, SAPISIDHASH scheme: requests carry the browser's Cookie header and a
per-request `SAPISIDHASH` Authorization value derived from the SAPISID cookie.

| Function | Role |
| --- | --- |
| `ParseRawHeaders(raw) (*Credentials, error)` | Turns a DevTools "Copy request headers" blob into `Credentials` (Cookie + optional `X-Goog-AuthUser`). Accepts both classic and HTTP/2-lowercase header styles; rejoins split Cookie lines. |
| `SaveCredentials(path, c)` | Atomic write: 0700 parent dir, 0600 same-directory temp file, rename. The cookie is a live session secret. |
| `LoadCredentials(path)` | Reads what SaveCredentials wrote. |
| `NewClient(creds, opts...)` | Validates up front — nil creds or a cookie without a SAPISID is a construction error, not a per-request surprise. `WithHTTPClient` swaps the transport (tests). |

**`ErrAuthExpired`**: any browse rejected as unauthenticated — HTTP 401/403 *or* a 200
carrying an `{"error": {"code": 401|403}}` payload — wraps this sentinel (`errors.Is`).
Retrying with the same credentials cannot succeed; re-capture the cookie from a logged-in
browser.

## Browse API

| Call | Returns | Notes |
| --- | --- | --- |
| `Client.LibraryPlaylists(ctx)` | `[]Playlist` | Synthetic "New playlist" tile skipped; IDs have the `VL` browse prefix stripped. |
| `Client.PlaylistTracks(ctx, id)` | `(Playlist, []Track, error)` | `id` with or without `VL`. Complete track list — continuations followed to exhaustion. |
| `Client.LibraryPodcasts(ctx)` | `[]Podcast` | IDs keep their `MPSP` prefix and feed `PodcastEpisodes` directly. No subscriptions → empty list, not an error. |
| `Client.PodcastEpisodes(ctx, id)` | `(Podcast, []Episode, error)` | `id` with or without `MPSP`. `Episode.Published` is InnerTube display text ("3 days ago"), not a parseable timestamp. |

The parsers fail loudly when a page shape is unrecognizable (no silent partial results),
and the legacy pre-2024 query-parameter continuation scheme is rejected rather than
silently truncated.

## Stream resolution

`NewResolver(ytDlpPath, opts...)` wraps the yt-dlp binary; `Resolve(ctx, videoID)`
returns:

```go
type StreamInfo struct {
    URL         string    // directly playable googlevideo URL
    MIME        string    // from the container ext (m4a → audio/mp4, webm/opus → audio/webm)
    ExpiresAt   time.Time // the URL's own `expire` parameter; now+1h floor when absent
    DurationSec float64
    ABR         float64
}
```

- Format selection: `bestaudio[ext=m4a]/bestaudio`.
- Video IDs are validated (`^[A-Za-z0-9_-]{11}$`) **before** they touch a command line.
- Defaults: 60 s per yt-dlp run (`WithResolveTimeout`), 2 concurrent processes
  (`WithMaxConcurrent`); callers queue for a slot, abandoning the wait when ctx ends.
- Resolved URLs are **IP-bound and expiring**: use them from the resolving host, promptly,
  and re-resolve on upstream 403.
- `WithCookiesFile` passes a Netscape cookies file to yt-dlp (age-restricted /
  premium-quality streams need it) — but handing an account cookie to yt-dlp risks Google
  rotating the session and invalidating the browse cookie with it. Run yt-dlp anonymous
  unless you accept that risk.

## Example

```go
creds, err := gowild_ytmusic.ParseRawHeaders(devtoolsHeaderBlob) // or LoadCredentials(path)
if err != nil { /* ... */ }
client, err := gowild_ytmusic.NewClient(creds)
if err != nil { /* ... */ }

playlists, err := client.LibraryPlaylists(ctx)
pl, tracks, err := client.PlaylistTracks(ctx, playlists[0].ID)

resolver := gowild_ytmusic.NewResolver("yt-dlp")
info, err := resolver.Resolve(ctx, tracks[0].VideoID)
// info.URL plays from this host until info.ExpiresAt
```

## Maintenance notes

- InnerTube page shapes drift; the parsers already tolerate the known generations
  (two-column vs single-column pages, both header/shelf renderer names). When YouTube
  ships a new shape the calls fail with a message naming the missing renderer — extend
  the finder, don't loosen it.
- Stream resolution breaks whenever YouTube breaks yt-dlp. `yt-dlp -U` first.
- `clientVersion` in client.go pins the WEB_REMIX client version string; bump it if
  browse responses degrade.
