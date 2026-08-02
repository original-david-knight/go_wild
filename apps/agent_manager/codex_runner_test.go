package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	codexllm "github.com/original-david-knight/go_wild/codexllm"
)

// TestSanitizeHostCodexConfigForMergeTrailingNewline verifies the sanitizer
// produces a safe concatenation boundary regardless of whether the host
// config ends with a newline. Without this, appending a new [section]
// directly after a key line that lacks "\n" would merge the two into a
// single malformed line, e.g.
//
//	key = "value"[mcp_servers.broker]
func TestSanitizeHostCodexConfigForMergeTrailingNewline(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"no_trailing_newline", `model = "gpt-5"`},
		{"single_trailing_newline", `model = "gpt-5"` + "\n"},
		{"multiple_trailing_newlines", `model = "gpt-5"` + "\n\n\n"},
		{"trailing_crlf", `model = "gpt-5"` + "\r\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeHostCodexConfigForMerge([]byte(tc.in))
			if !strings.HasSuffix(got, "\n") {
				t.Fatalf("result must end with newline, got %q", got)
			}
			if strings.HasSuffix(got, "\n\n") {
				t.Fatalf("result must end with exactly one newline, got %q", got)
			}
			if !strings.Contains(got, `model = "gpt-5"`) {
				t.Fatalf("original content dropped: %q", got)
			}
		})
	}
}

// TestSanitizeHostCodexConfigForMergeEmpty verifies that absent/empty host
// config yields an empty string — no spurious leading newline that would
// produce a blank first line in the merged output.
func TestSanitizeHostCodexConfigForMergeEmpty(t *testing.T) {
	for _, in := range []string{"", "\n", "\n\n\n"} {
		got := sanitizeHostCodexConfigForMerge([]byte(in))
		if got != "" {
			t.Fatalf("empty/whitespace host config should yield empty result, got %q", got)
		}
	}
}

// TestSanitizeHostCodexConfigForMergeStripsBrokerSection verifies that an
// existing [mcp_servers.broker] section is removed so our freshly-appended
// section does not collide with it (duplicate TOML tables are an error).
func TestSanitizeHostCodexConfigForMergeStripsBrokerSection(t *testing.T) {
	in := strings.Join([]string{
		`model = "gpt-5"`,
		``,
		`[mcp_servers.broker]`,
		`command = "/some/old/broker"`,
		`tool_timeout_sec = 60`,
		``,
		`[mcp_servers.broker.env]`,
		`STALE_KEY = "stale"`,
		``,
		`[other_section]`,
		`keep = true`,
		``,
	}, "\n")
	got := sanitizeHostCodexConfigForMerge([]byte(in))
	for _, forbidden := range []string{
		"[mcp_servers.broker]",
		"[mcp_servers.broker.env]",
		"/some/old/broker",
		"STALE_KEY",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("result still contains %q: %q", forbidden, got)
		}
	}
	for _, kept := range []string{
		`model = "gpt-5"`,
		`[other_section]`,
		`keep = true`,
	} {
		if !strings.Contains(got, kept) {
			t.Fatalf("result missing expected content %q: %q", kept, got)
		}
	}
}

// TestSanitizeHostCodexConfigForMergeStripsBrokerSubsections verifies that
// arbitrary subsections like [mcp_servers.broker.foo] (not just .env) are
// stripped. The concatenated section owns the whole "broker" namespace.
func TestSanitizeHostCodexConfigForMergeStripsBrokerSubsections(t *testing.T) {
	in := strings.Join([]string{
		`[mcp_servers.broker.custom_subsection]`,
		`foo = "bar"`,
		``,
		`[mcp_servers.other_server]`,
		`keep = true`,
	}, "\n")
	got := sanitizeHostCodexConfigForMerge([]byte(in))
	if strings.Contains(got, "custom_subsection") || strings.Contains(got, `foo = "bar"`) {
		t.Fatalf("broker subsection not stripped: %q", got)
	}
	if !strings.Contains(got, "[mcp_servers.other_server]") {
		t.Fatalf("non-broker mcp_server section wrongly stripped: %q", got)
	}
	if !strings.Contains(got, "keep = true") {
		t.Fatalf("non-broker mcp_server content wrongly stripped: %q", got)
	}
}

// TestSanitizeHostCodexConfigForMergeDoesNotStripLookalikes verifies the
// sanitizer only strips exact matches. Section names like
// [mcp_servers.brokerage] or [mcp_servers_broker] share a prefix but are
// different tables and must be preserved.
func TestSanitizeHostCodexConfigForMergeDoesNotStripLookalikes(t *testing.T) {
	in := strings.Join([]string{
		`[mcp_servers.brokerage]`,
		`keep_a = true`,
		``,
		`[mcp_servers_broker]`,
		`keep_b = true`,
		``,
		`[not_mcp_servers.broker]`,
		`keep_c = true`,
	}, "\n")
	got := sanitizeHostCodexConfigForMerge([]byte(in))
	for _, kept := range []string{
		"[mcp_servers.brokerage]",
		"keep_a = true",
		"[mcp_servers_broker]",
		"keep_b = true",
		"[not_mcp_servers.broker]",
		"keep_c = true",
	} {
		if !strings.Contains(got, kept) {
			t.Fatalf("sanitizer wrongly stripped lookalike section %q from: %q", kept, got)
		}
	}
}

// TestSanitizeHostCodexConfigForMergeHandlesWhitespaceAndCommentsInHeader
// verifies that section headers with surrounding whitespace or trailing
// comments are still recognised and stripped. These forms are valid TOML
// even if uncommon in codex config files.
func TestSanitizeHostCodexConfigForMergeHandlesWhitespaceAndCommentsInHeader(t *testing.T) {
	in := strings.Join([]string{
		`model = "gpt-5"`,
		`  [mcp_servers.broker]   `,
		`command = "/old"`,
		`[mcp_servers.broker.env]  # inline comment`,
		`K = "v"`,
		`[keep_me]`,
		`ok = true`,
	}, "\n")
	got := sanitizeHostCodexConfigForMerge([]byte(in))
	if strings.Contains(got, "/old") || strings.Contains(got, `K = "v"`) {
		t.Fatalf("header whitespace/comment caused sanitizer to miss the section: %q", got)
	}
	if !strings.Contains(got, "[keep_me]") || !strings.Contains(got, "ok = true") {
		t.Fatalf("section following broker block was incorrectly stripped: %q", got)
	}
}

