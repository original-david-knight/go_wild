package gowild_ytmusic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const testVideoID = "dQw4w9WgXcQ"

// fakeYtDlp writes an executable shell script standing in for yt-dlp and
// returns its path.
func fakeYtDlp(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake yt-dlp: %v", err)
	}
	return path
}

func TestResolverSuccess(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$@" > %s
cat <<'EOF'
{"url":"https://rr3---sn-x.googlevideo.com/videoplayback?expire=1755000000&itag=140","ext":"m4a","duration":205.5,"abr":129.478}
EOF
`, argsFile)
	r := NewResolver(fakeYtDlp(t, script))

	info, err := r.Resolve(context.Background(), testVideoID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := "https://rr3---sn-x.googlevideo.com/videoplayback?expire=1755000000&itag=140"; info.URL != want {
		t.Errorf("URL = %q, want %q", info.URL, want)
	}
	if info.MIME != "audio/mp4" {
		t.Errorf("MIME = %q, want audio/mp4", info.MIME)
	}
	if want := time.Unix(1755000000, 0); !info.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v (from expire param)", info.ExpiresAt, want)
	}
	if info.DurationSec != 205.5 {
		t.Errorf("DurationSec = %v, want 205.5", info.DurationSec)
	}
	if info.ABR != 129.478 {
		t.Errorf("ABR = %v, want 129.478", info.ABR)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read recorded args: %v", err)
	}
	got := strings.Split(strings.TrimSpace(string(raw)), "\n")
	want := []string{
		"-f", "bestaudio[ext=m4a]/bestaudio",
		"--no-playlist",
		"--no-warnings",
		"-j",
		"https://music.youtube.com/watch?v=" + testVideoID,
	}
	if !slices.Equal(got, want) {
		t.Errorf("yt-dlp args = %q, want %q", got, want)
	}
}

func TestResolverCookiesFile(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$@" > %s
echo '{"url":"https://x.googlevideo.com/videoplayback?expire=1755000000","ext":"m4a","duration":1,"abr":1}'
`, argsFile)
	cookies := filepath.Join(t.TempDir(), "cookies.txt")
	r := NewResolver(fakeYtDlp(t, script), WithCookiesFile(cookies))

	if _, err := r.Resolve(context.Background(), testVideoID); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read recorded args: %v", err)
	}
	got := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(got) < 2 || got[len(got)-2] != "--cookies" || got[len(got)-1] != cookies {
		t.Errorf("yt-dlp args = %q, want trailing [--cookies %s]", got, cookies)
	}
}

func TestResolverURLFallback(t *testing.T) {
	// No top-level url/ext: both must come from requested_downloads[0].
	script := `#!/bin/sh
echo '{"requested_downloads":[{"url":"https://rr1.googlevideo.com/videoplayback?expire=1760000000","ext":"webm"}],"duration":10,"abr":64}'
`
	r := NewResolver(fakeYtDlp(t, script))

	info, err := r.Resolve(context.Background(), testVideoID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := "https://rr1.googlevideo.com/videoplayback?expire=1760000000"; info.URL != want {
		t.Errorf("URL = %q, want %q (from requested_downloads[0])", info.URL, want)
	}
	if info.MIME != "audio/webm" {
		t.Errorf("MIME = %q, want audio/webm", info.MIME)
	}
	if want := time.Unix(1760000000, 0); !info.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", info.ExpiresAt, want)
	}
}

