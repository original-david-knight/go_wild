package gowild_my

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPatchEnvFileIsTheOneWritePath pins the shared path's whole contract:
// replace-in-place, append-missing in the caller's order, comments and
// unrelated lines survive verbatim, and the renamed file is 0600.
func TestPatchEnvFileIsTheOneWritePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thing.env")
	existing := "# a comment that must survive\nKEY_A=old\nCUSTOM_OVERRIDE=kept\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	err := PatchEnvFile(path, "# scaffold\n", map[string]string{
		"KEY_A": "new",
		"KEY_B": "added",
	}, []string{"KEY_A", "KEY_B"})
	if err != nil {
		t.Fatalf("PatchEnvFile: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{
		"# a comment that must survive",
		"KEY_A=new",
		"CUSTOM_OVERRIDE=kept",
		"KEY_B=added",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("patched file lost %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "old") {
		t.Errorf("the replaced value survived:\n%s", got)
	}
	if strings.Contains(got, "scaffold") {
		t.Errorf("the scaffold overwrote an existing file:\n%s", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %04o, want 0600", perm)
	}

	// A fresh file starts from the scaffold.
	fresh := filepath.Join(dir, "fresh.env")
	if err := PatchEnvFile(fresh, "# explains itself\n", map[string]string{"K": "v"}, []string{"K"}); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(fresh)
	if !strings.Contains(string(raw), "# explains itself") || !strings.Contains(string(raw), "K=v") {
		t.Errorf("fresh file = %q, want the scaffold plus the key", raw)
	}

	// The read half sees exactly what the write half put down.
	vars, err := ReadEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if vars["KEY_A"] != "new" || vars["KEY_B"] != "added" || vars["CUSTOM_OVERRIDE"] != "kept" {
		t.Errorf("ReadEnvFile = %v, want the patched values", vars)
	}
	missing, err := ReadEnvFile(filepath.Join(dir, "absent.env"))
	if err != nil || len(missing) != 0 {
		t.Errorf("ReadEnvFile(absent) = %v, %v — a missing file is empty, not an error", missing, err)
	}
}
