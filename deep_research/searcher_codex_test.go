package deepresearch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/original-david-knight/go_wild/codexllm"
)

// TestDefaultCodexSearcherEnablesWebSearch is the regression guard for the fix
// in searcher_codex.go: without WebSearch=true on the client, codex won't
// actually get the native web_search tool and will silently hallucinate URLs.
// This test fails if someone reverts that field.
func TestDefaultCodexSearcherEnablesWebSearch(t *testing.T) {
	clearCodexProfileEnv(t)
	t.Setenv("DEEP_RESEARCH_CODEX_SEARCH_PROFILE", "phase-search")

	s, err := DefaultCodexSearcher()
	if err != nil {
		t.Fatalf("DefaultCodexSearcher() error = %v", err)
	}
	if !s.client.WebSearch {
		t.Fatalf("searcher client must have WebSearch=true to receive the native web_search tool; client=%#v", s.client)
	}
	if !s.client.RequireWebSearchUse {
		t.Fatalf("searcher client must also have RequireWebSearchUse=true so a no-search answer is treated as hallucination; client=%#v", s.client)
	}
}

// TestNewCodexSearcherForcesWebSearchOnInjectedClient closes the bypass
// Codex flagged: a caller (or lazy test) passing a Client{} without the
// web-search flags must still get a searcher that refuses to silently
// hallucinate. The constructor takes a shallow COPY of the injected client
// and enables the flags on the copy, so the caller's original pointer is
// never mutated — other code paths sharing that client keep their original
// behavior, and we avoid the race that would exist if RequireWebSearchUse
// were flipped on a concurrently-used struct.
func TestNewCodexSearcherForcesWebSearchOnInjectedClient(t *testing.T) {
	clearCodexProfileEnv(t)
	injected := &codexllm.Client{Profile: "caller-chose-this", Label: "test"} // flags default false
	s := newCodexSearcher(injected)
	// Searcher must hold a DIFFERENT pointer — a shallow copy, not the
	// caller's original.
	if s.client == injected {
		t.Fatalf("searcher must hold a copy, not the caller's pointer; got same address %p", injected)
	}
	// The copy must have the searcher's required flags.
	if !s.client.WebSearch || !s.client.RequireWebSearchUse {
		t.Fatalf("searcher's copy must have WebSearch and RequireWebSearchUse set; got %#v", s.client)
	}
	// Caller-supplied non-flag fields must carry over to the copy.
	if s.client.Profile != "caller-chose-this" {
		t.Fatalf("copy should carry caller's Profile; got %q", s.client.Profile)
	}
	// Caller's original must be UNTOUCHED — this is the whole point of the
	// shallow-copy. Mutating it would silently flip behavior for every
	// other consumer of the same client.
	if injected.WebSearch || injected.RequireWebSearchUse {
		t.Fatalf("constructor must NOT mutate caller's client; injected=%#v", injected)
	}
}

// TestCodexSearcherSearchSurfacesGenerateError covers the end-to-end failure
// path: when the underlying Client.Generate returns an error (e.g. because
// codex produced no web_search events and the WebSearch guard fired),
// Search must propagate that error rather than returning empty hits.
func TestCodexSearcherSearchSurfacesGenerateError(t *testing.T) {
	wantErr := errors.New("codex: refusing to return potentially hallucinated results")
	s := &codexDeepResearchSearcher{
		generate: func(context.Context, string, string) (string, []string, error) {
			return "", nil, wantErr
		},
	}

	hits, err := s.Search(context.Background(), SearchRequest{Query: "anything"})
	if err == nil {
		t.Fatalf("expected error to propagate, got hits=%v", hits)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error chain should wrap the generate error; got %v", err)
	}
}