// TestSanitizeHostCodexConfigForMergeBrokerAtEOF verifies that a broker
// section running to EOF (no trailing sibling section) is fully removed and
// the remaining preamble still gets a single trailing newline.
func TestSanitizeHostCodexConfigForMergeBrokerAtEOF(t *testing.T) {
	in := strings.Join([]string{
		`model = "gpt-5"`,
		``,
		`[mcp_servers.broker]`,
		`command = "/old"`,
		`tool_timeout_sec = 10`,
	}, "\n")
	got := sanitizeHostCodexConfigForMerge([]byte(in))
	if strings.Contains(got, "mcp_servers.broker") || strings.Contains(got, "/old") {
		t.Fatalf("EOF-anchored broker section not fully stripped: %q", got)
	}
	if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
		t.Fatalf("result must end with exactly one newline, got %q", got)
	}
	if !strings.Contains(got, `model = "gpt-5"`) {
		t.Fatalf("preamble dropped: %q", got)
	}
}

// TestSanitizeHostCodexConfigForMergeStripsNormalizedBrokerVariants verifies
// that TOML-equivalent header forms for [mcp_servers.broker] are all stripped.
// Each of these is the same table per the TOML spec (whitespace around dots
// is ignored; bare and ASCII-quoted segments are interchangeable). If the
// sanitizer missed any of them, the merged file would contain a duplicate
// definition and codex would reject it at startup.
func TestSanitizeHostCodexConfigForMergeStripsNormalizedBrokerVariants(t *testing.T) {
	variants := []string{
		`[mcp_servers.broker]`,
		`[mcp_servers . broker]`,
		`[ mcp_servers.broker ]`,
		`[mcp_servers."broker"]`,
		`["mcp_servers"."broker"]`,
		`[mcp_servers . "broker"]`,
		`[mcp_servers.'broker']`,
	}
	for _, header := range variants {
		t.Run(header, func(t *testing.T) {
			in := strings.Join([]string{
				`model = "gpt-5"`,
				header,
				`command = "/stale/path"`,
				`tool_timeout_sec = 30`,
				`[keep_me]`,
				`ok = true`,
			}, "\n")
			got := sanitizeHostCodexConfigForMerge([]byte(in))
			if strings.Contains(got, "/stale/path") || strings.Contains(got, "tool_timeout_sec = 30") {
				t.Fatalf("variant %q not stripped:\n%s", header, got)
			}
			if !strings.Contains(got, "[keep_me]") || !strings.Contains(got, "ok = true") {
				t.Fatalf("variant %q caused sibling section to be dropped:\n%s", header, got)
			}
		})
	}
}

// TestWriteCodexMCPConfigMergesHostConfigWithoutTrailingNewline exercises the
// full merge end-to-end. Pinned behaviours: (1) the host config is included
// verbatim even when it has no trailing newline, (2) no duplicate
// [mcp_servers.broker] section is produced when the host already defines one,
// (3) the generated broker section contains the expected TOML structure, and
// (4) no stale broker config from the host leaks into the merged output.
func TestWriteCodexMCPConfigMergesHostConfigWithoutTrailingNewline(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	codexDir := filepath.Join(userHome, ".codex")
	if err := os.MkdirAll(codexDir, 0700); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}
	// Host config lacks a trailing newline AND has a stale
	// [mcp_servers.broker] section — both failure modes the sanitizer
	// exists to handle.
	hostConfig := strings.Join([]string{
		`model = "gpt-5"`,
		`[profiles.smart]`,
		`model = "gpt-5-smart"`,
		`[mcp_servers.broker]`,
		`command = "/stale/path"`,
		`tool_timeout_sec = 30`,
		`[mcp_servers.broker.env]`,
		`STALE = "x"`,
		`[profiles.fast]`,
		`model = "gpt-5-fast"`, // no trailing newline — deliberate
	}, "\n")
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(hostConfig), 0600); err != nil {
		t.Fatalf("write host config: %v", err)
	}

	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "mcp-broker-server"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake mcp-broker-server: %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	t.Setenv("BROKER_SECRET", "test-secret")
	t.Setenv("BROKER_URL", "http://broker.test")

	pe := &PipelineEngine{}
	path, _, cleanup, err := pe.writeCodexMCPConfig("agent-42", "pipeline-step", nil, false)
	if err != nil {
		t.Fatalf("writeCodexMCPConfig: %v", err)
	}
	defer cleanup()

	merged, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read merged config: %v", err)
	}
	mergedStr := string(merged)

	// Stale host-side broker config must be gone.
	for _, forbidden := range []string{`/stale/path`, `STALE = "x"`} {
		if strings.Contains(mergedStr, forbidden) {
			t.Fatalf("merged config still contains stale broker entry %q:\n%s", forbidden, mergedStr)
		}
	}

	// Our broker section must be present exactly once.
	if n := strings.Count(mergedStr, "[mcp_servers.broker]"); n != 1 {
		t.Fatalf("expected exactly one [mcp_servers.broker] section, got %d:\n%s", n, mergedStr)
	}
	if n := strings.Count(mergedStr, "[mcp_servers.broker.env]"); n != 1 {
		t.Fatalf("expected exactly one [mcp_servers.broker.env] section, got %d:\n%s", n, mergedStr)
	}

	// Host profiles must survive.
	for _, expected := range []string{
		`model = "gpt-5"`,
		`[profiles.smart]`,
		`model = "gpt-5-smart"`,
		`[profiles.fast]`,
		`model = "gpt-5-fast"`,
	} {
		if !strings.Contains(mergedStr, expected) {
			t.Fatalf("merged config missing preserved host entry %q:\n%s", expected, mergedStr)
		}
	}

	// Generated broker env block.
	for _, expected := range []string{
		`AGENT_ID = "agent-42"`,
		`BROKER_SECRET = "test-secret"`,
		`BROKER_URL = "http://broker.test"`,
		`EXECUTION_METHOD = "pipeline-step"`,
		`tool_timeout_sec = 600`,
	} {
		if !strings.Contains(mergedStr, expected) {
			t.Fatalf("merged config missing broker entry %q:\n%s", expected, mergedStr)
		}
	}

	// The critical regression: before the fix, a host config ending with
	// `model = "gpt-5-fast"` (no newline) would be concatenated directly
	// with `[mcp_servers.broker]`, producing a malformed line. Assert the
	// boundary is healthy.
	if strings.Contains(mergedStr, `gpt-5-fast"[mcp_servers.broker]`) {
		t.Fatalf("missing section boundary between host config and broker section:\n%s", mergedStr)
	}
	if !strings.Contains(mergedStr, "gpt-5-fast\"\n[mcp_servers.broker]") {
		t.Fatalf("expected newline separator between host tail and broker section, got:\n%s", mergedStr)
	}
}

