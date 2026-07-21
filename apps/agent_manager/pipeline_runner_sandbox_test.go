package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
)

// TestResolveTieredEnvConfigFastTier verifies that methods with
// model_tier="fast" read the fast env var and ignore the smart one.
func TestResolveTieredEnvConfigFastTier(t *testing.T) {
	t.Setenv("TEST_FAST_ENV", "fast-value")
	t.Setenv("TEST_SMART_ENV", "smart-value")

	methodDef := &data.A2AMethod{ModelTier: "fast"}
	got, err := resolveTieredEnvConfig(methodDef, "TEST_FAST_ENV", "TEST_SMART_ENV", "test")
	if err != nil {
		t.Fatalf("resolveTieredEnvConfig returned error: %v", err)
	}
	if got != "fast-value" {
		t.Fatalf("resolveTieredEnvConfig = %q, want fast-value", got)
	}

	// Case-insensitive + surrounding whitespace should still route to fast.
	methodDef = &data.A2AMethod{ModelTier: "  FAST  "}
	got, err = resolveTieredEnvConfig(methodDef, "TEST_FAST_ENV", "TEST_SMART_ENV", "test")
	if err != nil {
		t.Fatalf("resolveTieredEnvConfig (whitespace+caps) returned error: %v", err)
	}
	if got != "fast-value" {
		t.Fatalf("resolveTieredEnvConfig (whitespace+caps) = %q, want fast-value", got)
	}
}

// TestResolveTieredEnvConfigSmartTier verifies that methods without a fast
// tier fall through to the smart env var.
func TestResolveTieredEnvConfigSmartTier(t *testing.T) {
	t.Setenv("TEST_FAST_ENV", "fast-value")
	t.Setenv("TEST_SMART_ENV", "smart-value")

	cases := []struct {
		name   string
		method *data.A2AMethod
	}{
		{"nil_methodDef", nil},
		{"empty_tier", &data.A2AMethod{ModelTier: ""}},
		{"smart_tier", &data.A2AMethod{ModelTier: "smart"}},
		{"unknown_tier", &data.A2AMethod{ModelTier: "deep"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveTieredEnvConfig(tc.method, "TEST_FAST_ENV", "TEST_SMART_ENV", "test")
			if err != nil {
				t.Fatalf("resolveTieredEnvConfig returned error: %v", err)
			}
			if got != "smart-value" {
				t.Fatalf("resolveTieredEnvConfig = %q, want smart-value", got)
			}
		})
	}
}

// TestResolveTieredEnvConfigReturnsErrorOnMissingFast verifies the fast-
// tier error names the missing env var. Load-bearing: pipeline steps
// run in a detached goroutine, so panicking would crash the whole
// manager; the error must instead propagate through the step-failure
// path with enough detail for ops to identify the misconfiguration.
func TestResolveTieredEnvConfigReturnsErrorOnMissingFast(t *testing.T) {
	t.Setenv("TEST_FAST_ENV", "")
	t.Setenv("TEST_SMART_ENV", "smart-value")

	methodDef := &data.A2AMethod{ModelTier: "fast"}

	got, err := resolveTieredEnvConfig(methodDef, "TEST_FAST_ENV", "TEST_SMART_ENV", "my-label")
	if err == nil {
		t.Fatalf("expected error, got value %q", got)
	}
	msg := err.Error()
	if !strings.Contains(msg, "my-label") {
		t.Fatalf("error %q missing label", msg)
	}
	if !strings.Contains(msg, "TEST_FAST_ENV") {
		t.Fatalf("error %q missing env var name", msg)
	}
}

// TestResolveTieredEnvConfigReturnsErrorOnMissingSmart verifies the
// smart-tier error includes the smart env var name (not the fast one).
func TestResolveTieredEnvConfigReturnsErrorOnMissingSmart(t *testing.T) {
	t.Setenv("TEST_FAST_ENV", "fast-value")
	t.Setenv("TEST_SMART_ENV", "")

	// nil methodDef → smart path
	got, err := resolveTieredEnvConfig(nil, "TEST_FAST_ENV", "TEST_SMART_ENV", "my-label")
	if err == nil {
		t.Fatalf("expected error, got value %q", got)
	}
	msg := err.Error()
	if !strings.Contains(msg, "my-label") {
		t.Fatalf("error %q missing label", msg)
	}
	if !strings.Contains(msg, "TEST_SMART_ENV") {
		t.Fatalf("error %q missing env var name", msg)
	}
	if strings.Contains(msg, "TEST_FAST_ENV") {
		t.Fatalf("error for missing SMART should not mention FAST env: %q", msg)
	}
}