// TestCodexSearcherSearchParsesResults verifies the happy-path parse so the
// prompt change (now demanding the web_search tool) can't silently break
// deserialization of the JSON envelope. All returned URLs are listed in the
// observed set so attestation passes — the parse assertions are what the
// test is actually proving.
func TestCodexSearcherSearchParsesResults(t *testing.T) {
	payload := `{"results":[
        {"url":"https://a.example","title":"A","snippet":"s1","published_at":"2026-01-01T00:00:00Z"},
        {"url":"https://b.example","title":"B","snippet":"s2"},
        {"url":"https://a.example","title":"dup","snippet":"dup"}
    ]}`
	observed := []string{"https://a.example", "https://b.example"}
	s := &codexDeepResearchSearcher{
		generate: func(context.Context, string, string) (string, []string, error) {
			return payload, observed, nil
		},
	}

	hits, err := s.Search(context.Background(), SearchRequest{Query: "q", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 unique hits (duplicate URL suppressed), got %d: %+v", len(hits), hits)
	}
	if hits[0].URL != "https://a.example" || hits[0].PublishedAt.IsZero() {
		t.Fatalf("first hit malformed: %+v", hits[0])
	}
	if hits[1].URL != "https://b.example" {
		t.Fatalf("second hit URL = %q", hits[1].URL)
	}
}

// TestCodexSearcherRejectsFabricatedURLs is the core anti-hallucination test:
// even with a web_search event present (satisfying the existence-only guard
// in codexllm.Client), a URL the model emits that does not appear in the
// observed/open_page set must be treated as fabricated. When ALL candidate
// URLs are fabricated, Search returns an error that names the rejection —
// not silently-empty hits, which would mask the bug.
func TestCodexSearcherRejectsFabricatedURLs(t *testing.T) {
	payload := `{"results":[
        {"url":"https://fabricated.example/one","title":"fake","snippet":"s"},
        {"url":"https://fabricated.example/two","title":"fake","snippet":"s"}
    ]}`
	// Model emitted no open_page actions → observed set is empty.
	s := &codexDeepResearchSearcher{
		generate: func(context.Context, string, string) (string, []string, error) {
			return payload, nil, nil
		},
	}
	hits, err := s.Search(context.Background(), SearchRequest{Query: "q"})
	if err == nil {
		t.Fatalf("expected fabrication error, got hits=%v", hits)
	}
	if !strings.Contains(err.Error(), "fabricating") && !strings.Contains(err.Error(), "attested") {
		t.Fatalf("error should name the fabrication/attestation failure mode; got %v", err)
	}
}

// TestCodexSearcherDropsFabricatedURLsButKeepsAttested covers the partial-
// fabrication path: the model returns a mix of real (attested) and invented
// URLs. The attested ones must survive; the invented ones must be silently
// dropped. This is the whole point of the cross-check — the pipeline should
// degrade gracefully, not fail whenever the model sprinkles in a fake.
func TestCodexSearcherDropsFabricatedURLsButKeepsAttested(t *testing.T) {
	payload := `{"results":[
        {"url":"https://real.example/page","title":"real","snippet":"s"},
        {"url":"https://fake.example/made-up","title":"fake","snippet":"s"},
        {"url":"https://also-real.example/doc","title":"real2","snippet":"s"}
    ]}`
	observed := []string{"https://real.example/page", "https://also-real.example/doc"}
	s := &codexDeepResearchSearcher{
		generate: func(context.Context, string, string) (string, []string, error) {
			return payload, observed, nil
		},
	}
	hits, err := s.Search(context.Background(), SearchRequest{Query: "q", Limit: 10})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 attested hits (fake dropped), got %d: %+v", len(hits), hits)
	}
	for _, h := range hits {
		if strings.Contains(h.URL, "fake") {
			t.Fatalf("fabricated URL survived attestation: %s", h.URL)
		}
	}
}

// TestCodexSearcherAttestationToleratesNormalization confirms the cosmetic-
// variation rules in urlAttested: trailing slash differences on either side
// and URL fragments are normalized away so honest runs don't fail on
// canonicalization quirks codex introduces. Query strings are NOT normalized
// — see TestCodexSearcherAttestationRejectsQueryStringBypass for why.
func TestCodexSearcherAttestationToleratesNormalization(t *testing.T) {
	cases := []struct {
		name     string
		observed string
		returned string
	}{
		{"trailing slash on returned", "https://ex.example/docs", "https://ex.example/docs/"},
		{"trailing slash on observed", "https://ex.example/docs/", "https://ex.example/docs"},
		{"fragment stripped", "https://ex.example/docs", "https://ex.example/docs#intro"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := `{"results":[{"url":"` + tc.returned + `","title":"t","snippet":"s"}]}`
			s := &codexDeepResearchSearcher{
				generate: func(context.Context, string, string) (string, []string, error) {
					return payload, []string{tc.observed}, nil
				},
			}
			hits, err := s.Search(context.Background(), SearchRequest{Query: "q"})
			if err != nil {
				t.Fatalf("unexpected fabrication rejection: %v", err)
			}
			if len(hits) != 1 {
				t.Fatalf("expected 1 attested hit, got %d", len(hits))
			}
		})
	}
}