// TestWriteCodexMCPConfigEmptyHostProducesStandaloneBrokerSection verifies
// that an absent host config.toml still yields a well-formed broker-only
// TOML file (no leading blank lines, section header is the first line).
func TestWriteCodexMCPConfigEmptyHostProducesStandaloneBrokerSection(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	// Intentionally do NOT create ~/.codex/config.toml — ReadFile returns
	// an empty byte slice and the sanitizer should yield "".

	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "mcp-broker-server"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake mcp-broker-server: %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	t.Setenv("BROKER_SECRET", "test-secret")
	t.Setenv("BROKER_URL", "http://broker.test")

	pe := &PipelineEngine{}
	path, _, cleanup, err := pe.writeCodexMCPConfig("agent-99", "", nil, false)
	if err != nil {
		t.Fatalf("writeCodexMCPConfig: %v", err)
	}
	defer cleanup()

	merged, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read merged config: %v", err)
	}
	mergedStr := string(merged)

	if !strings.HasPrefix(mergedStr, "[mcp_servers.broker]\n") {
		t.Fatalf("empty host config should produce broker section at start of file, got:\n%s", mergedStr)
	}
	if strings.Contains(mergedStr, `EXECUTION_METHOD`) {
		t.Fatalf("empty executionMethod should omit the env line, got:\n%s", mergedStr)
	}
}

// TestWriteCodexMCPConfigDisableAllToolsProducesOnlyHostConfig verifies the
// disableAllTools branch: no broker section appended, host config returned
// verbatim (after sanitizer normalization of any stale broker section).
func TestWriteCodexMCPConfigDisableAllToolsProducesOnlyHostConfig(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	codexDir := filepath.Join(userHome, ".codex")
	if err := os.MkdirAll(codexDir, 0700); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}
	hostConfig := `model = "gpt-5"` + "\n"
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(hostConfig), 0600); err != nil {
		t.Fatalf("write host config: %v", err)
	}

	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "mcp-broker-server"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake mcp-broker-server: %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	// BROKER_SECRET deliberately unset — disableAllTools=true means the
	// broker section is never built, so the secret check never runs. If
	// this test starts erroring on missing BROKER_SECRET, the short-circuit
	// has regressed.
	os.Unsetenv("BROKER_SECRET")

	pe := &PipelineEngine{}
	path, _, cleanup, err := pe.writeCodexMCPConfig("agent-1", "", nil, true)
	if err != nil {
		t.Fatalf("writeCodexMCPConfig: %v", err)
	}
	defer cleanup()

	merged, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read merged config: %v", err)
	}
	mergedStr := string(merged)
	if strings.Contains(mergedStr, "[mcp_servers.broker]") {
		t.Fatalf("disableAllTools should omit broker section, got:\n%s", mergedStr)
	}
	if !strings.Contains(mergedStr, `model = "gpt-5"`) {
		t.Fatalf("host config should be preserved, got:\n%s", mergedStr)
	}
}

// TestExtractTOMLSectionNameRecognisesHeaders pins the section-header parser
// used by the sanitizer. Any regression in what counts as a header directly
// translates to mis-stripped or mis-retained broker sections.
func TestExtractTOMLSectionNameRecognisesHeaders(t *testing.T) {
	cases := []struct {
		line     string
		wantName string
		wantOK   bool
	}{
		{`[mcp_servers.broker]`, "mcp_servers.broker", true},
		{`  [mcp_servers.broker]  `, "mcp_servers.broker", true},
		{`[mcp_servers.broker]  # trailing comment`, "mcp_servers.broker", true},
		{`[[mcp_servers.broker]]`, "mcp_servers.broker", true},
		{`[mcp_servers.broker.env]`, "mcp_servers.broker.env", true},
		{`[ other ]`, "other", true},
		{`key = "value"`, "", false},
		{`key = "[bracketed]"`, "", false},
		{`servers = [`, "", false}, // multi-line array opener
		{`]`, "", false},           // multi-line array closer
		{``, "", false},
		{`#[mcp_servers.broker]`, "", false}, // commented-out header
		{`[abc`, "", false},                  // missing ] — reaches single-bracket branch
		{`[[abc`, "", false},                 // missing ]] — reaches array-of-tables branch
		{`[[ `, "", false},                   // [[ with no ]] and no name
		// TOML spec equivalences: whitespace around dots is ignored, and a
		// dotted key segment can be bare OR ASCII-quoted. All of these
		// refer to the same table as [mcp_servers.broker] and must be
		// normalized so the sanitizer treats them identically.
		{`[mcp_servers . broker]`, "mcp_servers.broker", true},
		{`[ mcp_servers.broker ]`, "mcp_servers.broker", true},
		{`[mcp_servers."broker"]`, "mcp_servers.broker", true},
		{`["mcp_servers"."broker"]`, "mcp_servers.broker", true},
		{`[mcp_servers.'broker']`, "mcp_servers.broker", true},
		{`[mcp_servers . "broker" . env]`, "mcp_servers.broker.env", true},
		{`[[mcp_servers . broker]]`, "mcp_servers.broker", true},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			name, ok := extractTOMLSectionName(tc.line)
			if ok != tc.wantOK {
				t.Fatalf("extractTOMLSectionName(%q) ok = %v, want %v", tc.line, ok, tc.wantOK)
			}
			if name != tc.wantName {
				t.Fatalf("extractTOMLSectionName(%q) name = %q, want %q", tc.line, name, tc.wantName)
			}
		})
	}
}

