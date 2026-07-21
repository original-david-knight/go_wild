package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	data "github.com/original-david-knight/go_wild/agent_data"
)

// Shared defaults for pipeline runner CLI invocations. Individual runners
// (claude-code, codex) used to redeclare these with provider-prefixed names;
// they always had identical values, so they live here now.
const (
	pipelineRunnerDefaultTimeoutSec    = 1200
	pipelineRunnerMaxTimeoutSec        = 3600
	pipelineRunnerDefaultMaxConcurrent = 3
	pipelineRunnerCorrectionMaxChars   = 4000
)

// baseBwrapSandboxArgs returns the bwrap arguments common to every pipeline
// runner sandbox: namespace unsharing, ro-binds for system dirs, proc/dev,
// a sized tmpfs at /tmp, and the /sandbox skeleton directories. Callers
// append provider-specific binds (executable, config, auth files, work dir)
// and the --clearenv / --setenv block (see baseBwrapEnvArgs).
//
// Keeping these namespace/ro-bind flags in one place matters for security:
// if one runner drops --assert-userns-disabled or --unshare-pid and the
// other keeps it, the sandboxes silently diverge. Share the prefix so
// changes apply everywhere by construction.
//
// /etc handling: rather than binding all of /etc, we bind only the files
// required for DNS resolution, TLS trust, timezone, and user/group name
// lookups. This avoids exposing sensitive host files inside the sandbox.
// Even though /etc/shadow is typically mode 0640 root:shadow (unreadable
// to the sandbox's unprivileged UID), defense in depth: fewer mounted
// paths means less attack surface if a misconfigured host file (a
// root-readable secret in /etc/, a stray SSH host key, a PAM config with
// a secret) ever becomes readable. Explicitly NOT bound: /etc/shadow,
// /etc/gshadow, /etc/sudoers, /etc/sudoers.d, /etc/ssh, /etc/pam.d,
// /etc/security, /etc/environment (the last would also leak env vars
// past --clearenv).
func baseBwrapSandboxArgs(tmpfsSize int) []string {
	args := []string{
		"--die-with-parent",
		"--new-session",
		"--unshare-user",
		"--uid", strconv.Itoa(os.Getuid()),
		"--gid", strconv.Itoa(os.Getgid()),
		"--unshare-pid",
		"--unshare-uts",
		"--unshare-ipc",
		"--unshare-cgroup-try",
		"--disable-userns",
		"--assert-userns-disabled",
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/bin", "/bin",
		"--ro-bind", "/lib", "/lib",
		"--ro-bind", "/lib64", "/lib64",
	}
	for _, path := range selectiveEtcBinds {
		args = append(args, "--ro-bind-try", path, path)
	}
	args = append(args,
		"--ro-bind-try", "/run/systemd/resolve", "/run/systemd/resolve",
		"--proc", "/proc",
		"--dev", "/dev",
		"--size", strconv.Itoa(tmpfsSize),
		"--tmpfs", "/tmp",
		"--dir", "/sandbox",
		"--dir", "/sandbox/bin",
		"--dir", "/sandbox/config",
	)
	return args
}

// selectiveEtcBinds is the allowlist of host /etc paths exposed into
// every pipeline runner sandbox. Keep this list as small as possible;
// each entry is a path the sandboxed CLI (claude, codex, or their
// subprocesses) genuinely needs.
//
//   - DNS / hostname resolution: resolv.conf, hosts, host.conf, hostname,
//     nsswitch.conf, gai.conf. Without these, https calls to API
//     endpoints fail with "cannot resolve host".
//   - TLS trust store: ssl/, ca-certificates/, ca-certificates.conf (Debian
//     /Ubuntu /Arch), pki/ (RHEL /Fedora). Multi-distro coverage — missing
//     paths are handled by --ro-bind-try.
//   - Timezone: localtime (symlinked into /usr/share/zoneinfo on most
//     systems; bwrap resolves the link host-side).
//   - User/group name lookups: passwd, group. World-readable by design, so
//     binding them does not expose anything that isn't already public.
//     Many tools (git, some Node libs) call getpwuid(getuid()) and emit
//     cryptic errors without /etc/passwd.
var selectiveEtcBinds = []string{
	"/etc/resolv.conf",
	"/etc/hosts",
	"/etc/host.conf",
	"/etc/hostname",
	"/etc/nsswitch.conf",
	"/etc/gai.conf",
	"/etc/ssl",
	"/etc/ca-certificates",
	"/etc/ca-certificates.conf",
	"/etc/pki",
	"/etc/localtime",
	"/etc/passwd",
	"/etc/group",
}

// baseBwrapEnvArgs returns the --clearenv + canonical HOME/PATH/locale args
// appended to every pipeline runner sandbox command. Each runner supplies
// its own sandbox HOME path (claude uses /home/sandbox, codex uses
// /sandbox/home) and PATH (codex omits sbin entries).
func baseBwrapEnvArgs(sandboxHome, sandboxPath string) []string {
	return []string{
		"--clearenv",
		"--setenv", "HOME", sandboxHome,
		"--setenv", "PATH", sandboxPath,
		"--setenv", "LANG", "C.UTF-8",
		"--setenv", "LC_ALL", "C.UTF-8",
	}
}

// resolveTieredEnvConfig returns the env var value for a pipeline step's
// model/profile selection. Methods with model_tier="fast" read fastEnv;
// all others read smartEnv. label is a short identifier used in the
// returned error so ops can tell which runner was misconfigured.
//
// Returns an error (rather than panicking) on missing/whitespace-only env.
// Pipeline steps run in a detached goroutine (see TriggerPipeline), so a
// panic here crashes the whole manager process; callers surface the error
// through the normal step-failure path instead.
func resolveTieredEnvConfig(methodDef *data.A2AMethod, fastEnv, smartEnv, label string) (string, error) {
	if methodDef != nil && strings.ToLower(strings.TrimSpace(methodDef.ModelTier)) == "fast" {
		if v := strings.TrimSpace(os.Getenv(fastEnv)); v != "" {
			return v, nil
		}
		return "", fmt.Errorf("%s: %s not set", label, fastEnv)
	}
	if v := strings.TrimSpace(os.Getenv(smartEnv)); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s: %s not set", label, smartEnv)
}
