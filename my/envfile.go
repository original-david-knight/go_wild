package gowild_my

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadEnvFile parses one env file into its key/value pairs. A missing file
// is an empty map, not an error: an integration that was never configured is
// a normal state. It is the read half of the env-file kit — a caller that
// wants the file's truth rather than the process environment's snapshot of
// it.
func ReadEnvFile(path string) (map[string]string, error) {
	vars, err := parseEnvFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return vars, nil
}

// PatchEnvFile rewrites an env file with updates applied in place: an
// existing KEY= line is replaced where it stands, a missing one is appended
// in the order given by appendOrder, and every other line — comments,
// overrides — is kept verbatim. A file that does not exist yet starts from
// scaffold. The write is atomic (temp file in the same directory, chmod 0600
// before any content lands, then rename).
//
// This is meant to be the one write path for env files that hold secrets:
// every caller that persists a value goes through here, so the atomicity,
// the mode and the comment-preserving patch are decided once.
func PatchEnvFile(path, scaffold string, updates map[string]string, appendOrder []string) error {
	dir := filepath.Dir(path)
	if err := ensureDir(dir); err != nil {
		return err
	}

	content := scaffold
	if raw, err := os.ReadFile(path); err == nil {
		content = string(raw)
	} else if !os.IsNotExist(err) {
		return err
	}

	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	seen := map[string]bool{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		key, _, ok := strings.Cut(trimmed, "=")
		if !ok || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key = strings.TrimSpace(key)
		if value, wanted := updates[key]; wanted {
			lines[i] = key + "=" + value
			seen[key] = true
		}
	}
	// Append the keys the file did not hold, in the caller's stable order.
	for _, key := range appendOrder {
		if value, wanted := updates[key]; wanted && !seen[key] {
			lines = append(lines, key+"="+value)
		}
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op after the rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, filepath.Clean(path))
}

// parseEnvFile reads path and returns its KEY=value pairs. Blank lines and
// comments are skipped, an "export " prefix is tolerated, and a value wrapped
// in matching single or double quotes is unwrapped.
func parseEnvFile(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	out := map[string]string{}
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		if key != "" {
			out[key] = value
		}
	}
	return out, nil
}

// ensureDir creates the directory at 0700 if it does not exist, and tightens
// the mode if it does: env files hold secrets, so the directory that holds
// them stays owner-only.
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}
