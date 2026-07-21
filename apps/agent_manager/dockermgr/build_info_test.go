package dockermgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentBuildPathsExist(t *testing.T) {
	root, err := gitOutput("", "rev-parse", "--show-toplevel")
	if err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	root = strings.TrimSpace(root)

	for _, p := range agentBuildPaths {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("agentBuildPaths entry %q does not exist under repo root %q: %v", p, root, err)
		}
	}
}