// TestResolveTieredEnvConfigTreatsWhitespaceAsUnset verifies that an env
// var containing only whitespace is treated the same as unset. Ops
// sometimes leave values blank in .env files and this prevents a silent
// "selected model: whitespace" failure downstream.
func TestResolveTieredEnvConfigTreatsWhitespaceAsUnset(t *testing.T) {
	t.Setenv("TEST_SMART_ENV", "   \t  ")

	got, err := resolveTieredEnvConfig(nil, "TEST_FAST_ENV", "TEST_SMART_ENV", "test")
	if err == nil {
		t.Fatalf("expected error for whitespace-only env, got value %q", got)
	}
}

// TestBaseBwrapSandboxArgsSelectiveEtcBinds exercises the shared helper
// directly, independent of either caller, so that a future refactor that
// inlines /etc handling in just one runner still has a guard against
// regressing the allowlist. See the TODO item "bwrap args bind-mount /etc
// read-only" for the security rationale: the blanket `/etc` bind used to
// expose /etc/shadow, /etc/sudoers, /etc/ssh, etc., and this test pins the
// replacement allowlist.
func TestBaseBwrapSandboxArgsSelectiveEtcBinds(t *testing.T) {
	args := baseBwrapSandboxArgs(123)

	// Regression guard: the blanket /etc ro-bind must never return.
	if containsArgTriplet(args, "--ro-bind", "/etc", "/etc") {
		t.Fatalf("baseBwrapSandboxArgs must not bind /etc wholesale: %v", args)
	}

	// Allowlist: each entry mounted with --ro-bind-try so the same list
	// works on distros where a given file is absent.
	wantEtc := []string{
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
	// The test-visible list must match the package-private source of truth.
	// Without this, someone could remove an entry from selectiveEtcBinds and
	// this test would still pass because it checks a stale literal list.
	if len(selectiveEtcBinds) != len(wantEtc) {
		t.Fatalf("selectiveEtcBinds drifted from test expectation: got %d entries %v, test expects %d %v", len(selectiveEtcBinds), selectiveEtcBinds, len(wantEtc), wantEtc)
	}
	for i, got := range selectiveEtcBinds {
		if got != wantEtc[i] {
			t.Fatalf("selectiveEtcBinds[%d] = %q, want %q (test list must mirror source)", i, got, wantEtc[i])
		}
	}
	for _, path := range wantEtc {
		if !containsArgTriplet(args, "--ro-bind-try", path, path) {
			t.Fatalf("baseBwrapSandboxArgs missing --ro-bind-try %s: %v", path, args)
		}
	}

	// Explicit deny-list: these paths appeared as descendants of the
	// previous blanket `/etc` bind. Catches both accidental re-adds of
	// the blanket bind AND someone naively extending the allowlist.
	for _, sensitive := range []string{
		"/etc/shadow",
		"/etc/gshadow",
		"/etc/sudoers",
		"/etc/sudoers.d",
		"/etc/ssh",
		"/etc/pam.d",
		"/etc/security",
		"/etc/environment",
	} {
		if containsArg(args, sensitive) {
			t.Fatalf("sensitive host path %s must not be bound by baseBwrapSandboxArgs: %v", sensitive, args)
		}
	}

	// Existing namespace / core ro-bind flags must survive. Copied from
	// the codex builder test so refactors to the shared helper don't
	// silently drop one of these without the corresponding caller test
	// catching it in the narrow window where the helper's output is not
	// yet reflected in the caller's cmd.Args.
	for _, flag := range []string{
		"--die-with-parent",
		"--unshare-user",
		"--unshare-pid",
		"--disable-userns",
		"--assert-userns-disabled",
	} {
		if !containsArg(args, flag) {
			t.Fatalf("baseBwrapSandboxArgs missing %s: %v", flag, args)
		}
	}

	// Network is intentionally NOT unshared: codex needs outbound HTTPS to
	// reach the OpenAI API, and the MCP broker child process needs to reach
	// the host broker. The codex-runner safety argument at the
	// --dangerously-bypass-approvals-and-sandbox call site documents this
	// as deliberate non-isolation, with network exfiltration managed by
	// broker HMAC auth + TLS rather than the sandbox. A future "harden the
	// sandbox" refactor that adds --unshare-net here would break codex
	// completely and violate the documented contract. Pin it.
	if containsArg(args, "--unshare-net") {
		t.Fatalf("baseBwrapSandboxArgs must not unshare network (codex needs outbound HTTPS): %v", args)
	}
}

// TestSelectiveEtcBindsNoSensitivePaths is a belt-and-suspenders check on
// the source-of-truth list — independent of how baseBwrapSandboxArgs uses
// it, the list itself must never contain a sensitive entry. Guards against
// a refactor that moves the render logic elsewhere but reuses the list.
func TestSelectiveEtcBindsNoSensitivePaths(t *testing.T) {
	sensitive := map[string]struct{}{
		"/etc/shadow":      {},
		"/etc/gshadow":     {},
		"/etc/sudoers":     {},
		"/etc/sudoers.d":   {},
		"/etc/ssh":         {},
		"/etc/pam.d":       {},
		"/etc/security":    {},
		"/etc/environment": {},
	}
	for _, entry := range selectiveEtcBinds {
		if _, bad := sensitive[entry]; bad {
			t.Fatalf("selectiveEtcBinds must not include sensitive path %q", entry)
		}
	}
}

// TestBuildCodexPipelineSandboxCommandBindsCodexConfigAndAuth exercises the
// codex-specific sandbox builder end-to-end. Complements the claude-side
// coverage: asserts the shared bwrap prefix is present (namespace flags,
// ro-binds for /usr /bin /lib /lib64, selective /etc binds, tmpfs /tmp,
// /sandbox skeleton), that codex-specific binds point at the right
// in-sandbox paths (config.toml, auth.json under /sandbox/home/.codex),
// that the shared env block is applied, and that the codex CLI tail has
// the expected flags.
func TestBuildCodexPipelineSandboxCommandBindsCodexConfigAndAuth(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)

	codexDir := filepath.Join(userHome, ".codex")
	if err := os.MkdirAll(codexDir, 0700); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}
	authPath := filepath.Join(codexDir, "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"access_token":"oauth"}`), 0600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}

	binDir := t.TempDir()
	bwrapPath := filepath.Join(binDir, "bwrap")
	if err := os.WriteFile(bwrapPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake bwrap: %v", err)
	}
	codexPath := filepath.Join(binDir, "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	mcpBrokerPath := filepath.Join(binDir, "mcp-broker-server")
	if err := os.WriteFile(mcpBrokerPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake mcp broker: %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	mcpConfigDir := t.TempDir()
	mcpConfigPath := filepath.Join(mcpConfigDir, "config.toml")
	if err := os.WriteFile(mcpConfigPath, []byte(`[mcp_servers.broker]`+"\n"), 0600); err != nil {
		t.Fatalf("write mcp config: %v", err)
	}

	cmd, cleanup, err := buildCodexPipelineSandboxCommand(
		context.Background(),
		"investigate market 42",
		"you are codex",
		"smart-profile",
		mcpConfigPath,
		mcpBrokerPath,
		codexPath,
		false,
	)
	if err != nil {
		t.Fatalf("buildCodexPipelineSandboxCommand returned error: %v", err)
	}
	defer cleanup()

	if !strings.Contains(filepath.Base(cmd.Dir), "gowild-codex-runner-") {
		t.Fatalf("cmd.Dir = %q, want scratch runner dir", cmd.Dir)
	}

	// Shared bwrap prefix: namespace and ro-bind flags that
	// baseBwrapSandboxArgs contributes. If any of these disappear the codex
	// sandbox has silently weakened.
	mustArg := []string{
		"--die-with-parent",
		"--unshare-user",
		"--unshare-pid",
		"--unshare-uts",
		"--unshare-ipc",
		"--unshare-cgroup-try",
		"--disable-userns",
		"--assert-userns-disabled",
		"--proc",
		"--dev",
		"--tmpfs",
	}
	for _, a := range mustArg {
		if !containsArg(cmd.Args, a) {
			t.Fatalf("command args missing shared bwrap flag %q: %v", a, cmd.Args)
		}
	}
	for _, roBind := range []string{"/usr", "/bin", "/lib", "/lib64"} {
		if !containsArgTriplet(cmd.Args, "--ro-bind", roBind, roBind) {
			t.Fatalf("command args missing ro-bind %s: %v", roBind, cmd.Args)
		}
	}
	// /etc is bound selectively, not as a whole directory. If a future edit
	// regresses to `--ro-bind /etc /etc`, this check catches it.
	if containsArgTriplet(cmd.Args, "--ro-bind", "/etc", "/etc") {
		t.Fatalf("command args bind /etc wholesale — regression; bind specific files instead: %v", cmd.Args)
	}

	// Network is intentionally NOT unshared: the outer bwrap jail contains
	// the --dangerously-bypass-approvals-and-sandbox risk via filesystem /
	// process / userns isolation, but codex must reach the OpenAI API and
	// the MCP broker child must reach the host broker, so network exposure
	// is an accepted part of the design. A refactor adding --unshare-net
	// here would break codex completely and contradict the safety argument
	// in the call-site comment. Pin it at the caller level too so the
	// intent is enforced even if someone reworks the shared helper.
	if containsArg(cmd.Args, "--unshare-net") {
		t.Fatalf("codex sandbox must not unshare network (outbound HTTPS required): %v", cmd.Args)
	}

	// No unexpected writable mounts. The call-site safety argument
	// (codex_runner.go: "Why --dangerously-bypass-approvals-and-sandbox is
	// safe here") states that the only writable host-visible path is /work
	// and that there is no --dev-bind and no --bind other than the
	// per-invocation /work mapping. Pin that invariant: a future refactor
	// that adds another --bind or any --dev-bind silently invalidates the
	// comment (and expands the blast radius of a rogue tool call) — fail
	// loudly instead.
	if containsArg(cmd.Args, "--dev-bind") || containsArg(cmd.Args, "--dev-bind-try") {
		t.Fatalf("codex sandbox must not use --dev-bind (writable device bind weakens isolation): %v", cmd.Args)
	}
	var rwBinds [][2]string
	for i := 0; i+2 < len(cmd.Args); i++ {
		if cmd.Args[i] == "--bind" || cmd.Args[i] == "--bind-try" {
			rwBinds = append(rwBinds, [2]string{cmd.Args[i+1], cmd.Args[i+2]})
		}
	}
	if len(rwBinds) != 1 {
		t.Fatalf("expected exactly one writable --bind (scratchDir -> /work), got %d: %v", len(rwBinds), rwBinds)
	}
	if rwBinds[0][0] != cmd.Dir || rwBinds[0][1] != "/work" {
		t.Fatalf("the only writable --bind must map scratchDir -> /work, got %v -> %v", rwBinds[0][0], rwBinds[0][1])
	}
	// Each allowlisted /etc path uses --ro-bind-try (files may not exist
	// on every distro).
	for _, etcPath := range []string{
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
	} {
		if !containsArgTriplet(cmd.Args, "--ro-bind-try", etcPath, etcPath) {
			t.Fatalf("command args missing --ro-bind-try %s: %v", etcPath, cmd.Args)
		}
	}
	// Sensitive files must not appear anywhere in the arg list (neither as
	// a standalone path nor as a bind source). The blanket /etc bind used
	// to expose all of these; the allowlist replaces it.
	for _, sensitive := range []string{
		"/etc/shadow",
		"/etc/gshadow",
		"/etc/sudoers",
		"/etc/sudoers.d",
		"/etc/ssh",
		"/etc/pam.d",
		"/etc/security",
		"/etc/environment",
	} {
		if containsArg(cmd.Args, sensitive) {
			t.Fatalf("sensitive host path %s must not be bound into sandbox: %v", sensitive, cmd.Args)
		}
	}
	for _, dir := range []string{"/sandbox", "/sandbox/bin", "/sandbox/config"} {
		if !containsArgPair(cmd.Args, "--dir", dir) {
			t.Fatalf("command args missing --dir %s: %v", dir, cmd.Args)
		}
	}

	// Codex-specific binds: broker binary, scratch workdir, codex config.toml
	// and auth.json under /sandbox/home/.codex.
	if !containsArgTriplet(cmd.Args, "--ro-bind", mustAbsPath(t, mcpBrokerPath), codexSandboxMCPBroker) {
		t.Fatalf("command args missing bound MCP broker binary: %v", cmd.Args)
	}
	if !containsArgTriplet(cmd.Args, "--bind", cmd.Dir, "/work") {
		t.Fatalf("command args missing scratch workdir bind: %v", cmd.Args)
	}
	if !containsArgPair(cmd.Args, "--dir", codexSandboxHome+"/.codex") {
		t.Fatalf("command args missing --dir for codex home: %v", cmd.Args)
	}
	if !containsArgTriplet(cmd.Args, "--ro-bind", mustAbsPath(t, mcpConfigPath), codexSandboxHome+"/.codex/config.toml") {
		t.Fatalf("command args missing bound codex config.toml: %v", cmd.Args)
	}
	if !containsArgTriplet(cmd.Args, "--ro-bind-try", authPath, codexSandboxHome+"/.codex/auth.json") {
		t.Fatalf("command args missing bound codex auth.json: %v", cmd.Args)
	}

	// Shared env block: clearenv + HOME/PATH/LANG/LC_ALL. The HOME value
	// must be the codex sandbox home, not claude's.
	if !containsArg(cmd.Args, "--clearenv") {
		t.Fatalf("command args missing --clearenv: %v", cmd.Args)
	}
	if !containsArgTriplet(cmd.Args, "--setenv", "HOME", codexSandboxHome) {
		t.Fatalf("command args missing --setenv HOME %s: %v", codexSandboxHome, cmd.Args)
	}
	if !containsArgTriplet(cmd.Args, "--setenv", "PATH", codexSandboxPath) {
		t.Fatalf("command args missing --setenv PATH %s: %v", codexSandboxPath, cmd.Args)
	}

	// Codex CLI tail: after `--`, the codex executable and its flags.
	if !containsArg(cmd.Args, "exec") {
		t.Fatalf("command args missing codex exec subcommand: %v", cmd.Args)
	}
	if !containsArg(cmd.Args, "--json") {
		t.Fatalf("command args missing codex --json flag: %v", cmd.Args)
	}
	if !containsArg(cmd.Args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("command args missing codex bypass flag: %v", cmd.Args)
	}
	if !containsArg(cmd.Args, "--skip-git-repo-check") {
		t.Fatalf("command args missing --skip-git-repo-check: %v", cmd.Args)
	}
	if !containsArg(cmd.Args, "--ephemeral") {
		t.Fatalf("command args missing --ephemeral: %v", cmd.Args)
	}
	if !containsArgPair(cmd.Args, "-p", "smart-profile") {
		t.Fatalf("command args missing profile flag: %v", cmd.Args)
	}

	// Scratch dir is removed by cleanup.
	scratchDir := cmd.Dir
	cleanup()
	if _, err := os.Stat(scratchDir); !os.IsNotExist(err) {
		t.Fatalf("scratch dir should be removed after cleanup, stat err=%v", err)
	}
}

// TestBuildCodexPipelineSandboxCommandOmitsProfileFlagWhenEmpty verifies
// that passing an empty profile does NOT append `-p` (which would produce
// an invalid codex invocation).
func TestBuildCodexPipelineSandboxCommandOmitsProfileFlagWhenEmpty(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)

	binDir := t.TempDir()
	for _, name := range []string{"bwrap", "codex", "mcp-broker-server"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	mcpConfigPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(mcpConfigPath, []byte(""), 0600); err != nil {
		t.Fatalf("write mcp config: %v", err)
	}

	cmd, cleanup, err := buildCodexPipelineSandboxCommand(
		context.Background(),
		"do x",
		"",
		"",
		mcpConfigPath,
		filepath.Join(binDir, "mcp-broker-server"),
		filepath.Join(binDir, "codex"),
		false,
	)
	if err != nil {
		t.Fatalf("buildCodexPipelineSandboxCommand: %v", err)
	}
	defer cleanup()

	if containsArg(cmd.Args, "-p") {
		t.Fatalf("empty profile should not produce -p flag: %v", cmd.Args)
	}
}
