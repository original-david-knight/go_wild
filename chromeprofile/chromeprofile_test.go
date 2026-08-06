package chromeprofile

import (
	"os"
	"path/filepath"
	"testing"
)

// localState is the fragment of Chrome's registry this package reads: signed-in
// profiles alongside the signed-out kind that carries an empty user_name.
const localState = `{"profile":{"info_cache":{
	"Default":{"name":"David","user_name":"personal@example.com"},
	"Profile 1":{"name":"work","user_name":"Owner@Work.example"},
	"Profile 2":{"name":"guest","user_name":""}
}}}`

func statePath(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Local State")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFindMatchesTheEmailCaseInsensitively(t *testing.T) {
	profile, err := Find(statePath(t, localState), "owner@work.example")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if profile == nil || profile.Dir != "Profile 1" || profile.Name != "work" {
		t.Errorf("profile = %+v, want Profile 1 (work)", profile)
	}
}

func TestFindNoMatchIsNotAnError(t *testing.T) {
	profile, err := Find(statePath(t, localState), "elsewhere@example.com")
	if profile != nil || err != nil {
		t.Errorf("Find = %+v, %v; an account Chrome has never seen is nil, nil", profile, err)
	}
}

func TestFindEmptyEmailNeverMatchesSignedOutProfiles(t *testing.T) {
	// "Profile 2" carries an empty user_name; an empty query must not match it.
	profile, err := Find(statePath(t, localState), "  ")
	if profile != nil || err != nil {
		t.Errorf("Find(blank) = %+v, %v; want nil, nil", profile, err)
	}
}

func TestFindReportsAnUnreadableRegistry(t *testing.T) {
	if _, err := Find(filepath.Join(t.TempDir(), "absent"), "a@b.example"); err == nil {
		t.Error("Find on a missing registry returned no error; the caller needs the reason")
	}
	if _, err := Find(statePath(t, "not json"), "a@b.example"); err == nil {
		t.Error("Find on an unparsable registry returned no error")
	}
}