// TestNormalizeTOMLKey pins the dotted-key canonicalization used by
// extractTOMLSectionName. Each case must collapse to the same form the
// sanitizer matches against ("mcp_servers.broker"); a regression here
// silently lets stale broker sections survive into the merged config.
func TestNormalizeTOMLKey(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`mcp_servers.broker`, "mcp_servers.broker"},
		{`mcp_servers . broker`, "mcp_servers.broker"},
		{` mcp_servers.broker `, "mcp_servers.broker"},
		{`mcp_servers."broker"`, "mcp_servers.broker"},
		{`"mcp_servers"."broker"`, "mcp_servers.broker"},
		{`mcp_servers.'broker'`, "mcp_servers.broker"},
		{`mcp_servers.broker.env`, "mcp_servers.broker.env"},
		{`"x"`, "x"},
		{`'x'`, "x"},
		{``, ""},
		// Single-char segments are too short to strip quotes from (len < 2).
		{`"`, `"`},
		// Mismatched quote chars are left alone.
		{`"x'`, `"x'`},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := normalizeTOMLKey(tc.in)
			if got != tc.want {
				t.Fatalf("normalizeTOMLKey(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestToParsedCodexResultCopiesAllFields verifies that toParsedCodexResult
// produces a parsedRunnerResult with every field from the codexllm.ParsedResult
// copied across. The runner spec's parse function wires this conversion, so a
// regression here (e.g. dropped FormatError flag) silently breaks correction
// retries — the shared executor decides whether to retry by reading
// FormatError, and if it's always false, malformed outputs would be reported
// as hard failures instead of triggering the retry branch.
//
// The Payload assertion checks content equivalence, not backing-store identity:
// the contract is "every key/value survives the conversion," not "the two maps
// share memory." A defensive copy in a future refactor would be equally valid.
func TestToParsedCodexResultCopiesAllFields(t *testing.T) {
	payload := map[string]any{"k": "v", "n": 7}
	src := codexllm.ParsedResult{
		Status:        "failed",
		Payload:       payload,
		FailureReason: "malformed",
		FormatError:   true,
	}
	got := toParsedCodexResult(src)

	if got.Status != "failed" {
		t.Fatalf("Status = %q, want failed", got.Status)
	}
	if got.FailureReason != "malformed" {
		t.Fatalf("FailureReason = %q, want malformed", got.FailureReason)
	}
	if !got.FormatError {
		t.Fatalf("FormatError = false, want true")
	}
	if len(got.Payload) != len(payload) {
		t.Fatalf("Payload size = %d, want %d: %v", len(got.Payload), len(payload), got.Payload)
	}
	for k, want := range payload {
		if gotV, ok := got.Payload[k]; !ok || gotV != want {
			t.Fatalf("Payload[%q] = %v (present=%v), want %v", k, gotV, ok, want)
		}
	}

	// Inverse: FormatError=false must stay false so the executor doesn't
	// spuriously retry well-formed failures.
	got2 := toParsedCodexResult(codexllm.ParsedResult{Status: "succeeded"})
	if got2.FormatError {
		t.Fatalf("FormatError should default to false, got true")
	}
}

// TestCodexRunnerSpecFields pins the static configuration for the Codex
// runner. Cross-runner invariants worth locking:
//   - label="codex" drives log-line routing and failure messages.
//   - jobIDPrefix="codex-" keeps localA2AJob rows attributable.
//   - modelProvider=OpenAI feeds agent.ModelProvider; a mis-set provider
//     would mis-route downstream tooling that branches on provider.
//   - deferFanOutActivation=true is load-bearing for the orphan detector:
//     fan-out branches must stay queued until the semaphore is acquired.
func TestCodexRunnerSpecFields(t *testing.T) {
	pe := &PipelineEngine{}
	spec := codexRunnerSpec(pe, "smart-test", nil)

	if spec.label != "codex" {
		t.Fatalf("label = %q, want codex", spec.label)
	}
	if spec.jobIDPrefix != "codex-" {
		t.Fatalf("jobIDPrefix = %q, want codex-", spec.jobIDPrefix)
	}
	if spec.modelProvider != data.LLMProviderOpenAI {
		t.Fatalf("modelProvider = %q, want %q", spec.modelProvider, data.LLMProviderOpenAI)
	}
	if !strings.Contains(strings.ToLower(spec.missionIntro), "codex") {
		t.Fatalf("missionIntro = %q, want to mention Codex", spec.missionIntro)
	}
	if !spec.deferFanOutActivation {
		t.Fatalf("deferFanOutActivation = false, want true (fan-out orphan-detector contract)")
	}
	for name, fn := range map[string]any{
		"invoke":                spec.invoke,
		"parse":                 spec.parse,
		"extractFinal":          spec.extractFinal,
		"formatEventLog":        spec.formatEventLog,
		"buildFailure":          spec.buildFailure,
		"buildCorrectionPrompt": spec.buildCorrectionPrompt,
	} {
		if fn == nil {
			t.Fatalf("spec.%s is nil", name)
		}
	}
}

// TestCodexRunnerSpecInvokeReturnsProfileError pins the profileErr short-
// circuit. When resolveCodexProfile fails (missing CODEX_*_PROFILE env),
// executeCodexStep still constructs the spec, and the error must surface
// through invoke so the step-run fails cleanly instead of crashing the
// pipeline goroutine.
func TestCodexRunnerSpecInvokeReturnsProfileError(t *testing.T) {
	pe := &PipelineEngine{}
	sentinel := errors.New("sentinel profile error")
	spec := codexRunnerSpec(pe, "", sentinel)

	activateCalled := false
	out, err := spec.invoke(context.Background(), pe, "agent", "prompt", "sys", func() {
		activateCalled = true
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("invoke returned err=%v, want sentinel", err)
	}
	if out != "" {
		t.Fatalf("invoke output = %q, want empty on error", out)
	}
	if activateCalled {
		t.Fatalf("activate must not run when profileErr is set (step-run stays queued)")
	}
}

// TestCodexRunnerSpecInvokeHappyPathCallsRunner verifies the non-error path:
// invoke forwards prompt/systemPrompt/profile through invokeCodexRunner to
// the test-seam runner. This mirrors TestInvokeCodexRunnerOnAcquireOrdering
// but exercises the wiring through codexRunnerSpec's closure instead of the
// method directly.
func TestCodexRunnerSpecInvokeHappyPathCallsRunner(t *testing.T) {
	pe := &PipelineEngine{}
	var got struct {
		agent, prompt, sys, profile string
	}
	pe.codexRunner = func(ctx context.Context, agentID, prompt, systemPrompt, profile string) (string, error) {
		got.agent = agentID
		got.prompt = prompt
		got.sys = systemPrompt
		got.profile = profile
		return "runner-output", nil
	}

	spec := codexRunnerSpec(pe, "smart-profile", nil)
	out, err := spec.invoke(context.Background(), pe, "agent-x", "prompt-x", "sys-x", nil)
	if err != nil {
		t.Fatalf("invoke returned err=%v", err)
	}
	if out != "runner-output" {
		t.Fatalf("invoke output = %q, want runner-output", out)
	}
	if got.agent != "agent-x" || got.prompt != "prompt-x" || got.sys != "sys-x" || got.profile != "smart-profile" {
		t.Fatalf("runner received unexpected args: %+v", got)
	}
}

// TestCodexRunnerSpecParseWiresCodexLLM verifies that spec.parse collapses
// codexllm.ParseResult output into parsedRunnerResult. Exercising both the
// stream-events success branch and the malformed-input format-error branch
// guards against a future refactor that bypasses toParsedCodexResult and
// drops FormatError (which would silently disable correction retries).
func TestCodexRunnerSpecParseWiresCodexLLM(t *testing.T) {
	pe := &PipelineEngine{}
	spec := codexRunnerSpec(pe, "profile", nil)

	t.Run("stream success", func(t *testing.T) {
		input := `{"type":"item.completed","item":{"type":"agent_message","text":"{\"status\":\"succeeded\",\"result\":{\"x\":1}}"}}`
		got := spec.parse(input)
		if got.Status != "succeeded" {
			t.Fatalf("Status = %q, want succeeded", got.Status)
		}
		if got.FormatError {
			t.Fatalf("FormatError = true, want false for well-formed output")
		}
		if _, ok := got.Payload["x"]; !ok {
			t.Fatalf("Payload missing x: %v", got.Payload)
		}
	})

	t.Run("malformed marks FormatError", func(t *testing.T) {
		// Empty output is the canonical "format error" case in codexllm.
		got := spec.parse("")
		if !got.FormatError {
			t.Fatalf("FormatError = false, want true for empty output")
		}
		if got.FailureReason == "" {
			t.Fatalf("FailureReason should be populated on format error")
		}
	})
}

// TestCodexSandboxEnvArgsForwardsSetVars verifies that env vars with values
// get emitted as `--setenv KEY VALUE` triplets and unset vars are silently
// omitted. Both halves matter: missing the forward breaks codex's network
// access (proxy vars, OPENAI_BASE_URL) inside the sandbox, while emitting
// triplets for unset vars injects empty strings into the sandbox env and
// can shadow defaults codex would otherwise derive.
func TestCodexSandboxEnvArgsForwardsSetVars(t *testing.T) {
	// Clear every key the function checks so the test starts from a known
	// empty baseline; otherwise the host env leaks into the assertion.
	keys := []string{
		"OPENAI_API_KEY",
		"OPENAI_BASE_URL",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"NO_PROXY",
		"http_proxy",
		"https_proxy",
		"no_proxy",
	}
	for _, k := range keys {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}

	// Set a subset — the rest should stay unset.
	t.Setenv("OPENAI_API_KEY", "sk-test-abc")
	t.Setenv("HTTPS_PROXY", "http://proxy.test:8080")

	args := codexSandboxEnvArgs()

	if !containsArgTriplet(args, "--setenv", "OPENAI_API_KEY", "sk-test-abc") {
		t.Fatalf("args missing --setenv OPENAI_API_KEY sk-test-abc: %v", args)
	}
	if !containsArgTriplet(args, "--setenv", "HTTPS_PROXY", "http://proxy.test:8080") {
		t.Fatalf("args missing --setenv HTTPS_PROXY ...: %v", args)
	}

	// Unset keys must NOT appear — not as --setenv triplets and not as bare
	// flag names. An empty-string forward would look like
	// `--setenv OPENAI_BASE_URL ""` which would hide a real default inside
	// the sandbox.
	for _, k := range []string{"OPENAI_BASE_URL", "HTTP_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"} {
		if containsArg(args, k) {
			t.Fatalf("unset env key %q wrongly forwarded in args: %v", k, args)
		}
	}
}

// TestCodexSandboxEnvArgsEmptyWhenAllUnset verifies the zero-case: when no
// known env var is set, the returned slice is empty (nil is also fine),
// so callers can append it unconditionally without polluting the sandbox
// env.
func TestCodexSandboxEnvArgsEmptyWhenAllUnset(t *testing.T) {
	for _, k := range []string{
		"OPENAI_API_KEY",
		"OPENAI_BASE_URL",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"NO_PROXY",
		"http_proxy",
		"https_proxy",
		"no_proxy",
	} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}

	args := codexSandboxEnvArgs()
	if len(args) != 0 {
		t.Fatalf("expected no --setenv args when all keys unset, got: %v", args)
	}
}

// TestCodexLogPipelineEventHandlesAllTypes verifies that the JSONL event
// logger tolerates every event shape codex emits without panicking. The
// function has no return value — it only writes to the Go log — so the
// test's contract is simply "do not panic and accept the input". A
// regression to a missing nil-check (e.g. event.Item dereferenced before
// the nil guard) would fail the whole pipeline runner at runtime, not just
// one step.
func TestCodexLogPipelineEventHandlesAllTypes(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"empty", ""},
		{"whitespace_only", "    "},
		{"not_json", "this is not JSON"},
		{"unknown_type", `{"type":"unknown.event"}`},
		{"turn_started", `{"type":"turn.started"}`},
		{"item_completed_no_item", `{"type":"item.completed"}`},
		{"item_completed_agent_message", `{"type":"item.completed","item":{"type":"agent_message","text":"hi"}}`},
		{"item_completed_agent_message_long", `{"type":"item.completed","item":{"type":"agent_message","text":"` + strings.Repeat("x", 500) + `"}}`},
		{"item_completed_tool_call", `{"type":"item.completed","item":{"type":"tool_call"}}`},
		{"item_completed_tool_output", `{"type":"item.completed","item":{"type":"tool_output"}}`},
		{"item_completed_error", `{"type":"item.completed","item":{"type":"error","message":"boom"}}`},
		{"item_completed_other", `{"type":"item.completed","item":{"type":"something_new"}}`},
		{"turn_completed_no_usage", `{"type":"turn.completed"}`},
		{"turn_completed_with_usage", `{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":5}}`},
		{"error_event", `{"type":"error","message":"fatal"}`},
		{"turn_failed", `{"type":"turn.failed","message":"timeout"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The contract is "do not panic"; the log output is intentionally
			// not asserted (format is operational/human-facing, not load-bearing).
			codexLogPipelineEvent("agent-log", tc.line, 3*time.Second)
		})
	}
}

// TestRunCodexFailsWhenCodexExecutableMissing verifies the earliest failure
// path: if the codex binary can't be resolved, runCodex returns an error
// message that names codex. Regression target: a reorder that tries to
// write the MCP config before the binary check would surface as the test
// observing a different error message ("failed to write MCP config"
// instead of "codex..."). The test does not attempt to verify "no
// tempfiles created" — that would require listing os.TempDir, which is
// racy against sibling tests using the same prefix.
func TestRunCodexFailsWhenCodexExecutableMissing(t *testing.T) {
	// Scrub PATH so exec.LookPath("codex") fails deterministically.
	t.Setenv("PATH", "/nonexistent-path-for-codex-runner-test")

	pe := &PipelineEngine{}
	out, err := pe.runCodex(context.Background(), "agent-1", "prompt", "sys", "profile")
	if err == nil {
		t.Fatalf("runCodex returned nil error, want codex-not-found error")
	}
	if out != "" {
		t.Fatalf("runCodex output = %q, want empty on pre-exec failure", out)
	}
	if !strings.Contains(err.Error(), "codex") {
		t.Fatalf("error %q should mention codex binary", err.Error())
	}
}

// TestRunCodexFailsWhenBrokerSecretMissing verifies that a missing
// BROKER_SECRET fails the step *before* bwrap is spawned. The MCP config
// writer returns an error (since we need the secret to authenticate the
// broker child), and runCodex surfaces it wrapped with "failed to write
// MCP config". Critical because without this gate the sandbox would
// launch with an empty secret and every broker tool call would 401.
func TestRunCodexFailsWhenBrokerSecretMissing(t *testing.T) {
	// Provide fake codex + mcp-broker-server so the earlier resolve steps
	// succeed and we reach writeCodexMCPConfig.
	binDir := t.TempDir()
	for _, name := range []string{"codex", "mcp-broker-server", "bwrap"} {
		path := filepath.Join(binDir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}
	t.Setenv("PATH", binDir)

	// Clear any BROKER_SECRET the test runner might have inherited.
	t.Setenv("BROKER_SECRET", "")
	os.Unsetenv("BROKER_SECRET")

	// HOME with no ~/.codex so sanitizeHostCodexConfigForMerge sees an
	// empty host config.
	t.Setenv("HOME", t.TempDir())

	// pe.db is nil, so the stored-secret fallback in writeCodexMCPConfig
	// also yields empty — the "BROKER_SECRET not available" branch fires.
	pe := &PipelineEngine{}
	out, err := pe.runCodex(context.Background(), "agent-1", "prompt", "sys", "profile")
	if err == nil {
		t.Fatalf("runCodex returned nil error, want BROKER_SECRET error")
	}
	if !strings.Contains(err.Error(), "MCP config") {
		t.Fatalf("error %q should wrap MCP config failure", err.Error())
	}
	if !strings.Contains(err.Error(), "BROKER_SECRET") {
		t.Fatalf("error %q should name BROKER_SECRET as the missing input", err.Error())
	}
	if out != "" {
		t.Fatalf("runCodex output = %q, want empty on pre-exec failure", out)
	}
}

// TestWriteCodexMCPConfigPullsBrokerSecretFromDB exercises the DB-fallback
// branch of writeCodexMCPConfig: when BROKER_SECRET is not in the
// environment but is stored in the settings table, the merged config
// should include the stored value rather than failing. This is the
// production boot path (the manager persists the auto-generated secret
// and expects subsequent process starts to read it from the DB).
func TestWriteCodexMCPConfigPullsBrokerSecretFromDB(t *testing.T) {
	// Fake mcp-broker-server so resolveMCPBrokerServerBinary succeeds.
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "mcp-broker-server"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake mcp-broker-server: %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	// Env-side secret unset; DB side provides the value.
	t.Setenv("BROKER_SECRET", "")
	os.Unsetenv("BROKER_SECRET")

	db := setupManagerTestDB(t)
	if err := SetSetting(context.Background(), db, "broker_secret", "db-stored-secret"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	pe := &PipelineEngine{db: db}
	path, _, cleanup, err := pe.writeCodexMCPConfig("agent-db", "", nil, false)
	if err != nil {
		t.Fatalf("writeCodexMCPConfig returned err=%v, expected DB-fallback to succeed", err)
	}
	defer cleanup()

	merged, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read merged config: %v", err)
	}
	if !strings.Contains(string(merged), `BROKER_SECRET = "db-stored-secret"`) {
		t.Fatalf("merged config missing DB-sourced broker secret:\n%s", merged)
	}
}

// TestCodexSemaphoreReturnsUsableInstance smoke-tests the semaphore factory.
// EnvSemaphore caches globally by env key, so we can't cleanly assert that a
// specific CODEX_MAX_CONCURRENT value propagated (earlier test runs in the
// same process would have already cached whatever was in place). We settle
// for the thinner contract: the returned semaphore is non-nil and round-trips
// Acquire/Release without blocking forever on a cancelled context — enough
// to catch a regression that accidentally returns nil or a zero-capacity
// semaphore.
//
// Acquire uses a bounded ctx rather than context.Background() specifically
// because the semaphore is a process-global cache: if a sibling test in the
// same binary leaked a token (or ran in parallel and drained capacity), an
// unbounded Acquire here would hang the suite instead of failing cleanly.
func TestCodexSemaphoreReturnsUsableInstance(t *testing.T) {
	sema := codexSemaphore()
	if sema == nil {
		t.Fatalf("codexSemaphore returned nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sema.Acquire(ctx); err != nil {
		t.Fatalf("Acquire on cached semaphore returned %v — capacity exhausted by a sibling test?", err)
	}
	sema.Release()
}

// TestCodexRunnerSpecBuildCorrectionPromptClosure directly exercises the
// buildCorrectionPrompt closure stored in the spec. The closure's only job
// is to call buildPipelineCorrectionPrompt with pipelineRunnerCorrectionMaxChars;
// the pairing matters because an uncalled closure in production would pass
// 0 for maxChars, disable truncation, and let a 100 KB prior response flow
// into the retry prompt (blowing past the LLM context window).
func TestCodexRunnerSpecBuildCorrectionPromptClosure(t *testing.T) {
	spec := codexRunnerSpec(&PipelineEngine{}, "profile", nil)

	// Truncation is applied when prior is longer than
	// pipelineRunnerCorrectionMaxChars; pick a length well over the limit to
	// avoid coupling the test to the exact constant.
	longPrior := strings.Repeat("A", pipelineRunnerCorrectionMaxChars+500)
	got := spec.buildCorrectionPrompt("bad status value", longPrior, nil)

	if !strings.Contains(got, "Error: bad status value") {
		t.Fatalf("correction prompt missing failure reason: %q", got)
	}
	if !strings.Contains(got, "(truncated)") {
		t.Fatalf("correction prompt should truncate oversized prior response: %q", got)
	}
	if !strings.Contains(got, "Reformat this into the required JSON envelope") {
		t.Fatalf("correction prompt missing reformat instructions: %q", got)
	}
}

// TestInvokeCodexRunnerUsesSemaphoreWhenNoTestSeam covers the production
// branch of invokeCodexRunner: when pe.codexRunner is nil, the semaphore is
// acquired before runCodex runs. The test *actually* verifies semaphore use
// by draining every token on the globally-cached codex semaphore first, then
// calling invokeCodexRunner with a short-deadline context: if Acquire is
// called (correct), it blocks and returns the ctx error; if Acquire is
// bypassed (regression), invokeCodexRunner would proceed to runCodex and
// return a codex-not-found error instead. The two failure modes are
// distinguishable, which is the whole point — a weaker test that only
// checks for any error would silently pass on the bypass regression.
//
// Also pins: onAcquire must NOT run when Acquire fails (fan-out branches
// stay queued so the orphan detector skips them).
func TestInvokeCodexRunnerUsesSemaphoreWhenNoTestSeam(t *testing.T) {
	t.Setenv("PATH", "/nonexistent-for-codex-sema-test")

	// Drain the globally-cached semaphore. EnvSemaphore is keyed only by
	// env var name, so this instance is shared across the whole test
	// binary — we must return every token on cleanup or sibling tests that
	// rely on the semaphore will hang forever.
	sema := codexSemaphore()
	held := 0
	for {
		probeCtx, probeCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		err := sema.Acquire(probeCtx)
		probeCancel()
		if err != nil {
			break
		}
		held++
		if held > 1024 {
			t.Fatalf("semaphore appears unbounded (drained >1024 tokens)")
		}
	}
	t.Cleanup(func() {
		for i := 0; i < held; i++ {
			sema.Release()
		}
	})
	if held == 0 {
		t.Skip("could not reserve any semaphore capacity — another test is holding all tokens")
	}

	pe := &PipelineEngine{}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	activateCalled := 0
	_, err := pe.invokeCodexRunner(ctx, "agent", "prompt", "sys", "profile", func() {
		activateCalled++
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	// Correct behavior: Acquire blocks, ctx deadline fires, err is context.*.
	// Regression (Acquire bypassed): would reach runCodex and return the
	// codex-not-found error instead.
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v; want context error — invokeCodexRunner may have bypassed sema.Acquire", err)
	}
	if activateCalled != 0 {
		t.Fatalf("onAcquire ran %d times despite Acquire failing — fan-out step-runs must stay queued", activateCalled)
	}
}

// TestInvokeCodexRunnerSemaphoreHappyPathRunsOnAcquireAndCallsRunCodex
// complements TestInvokeCodexRunnerUsesSemaphoreWhenNoTestSeam by covering
// the post-Acquire branch: when the semaphore has capacity, Acquire
// succeeds, onAcquire runs (before real work), runCodex is invoked, and
// Release fires via defer. The strengthened sibling test verifies Acquire
// is called at all; this one covers what happens after.
//
// We keep the work light by scrubbing PATH so runCodex fails fast at
// resolveCodexExecutable; the whole code path still exercises lines
// 122-132 of invokeCodexRunner.
func TestInvokeCodexRunnerSemaphoreHappyPathRunsOnAcquireAndCallsRunCodex(t *testing.T) {
	t.Setenv("PATH", "/nonexistent-for-codex-happy-sema-test")

	pe := &PipelineEngine{}
	// pe.codexRunner intentionally nil — force the real-semaphore path.

	activateCalled := 0
	_, err := pe.invokeCodexRunner(context.Background(), "agent", "prompt", "sys", "profile", func() {
		activateCalled++
	})
	if err == nil {
		t.Fatalf("expected codex-not-found error, got nil")
	}
	if !strings.Contains(err.Error(), "codex") {
		t.Fatalf("error %q should mention codex (proves runCodex was reached)", err.Error())
	}
	// onAcquire must run post-Acquire, pre-runCodex — it's what transitions
	// the step-run to "running" for fan-out branches.
	if activateCalled != 1 {
		t.Fatalf("onAcquire called %d times, want 1 on happy Acquire path", activateCalled)
	}
}

// TestRunCodexFailsWhenBwrapMissing covers the build-command failure branch
// of runCodex: codex and mcp-broker-server are on PATH, BROKER_SECRET is set,
// but bwrap is absent. writeCodexMCPConfig succeeds (so cleanup runs), then
// buildCodexPipelineSandboxCommand fails with "bwrap executable not found"
// and runCodex returns that error.
func TestRunCodexFailsWhenBwrapMissing(t *testing.T) {
	binDir := t.TempDir()
	for _, name := range []string{"codex", "mcp-broker-server"} {
		path := filepath.Join(binDir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}
	t.Setenv("PATH", binDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BROKER_SECRET", "secret")

	pe := &PipelineEngine{}
	_, err := pe.runCodex(context.Background(), "agent-bwrap", "prompt", "sys", "profile")
	if err == nil {
		t.Fatalf("expected bwrap-not-found error, got nil")
	}
	if !strings.Contains(err.Error(), "bwrap") {
		t.Fatalf("error %q should mention bwrap", err.Error())
	}
}

// TestRunCodexStreamsStdoutAndReturnsOnZeroExit covers the happy path of
// runCodex end-to-end using a fake "bwrap" shell script that ignores all
// its arguments and emits a well-formed codex JSONL stream to stdout. This
// exercises: the bufio.Scanner stdout streaming loop, the stderr drain
// goroutine, cmd.Wait() with exit 0, and the final return of the
// concatenated stdout.
func TestRunCodexStreamsStdoutAndReturnsOnZeroExit(t *testing.T) {
	binDir := t.TempDir()
	fakeBwrap := `#!/bin/sh
echo '{"type":"turn.started"}'
echo '{"type":"item.completed","item":{"type":"agent_message","text":"{\"status\":\"succeeded\",\"result\":{\"ok\":true}}"}}'
echo '{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":5}}'
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "bwrap"), []byte(fakeBwrap), 0755); err != nil {
		t.Fatalf("write fake bwrap: %v", err)
	}
	for _, name := range []string{"codex", "mcp-broker-server"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}
	t.Setenv("PATH", binDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BROKER_SECRET", "secret")

	pe := &PipelineEngine{}
	out, err := pe.runCodex(context.Background(), "agent-happy", "prompt", "sys", "profile")
	if err != nil {
		t.Fatalf("runCodex returned err=%v, want nil on exit 0", err)
	}
	for _, expected := range []string{
		`"type":"turn.started"`,
		`"type":"item.completed"`,
		`"type":"turn.completed"`,
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("stdout missing %q: %q", expected, out)
		}
	}

	// Downstream parse step must accept the captured stdout; a regression in
	// either the stdout capture or the parser's input contract breaks this.
	spec := codexRunnerSpec(pe, "profile", nil)
	parsed := spec.parse(out)
	if parsed.Status != "succeeded" {
		t.Fatalf("parsed.Status = %q, want succeeded (parse=%+v)", parsed.Status, parsed)
	}
}