func TestResolverExpiryFloor(t *testing.T) {
	// URL without an expire param: ExpiresAt falls back to now+1h.
	script := `#!/bin/sh
echo '{"url":"https://rr1.googlevideo.com/videoplayback?itag=140","ext":"m4a","duration":10,"abr":128}'
`
	r := NewResolver(fakeYtDlp(t, script))

	before := time.Now()
	info, err := r.Resolve(context.Background(), testVideoID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	after := time.Now()
	if info.ExpiresAt.Before(before.Add(time.Hour)) || info.ExpiresAt.After(after.Add(time.Hour)) {
		t.Errorf("ExpiresAt = %v, want now+1h (between %v and %v)", info.ExpiresAt, before.Add(time.Hour), after.Add(time.Hour))
	}
}

func TestResolverStderrInError(t *testing.T) {
	script := `#!/bin/sh
echo "ERROR: [youtube] dQw4w9WgXcQ: Sign in to confirm you are not a bot" >&2
exit 1
`
	r := NewResolver(fakeYtDlp(t, script))

	_, err := r.Resolve(context.Background(), testVideoID)
	if err == nil {
		t.Fatal("Resolve: want error on yt-dlp exit 1")
	}
	if !strings.Contains(err.Error(), "Sign in to confirm you are not a bot") {
		t.Errorf("error %q does not carry yt-dlp's stderr", err)
	}
}

func TestResolverTimeout(t *testing.T) {
	// exec so the sleep is the process itself and dies with the kill.
	script := "#!/bin/sh\nexec sleep 5\n"
	r := NewResolver(fakeYtDlp(t, script), WithResolveTimeout(100*time.Millisecond))

	_, err := r.Resolve(context.Background(), testVideoID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Resolve error = %v, want context.DeadlineExceeded", err)
	}
}

func TestResolverInvalidVideoID(t *testing.T) {
	// A path that does not exist: if validation ever ran after exec, every
	// case would fail with an exec error instead of the validation message.
	r := NewResolver(filepath.Join(t.TempDir(), "missing-yt-dlp"))
	for _, id := range []string{
		"",
		"short",
		"dQw4w9WgXc",   // 10 chars
		"dQw4w9WgXcQQ", // 12 chars
		"bad;id$(rm)",  // 11 chars, shell metacharacters
		"has spaces!",
	} {
		_, err := r.Resolve(context.Background(), id)
		if err == nil {
			t.Errorf("Resolve(%q): want error", id)
			continue
		}
		if !strings.Contains(err.Error(), "invalid video ID") {
			t.Errorf("Resolve(%q) error = %q, want invalid-video-ID rejection", id, err)
		}
	}
}

func TestResolverEmptyPath(t *testing.T) {
	r := NewResolver("")
	_, err := r.Resolve(context.Background(), testVideoID)
	if err == nil || !strings.Contains(err.Error(), "yt-dlp path") {
		t.Fatalf("Resolve error = %v, want empty yt-dlp path rejection", err)
	}
}

func TestResolverBadJSON(t *testing.T) {
	script := "#!/bin/sh\necho 'not json at all'\n"
	r := NewResolver(fakeYtDlp(t, script))

	_, err := r.Resolve(context.Background(), testVideoID)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("Resolve error = %v, want JSON decode failure", err)
	}
}

func TestResolverMissingURL(t *testing.T) {
	script := "#!/bin/sh\necho '{\"ext\":\"m4a\",\"duration\":10}'\n"
	r := NewResolver(fakeYtDlp(t, script))

	_, err := r.Resolve(context.Background(), testVideoID)
	if err == nil || !strings.Contains(err.Error(), "no stream url") {
		t.Fatalf("Resolve error = %v, want missing-url failure", err)
	}
}

func TestResolverConcurrencyLimit(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "running")
	if err := os.Mkdir(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	countsFile := filepath.Join(dir, "counts")
	// Each fake marks itself running, lingers so overlapping fakes see each
	// other, then records how many marks exist. The semaphore holds that
	// number at 2 no matter how many goroutines call Resolve.
	script := fmt.Sprintf(`#!/bin/sh
touch %[1]s/$$
sleep 0.3
ls %[1]s | wc -l >> %[2]s
rm %[1]s/$$
echo '{"url":"https://x.googlevideo.com/videoplayback?expire=1755000000","ext":"m4a","duration":1,"abr":1}'
`, runDir, countsFile)
	r := NewResolver(fakeYtDlp(t, script)) // default WithMaxConcurrent(2)

	ids := []string{"aaaaaaaaaaa", "bbbbbbbbbbb", "ccccccccccc", "ddddddddddd", "eeeeeeeeeee", "fffffffffff"}
	errs := make([]error, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = r.Resolve(context.Background(), id)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Resolve(%s): %v", ids[i], err)
		}
	}

	raw, err := os.ReadFile(countsFile)
	if err != nil {
		t.Fatalf("read counts: %v", err)
	}
	maxSeen := 0
	for _, field := range strings.Fields(string(raw)) {
		n, err := strconv.Atoi(field)
		if err != nil {
			t.Fatalf("counts file entry %q: %v", field, err)
		}
		if n > 2 {
			t.Errorf("observed %d fakes running at once; semaphore allows 2", n)
		}
		if n > maxSeen {
			maxSeen = n
		}
	}
	if maxSeen != 2 {
		t.Errorf("max observed concurrency = %d, want both slots in use at some point", maxSeen)
	}
}

func TestMimeForExt(t *testing.T) {
	for _, tc := range []struct{ ext, want string }{
		{"m4a", "audio/mp4"},
		{"webm", "audio/webm"},
		{"opus", "audio/webm"},
		{"mp3", "audio/mpeg"},
		{"", "audio/mpeg"},
	} {
		if got := mimeForExt(tc.ext); got != tc.want {
			t.Errorf("mimeForExt(%q) = %q, want %q", tc.ext, got, tc.want)
		}
	}
}

func TestExpiryFromURL(t *testing.T) {
	now := time.Unix(1700000000, 0)
	floor := now.Add(time.Hour)
	for _, tc := range []struct {
		url  string
		want time.Time
	}{
		{"https://r.googlevideo.com/videoplayback?expire=1755000000&itag=140", time.Unix(1755000000, 0)},
		{"https://r.googlevideo.com/videoplayback?expire=abc", floor},
		{"https://r.googlevideo.com/videoplayback?expire=0", floor},
		{"https://r.googlevideo.com/videoplayback", floor},
		{"://not a url", floor},
	} {
		if got := expiryFromURL(tc.url, now); !got.Equal(tc.want) {
			t.Errorf("expiryFromURL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}
