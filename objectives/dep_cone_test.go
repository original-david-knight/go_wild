package objectives

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// bannedDeps are the packages the module split removed. genai and agent_node
// came in with the planner; go-ethereum rode in behind agent_node. The life
// dashboard imports this module into a desktop binary, so any of them
// reappearing is a regression in the boundary, not a detail of some consumer.
var bannedDeps = []string{
	"genai",
	"go-ethereum",
	"agent_node",
}

func TestDependencyConeStaysTrimmed(t *testing.T) {
	// go list only loads packages, it does not compile them, so this stays
	// cheap enough to run on every `go test ./objectives/...`.
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not available")
	}

	cmd := exec.Command(goBin, "list", "-deps", "./...")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list -deps ./...: %v\n%s", err, stderr.String())
	}

	for _, line := range strings.Split(stdout.String(), "\n") {
		pkg := strings.TrimSpace(line)
		if pkg == "" {
			continue
		}
		for _, banned := range bannedDeps {
			if strings.Contains(pkg, banned) {
				t.Errorf("banned dependency %q back in the cone via %s", banned, pkg)
			}
		}
	}
}