// TestRunCodexReturnsExecutionErrorOnNonZeroExit covers the ExitError branch
// of runCodex: fake bwrap writes to stderr then exits non-zero.
// Expected: runCodex returns a *codexllm.ExecutionError whose ExitCode and
// Stdout are populated.
//
// Stderr content is deliberately NOT asserted: runCodex reads the stderr
// buffer (populated by a background goroutine) immediately after cmd.Wait()
// without explicit synchronization between the drain-goroutine exit and the
// buffer read — a pre-existing race in codex_runner.go:177-187,201-202.
// Under contention the buffer may be empty or partial when read, which
// would manifest here as a flaky Stderr-content assertion. Fixing the race
// requires a production-code change (sync.WaitGroup around the stderr
// goroutine) that's out of scope for this test file; asserting only the
// deterministic fields (ExitCode, Stdout — stdout is read synchronously in
// the main goroutine until EOF, so no race) keeps the test stable.
func TestRunCodexReturnsExecutionErrorOnNonZeroExit(t *testing.T) {
	binDir := t.TempDir()
	fakeBwrap := `#!/bin/sh
echo '{"type":"turn.started"}'
echo "codex: something broke" 1>&2
exit 42
`
	if err := os.WriteFile(filepath.Join(binDir, "bwrap"), []byte(fakeBwrap), 0755); err != nil {
		t.Fatalf("write fake bwrap: %v", err)
	}
	for _, name := range []string{"codex", "mcp-broker-server"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}
	t.Setenv("PATH", binDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BROKER_SECRET", "secret")

	pe := &PipelineEngine{}
	out, err := pe.runCodex(context.Background(), "agent-fail", "prompt", "sys", "profile")
	if err == nil {
		t.Fatalf("expected ExecutionError, got nil (stdout=%q)", out)
	}
	var execErr *codexllm.ExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("error type = %T, want *codexllm.ExecutionError", err)
	}
	if execErr.ExitCode != 42 {
		t.Fatalf("ExitCode = %d, want 42", execErr.ExitCode)
	}
	if !strings.Contains(execErr.Stdout, `"type":"turn.started"`) {
		t.Fatalf("Stdout missing captured event: %q", execErr.Stdout)
	}
	if !strings.Contains(out, `"type":"turn.started"`) {
		t.Fatalf("returned stdout missing captured event: %q", out)
	}
}

