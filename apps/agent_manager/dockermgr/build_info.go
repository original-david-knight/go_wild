package dockermgr

import (
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"strings"
)

// BuildInfo captures the desired agent image build signature.
type BuildInfo struct {
	ID       string
	SHA      string
	DiffHash string
	Dirty    bool
	Source   string
}

// Known reports whether the build info is usable for comparisons.
func (b BuildInfo) Known() bool {
	return b.ID != "" && b.ID != "unknown"
}

var agentBuildPaths = []string{
	"go.work",
	"apps/agent",
	"agent_data",
	"agentic_loop",
	"crypto",
	"data",
	"knowledge_graph",
	"my",
	"tools",
}

// ComputeDesiredBuildInfo returns the current working tree signature for agent images.
func ComputeDesiredBuildInfo() BuildInfo {
	root, err := gitOutput("", "rev-parse", "--show-toplevel")
	if err != nil {
		return BuildInfo{ID: "unknown", Source: "git-unavailable"}
	}
	root = strings.TrimSpace(root)

	sha, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return BuildInfo{ID: "unknown", Source: "git-unavailable"}
	}
	sha = strings.TrimSpace(sha)

	diff, _ := gitOutputBytes(root, append([]string{"diff", "--no-ext-diff", "--binary", "--relative", "--"}, agentBuildPaths...)...)
	status, _ := gitOutputBytes(root, append([]string{"status", "--porcelain", "--untracked-files=normal", "--"}, agentBuildPaths...)...)

	dirty := len(diff) > 0 || len(status) > 0
	diffHash := ""
	if dirty {
		sum := sha256.Sum256(append(diff, status...))
		diffHash = hex.EncodeToString(sum[:])
		if len(diffHash) > 12 {
			diffHash = diffHash[:12]
		}
	}

	id := sha
	if dirty {
		id = sha + "-dirty-" + diffHash
	}

	return BuildInfo{
		ID:       id,
		SHA:      sha,
		DiffHash: diffHash,
		Dirty:    dirty,
		Source:   "git",
	}
}

func gitOutput(dir string, args ...string) (string, error) {
	out, err := gitOutputBytes(dir, args...)
	return string(out), err
}

func gitOutputBytes(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			// git diff returns exit code 1 when differences are found.
			if ee.ExitCode() == 1 {
				return out, nil
			}
		}
		return out, err
	}
	return out, nil
}