// TestCodexSearcherAttestationRejectsQueryStringBypass is the regression
// guard for the P1 codex review finding: previously the attestation code
// stripped the candidate query string and re-matched against the base URL,
// which let an open_page of `https://ex.example/docs` attest an arbitrary
// fabricated URL like `https://ex.example/docs?market=evil-slug`.
//
// On a huge class of real sites the query selects the resource — GitHub
// gist `?id=...`, YouTube `?v=...`, Polymarket `?market=...`, signed S3
// URLs, paginated APIs. A query-strip bypass therefore let the model
// substitute a different resource and have it pass attestation. The
// correct behavior is strict equality: if the model wants to return a
// query-bearing URL, it must open_page exactly that URL.
func TestCodexSearcherAttestationRejectsQueryStringBypass(t *testing.T) {
	cases := []struct {
		name     string
		observed string
		returned string
	}{
		{
			name:     "query string appended to opened base is NOT attested",
			observed: "https://ex.example/docs",
			returned: "https://ex.example/docs?market=evil-slug",
		},
		{
			name:     "different query string on opened URL is NOT attested",
			observed: "https://ex.example/docs?v=alpha",
			returned: "https://ex.example/docs?v=beta",
		},
		{
			name:     "bare path vs query-bearing attestation is NOT attested",
			observed: "https://ex.example/docs?id=safe",
			returned: "https://ex.example/docs",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := `{"results":[{"url":"` + tc.returned + `","title":"t","snippet":"s"}]}`
			s := &codexDeepResearchSearcher{
				generate: func(context.Context, string, string) (string, []string, error) {
					return payload, []string{tc.observed}, nil
				},
			}
			hits, err := s.Search(context.Background(), SearchRequest{Query: "q"})
			if err == nil {
				t.Fatalf("query-string bypass must be rejected; got hits=%v", hits)
			}
			if !strings.Contains(err.Error(), "attested") && !strings.Contains(err.Error(), "fabricating") {
				t.Fatalf("error should name the fabrication failure; got %v", err)
			}
		})
	}
}

// TestCodexSearcherAttestationRejectsDifferentHost guards the flip side of
// the normalization rules: attestation must NOT match across different
// hosts, paths, or schemes. A strict prefix check on raw string equality
// would be too lax — we want `urlAttested` to reject `https://evil.example`
// even when `https://ev.example` was observed.
func TestCodexSearcherAttestationRejectsDifferentHost(t *testing.T) {
	payload := `{"results":[{"url":"https://evil.example/docs","title":"t","snippet":"s"}]}`
	s := &codexDeepResearchSearcher{
		generate: func(context.Context, string, string) (string, []string, error) {
			return payload, []string{"https://ev.example/docs"}, nil
		},
	}
	hits, err := s.Search(context.Background(), SearchRequest{Query: "q"})
	if err == nil {
		t.Fatalf("expected rejection of URL on different host; got hits=%v", hits)
	}
}

// TestCodexSearcherPromptMandatesOpenPage locks in the prompt instruction
// that the model must call open_page on every URL it returns. Without that
// instruction the attestation check would reject almost everything an
// honest run produces — regressing the prompt would manifest as mysterious
// "no attested results" failures in production.
func TestCodexSearcherPromptMandatesOpenPage(t *testing.T) {
	prompt := deepResearchCodexSearchPrompt(SearchRequest{Query: "x"}, 3)
	for _, want := range []string{"open_page", "rejected", "fabricated"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing required substring %q:\n%s", want, prompt)
		}
	}
}

// TestDeepResearchCodexSearchPromptMandatesWebSearch locks in the prompt
// language that tells codex to call the native web_search tool and refuse
// to fabricate. If someone softens this wording, the silent-hallucination
// risk resurfaces even if the WebSearch flag stays on.
func TestDeepResearchCodexSearchPromptMandatesWebSearch(t *testing.T) {
	prompt := deepResearchCodexSearchPrompt(SearchRequest{
		Query:           "moon landing",
		Guidance:        "find primary sources",
		ObjectiveKey:    "obj-1",
		Depth:           2,
		ExcludedDomains: []string{"spam.example"},
	}, 5)

	mustContain := []string{
		"web_search",      // names the actual tool
		"at least once",   // forbids "answer from memory"
		"fabricating",     // explicit anti-hallucination instruction
		"spam.example",    // excluded domains flow through
		"moon landing",    // query flows through
		"find primary",    // guidance flows through as "Problem:"
	}
	for _, s := range mustContain {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt missing required substring %q\nprompt:\n%s", s, prompt)
		}
	}
}

// compile-time sanity: codexllm.Client.WebSearch must exist and be a bool.
// If the field is removed or renamed, the searcher fix is gone.
var _ = func() bool { var c codexllm.Client; return c.WebSearch }