// TestRunCodexReturnsExecutionErrorOnContextTimeout covers the
// DeadlineExceeded branch: the context times out before bwrap exits,
// and runCodex classifies the failure as a timeout (not a generic exec
// failure) so operators can distinguish timeouts from legitimate non-zero
// exits downstream.
func TestRunCodexReturnsExecutionErrorOnContextTimeout(t *testing.T) {
	binDir := t.TempDir()
	// Fake bwrap uses `exec sleep 30` — the shell replaces itself with sleep,
	// so when exec.CommandContext SIGKILLs the process on timeout, sleep
	// (now PID of the shell) dies immediately. Without `exec`, the shell is
	// killed but its forked sleep child inherits the stdout pipe and keeps
	// scanner.Scan() blocked until sleep naturally exits — which would make
	// this test take ~30s instead of timing out cleanly.
	fakeBwrap := `#!/bin/sh
exec sleep 30
`
	if err := os.WriteFile(filepath.Join(binDir, "bwrap"), []byte(fakeBwrap), 0755); err != nil {
		t.Fatalf("write fake bwrap: %v", err)
	}
	for _, name := range []string{"codex", "mcp-broker-server"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BROKER_SECRET", "secret")

	// 2s timeout is deliberate: enough slack for cold-start setup on slow
	// CI / under -race (writeCodexMCPConfig, MkdirTemp, filepath.Abs, fork,
	// shell startup) so the deadline fires DURING `exec sleep 30` rather
	// than during setup — otherwise the test could hit a context error on
	// an earlier code path and silently test the wrong branch. The upper
	// bound is sleep 30s, so even a 10s-slow CI wouldn't mis-classify.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pe := &PipelineEngine{}
	_, err := pe.runCodex(ctx, "agent-timeout", "prompt", "sys", "profile")
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	var execErr *codexllm.ExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("error type = %T, want *codexllm.ExecutionError", err)
	}
	if !strings.Contains(execErr.Message, "timed out") {
		t.Fatalf("ExecutionError.Message = %q, want to mention timeout", execErr.Message)
	}
}

