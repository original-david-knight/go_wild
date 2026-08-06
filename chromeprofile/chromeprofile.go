// Package chromeprofile resolves which Chrome profile is signed in as a given
// account, and opens URLs there.
//
// A Chrome window is owned by whatever profile launched it, and Chrome opens
// an <a target="_blank"> in the profile that owns the clicking window — the
// destination and its session are never consulted. A link tied to a specific
// account therefore lands signed-out unless the caller routes it: Chrome's
// Local State file maps every profile directory to its signed-in email, and
// --profile-directory names where a URL goes.
//
// Read-only over Chrome's files: this package parses Local State and execs the
// browser; nothing here writes into the Chrome config dir.
package chromeprofile

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Browser is pinned absolute because callers such as systemd user services do
// not carry a login shell's PATH. Chrome, not chromium — the profiles being
// matched are the owner's Chrome profiles.
const Browser = "/usr/bin/google-chrome-stable"

// LocalStatePath is Chrome's profile registry for the invoking user.
func LocalStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "google-chrome", "Local State")
}

// Profile is one entry from the registry: the directory name Chrome wants on
// --profile-directory, and the local display name for logs and responses.
type Profile struct {
	Dir  string
	Name string
}

// Find returns the profile signed in as email, from the registry at path. A
// nil Profile with a nil error means no profile matches — an ordinary state
// (the account may simply not be signed into Chrome), distinct from an
// unreadable or unparsable registry.
func Find(path, email string) (*Profile, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		// Signed-out profiles carry an empty user_name; an empty query must
		// not match them.
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state struct {
		Profile struct {
			InfoCache map[string]struct {
				UserName string `json:"user_name"`
				Name     string `json:"name"`
			} `json:"info_cache"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("chrome Local State: %w", err)
	}
	for dir, info := range state.Profile.InfoCache {
		if strings.EqualFold(strings.TrimSpace(info.UserName), email) {
			return &Profile{Dir: dir, Name: info.Name}, nil
		}
	}
	return nil, nil
}

// Open opens url as a tab in the given profile: a running Chrome adopts it
// into an existing window of that profile, otherwise the profile starts. The
// child gets its own session because when no Chrome is running, this child IS
// the browser — a service restart must not take the owner's windows down with
// it. A goroutine reaps the handoff process.
func Open(profileDir, url string) error {
	cmd := exec.Command(Browser, "--profile-directory="+profileDir, url)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