// TestBuildCodexPipelineSandboxCommandRejectsEmptyMCPPaths pins the arg
// validation branches of buildCodexPipelineSandboxCommand: empty MCP config
// path and empty MCP broker binary path both fail before any mkdir. A
// regression that skipped this check would emit a bwrap command with an
// empty --ro-bind source, which bwrap silently ignores — the resulting
// codex run would have no MCP broker at all.
func TestBuildCodexPipelineSandboxCommandRejectsEmptyMCPPaths(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "bwrap"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake bwrap: %v", err)
	}
	codexPath := filepath.Join(binDir, "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	t.Run("empty mcp config path", func(t *testing.T) {
		_, cleanup, err := buildCodexPipelineSandboxCommand(
			context.Background(),
			"prompt",
			"sys",
			"profile",
			"   ",
			"/tmp/broker-server",
			codexPath,
			false,
		)
		if cleanup != nil {
			cleanup()
		}
		if err == nil {
			t.Fatalf("expected error for empty mcp config path, got nil")
		}
		if !strings.Contains(err.Error(), "mcp config path") {
			t.Fatalf("error %q should mention mcp config path", err.Error())
		}
	})

	t.Run("empty mcp broker binary path", func(t *testing.T) {
		_, cleanup, err := buildCodexPipelineSandboxCommand(
			context.Background(),
			"prompt",
			"sys",
			"profile",
			"/tmp/config.toml",
			"",
			codexPath,
			false,
		)
		if cleanup != nil {
			cleanup()
		}
		if err == nil {
			t.Fatalf("expected error for empty broker binary path, got nil")
		}
		if !strings.Contains(err.Error(), "broker binary") {
			t.Fatalf("error %q should mention broker binary", err.Error())
		}
	})
}

// TestBuildCodexPipelineSandboxCommandFailsWhenBwrapMissing covers the
// earliest failure branch of buildCodexPipelineSandboxCommand:
// resolveBwrapExecutable fails and the function returns a bwrap-named
// error. The test asserts on the error message, not on tempdir cleanup —
// verifying the latter would require listing /tmp and filtering, which is
// racy against sibling tests using the same scratch-dir prefix. The
// ordering invariant (bwrap resolve before mkdir) is enforced at the
// implementation level by the line order in codex_runner.go, and a
// regression moving mkdir above would be caught here via the error
// message changing.
func TestBuildCodexPipelineSandboxCommandFailsWhenBwrapMissing(t *testing.T) {
	t.Setenv("PATH", "/nonexistent-bwrap-check")
	_, cleanup, err := buildCodexPipelineSandboxCommand(
		context.Background(),
		"prompt",
		"sys",
		"profile",
		"/tmp/config.toml",
		"/tmp/broker",
		"/tmp/codex",
		false,
	)
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatalf("expected bwrap-not-found error, got nil")
	}
	if !strings.Contains(err.Error(), "bwrap") {
		t.Fatalf("error %q should mention bwrap", err.Error())
	}
}

// TestWriteCodexMCPConfigJoinsDisabledToolsCSV verifies the DISABLED_TOOLS
// env var is emitted as a comma-separated list inside the broker env block.
// Codex-side the MCP broker child parses this to decide which tool calls to
// reject; dropping or re-ordering the encoding silently unblocks disabled
// tools inside the sandbox.
func TestWriteCodexMCPConfigJoinsDisabledToolsCSV(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "mcp-broker-server"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake mcp-broker-server: %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BROKER_SECRET", "env-secret")

	pe := &PipelineEngine{}
	path, _, cleanup, err := pe.writeCodexMCPConfig(
		"agent-tools",
		"pipeline-step",
		[]string{"get_wallet_address", "send_token", "broker_list"},
		false,
	)
	if err != nil {
		t.Fatalf("writeCodexMCPConfig returned err=%v", err)
	}
	defer cleanup()

	merged, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read merged config: %v", err)
	}
	want := `DISABLED_TOOLS = "get_wallet_address,send_token,broker_list"`
	if !strings.Contains(string(merged), want) {
		t.Fatalf("merged config missing %q:\n%s", want, merged)
	}
}
