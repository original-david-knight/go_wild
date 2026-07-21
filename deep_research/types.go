package deepresearch

import (
	"context"
	"strings"
	"time"
)

// ObjectiveStatus describes whether the engine gathered enough evidence
// for a specific objective.
type ObjectiveStatus string

const (
	ObjectiveStatusMissing   ObjectiveStatus = "missing"
	ObjectiveStatusPartial   ObjectiveStatus = "partial"
	ObjectiveStatusSatisfied ObjectiveStatus = "satisfied"
)

// Objective is one schema-guided research target.
type Objective struct {
	Key         string `json:"key"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// SearchRequest describes a single search operation for an objective.
type SearchRequest struct {
	Query           string   `json:"query"`
	ObjectiveKey    string   `json:"objective_key,omitempty"`
	Depth           int      `json:"depth,omitempty"`
	Limit           int      `json:"limit,omitempty"`
	Guidance        string   `json:"guidance,omitempty"`
	ExcludedDomains []string `json:"excluded_domains,omitempty"`
}

// SearchHit is the provider-agnostic result of a search request.
type SearchHit struct {
	URL         string    `json:"url"`
	Title       string    `json:"title,omitempty"`
	Snippet     string    `json:"snippet,omitempty"`
	PublishedAt time.Time `json:"published_at,omitempty"`
}

// FetchedDocument is optional long-form content fetched for a URL.
type FetchedDocument struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content,omitempty"`
}

// Finding is one evidence item in the scratchpad.
type Finding struct {
	ObjectiveKey string    `json:"objective_key"`
	Query        string    `json:"query"`
	URL          string    `json:"url"`
	Domain       string    `json:"domain,omitempty"`
	Title        string    `json:"title,omitempty"`
	Snippet      string    `json:"snippet,omitempty"`
	Excerpt      string    `json:"excerpt,omitempty"`
	Score        float64   `json:"score"`
	Depth        int       `json:"depth"`
	Rank         int       `json:"rank"`
	RetrievedAt  time.Time `json:"retrieved_at"`
	PublishedAt  time.Time `json:"published_at,omitempty"`
}

// Source is the deduplicated source list built from findings.
type Source struct {
	URL         string    `json:"url"`
	Domain      string    `json:"domain,omitempty"`
	Title       string    `json:"title,omitempty"`
	BestScore   float64   `json:"best_score"`
	PublishedAt time.Time `json:"published_at,omitempty"`
}

// ObjectiveResult summarizes evidence coverage for one objective.
type ObjectiveResult struct {
	Objective     Objective       `json:"objective"`
	Status        ObjectiveStatus `json:"status"`
	EvidenceCount int             `json:"evidence_count"`
	BestFinding   *Finding        `json:"best_finding,omitempty"`
}

// Options controls depth/parallelism/completeness behavior.
type Options struct {
	MaxDepth                int      `json:"max_depth,omitempty"`
	MaxWorkers              int      `json:"max_workers,omitempty"`
	SearchResultsPerQuery   int      `json:"search_results_per_query,omitempty"`
	MinEvidencePerObjective int      `json:"min_evidence_per_objective,omitempty"`
	MaxExcerptChars         int      `json:"max_excerpt_chars,omitempty"`
	ExcludedDomains         []string `json:"excluded_domains,omitempty"`
	SearchesPerSecond       float64  `json:"searches_per_second,omitempty"`
	TimeoutSeconds          int      `json:"timeout_seconds,omitempty"`
	LLMBackend              string   `json:"llm_backend,omitempty"` // "gemini" (default) or "claude"
}

func (o Options) withDefaults() Options {
	if o.MaxDepth <= 0 {
		o.MaxDepth = 2
	}
	if o.MaxWorkers <= 0 {
		o.MaxWorkers = 6
	}
	if o.MaxWorkers > 32 {
		o.MaxWorkers = 32
	}
	if o.SearchResultsPerQuery <= 0 {
		o.SearchResultsPerQuery = 10
	}
if o.MinEvidencePerObjective <= 0 {
		o.MinEvidencePerObjective = 5
	}
	if o.MaxExcerptChars <= 0 {
		o.MaxExcerptChars = 1200
	}
	if o.SearchesPerSecond <= 0 {
		o.SearchesPerSecond = 3.0
	}
	if o.TimeoutSeconds <= 0 {
		o.TimeoutSeconds = 300
	}
	if len(o.ExcludedDomains) > 0 {
		seen := map[string]struct{}{}
		normalized := make([]string, 0, len(o.ExcludedDomains))
		for _, domain := range o.ExcludedDomains {
			domain = strings.ToLower(strings.TrimSpace(domain))
			domain = strings.TrimPrefix(domain, "www.")
			if domain == "" {
				continue
			}
			if _, ok := seen[domain]; ok {
				continue
			}
			seen[domain] = struct{}{}
			normalized = append(normalized, domain)
		}
		o.ExcludedDomains = normalized
	}
	return o
}

// Request drives a full deep research run.
type Request struct {
	Query      string              `json:"query"`
	Objectives []Objective         `json:"objectives,omitempty"`
	Schema     map[string]any      `json:"schema,omitempty"`
	Options    Options             `json:"options,omitempty"`
	Guidance   string              `json:"guidance,omitempty"`
	Progress   func(ProgressEvent) `json:"-"`
}

// ProgressEvent is emitted during a run for live status reporting.
// Depth is the 0-indexed recursion level. Round is Depth+1 (1-indexed, human-friendly).
type ProgressEvent struct {
	Stage        string `json:"stage"`
	Round        int    `json:"round,omitempty"`
	Depth        int    `json:"depth,omitempty"`
	ObjectiveKey string `json:"objective_key,omitempty"`
	Query        string `json:"query,omitempty"`
	URL          string `json:"url,omitempty"`
	Title        string `json:"title,omitempty"`
	Rank         int    `json:"rank,omitempty"`
	Warning      string `json:"warning,omitempty"`
}

// PlannedQuery is one planner-generated search query for an objective.
type PlannedQuery struct {
	ObjectiveKey string `json:"objective_key"`
	Query        string `json:"query"`
	Rationale    string `json:"rationale,omitempty"`
}

// PlanningRequest captures engine state needed to plan the next research batch.
// Depth is the 0-indexed recursion level. Round is Depth+1 (1-indexed).
type PlanningRequest struct {
	Query             string               `json:"query"`
	Guidance          string               `json:"guidance,omitempty"`
	Schema            map[string]any       `json:"schema,omitempty"`
	Objectives        []Objective          `json:"objectives,omitempty"`
	MissingObjectives []Objective          `json:"missing_objectives,omitempty"`
	Findings          map[string][]Finding `json:"findings,omitempty"`
	Sources           []Source             `json:"sources,omitempty"`
	Warnings          []string             `json:"warnings,omitempty"`
	ExcludedDomains   []string             `json:"excluded_domains,omitempty"`
	Depth             int                  `json:"depth"`
	Round             int                  `json:"round"`
}

// PlanningResult is planner output for one engine round.
type PlanningResult struct {
	Queries   []PlannedQuery `json:"queries"`
	Reasoning string         `json:"reasoning,omitempty"`
}

// Planner generates targeted search queries for missing objectives.
type Planner interface {
	Plan(ctx context.Context, req PlanningRequest) (PlanningResult, error)
}

// MissingObjective describes one unresolved objective from a completeness check.
type MissingObjective struct {
	ObjectiveKey string `json:"objective_key"`
	Question     string `json:"question,omitempty"`
}

// CompletenessRequest captures current run state for completeness evaluation.
// Depth is the 0-indexed recursion level. Round is Depth+1 (1-indexed).
type CompletenessRequest struct {
	Query            string               `json:"query"`
	Guidance         string               `json:"guidance,omitempty"`
	Schema           map[string]any       `json:"schema,omitempty"`
	Objectives       []Objective          `json:"objectives,omitempty"`
	ObjectiveResults []ObjectiveResult    `json:"objective_results,omitempty"`
	Findings         map[string][]Finding `json:"findings,omitempty"`
	Sources          []Source             `json:"sources,omitempty"`
	Warnings         []string             `json:"warnings,omitempty"`
	ExcludedDomains  []string             `json:"excluded_domains,omitempty"`
	Depth            int                  `json:"depth"`
	Round            int                  `json:"round"`
}

// CompletenessResult is the output of a completeness checker.
type CompletenessResult struct {
	Complete          bool               `json:"complete"`
	MissingObjectives []MissingObjective `json:"missing_objectives,omitempty"`
	Reasoning         string             `json:"reasoning,omitempty"`
}

// CompletenessChecker decides if research is sufficient and what remains.
type CompletenessChecker interface {
	Check(ctx context.Context, req CompletenessRequest) (CompletenessResult, error)
}

// SynthesisRequest captures state for final schema-grounded synthesis.
type SynthesisRequest struct {
	Query            string               `json:"query"`
	Guidance         string               `json:"guidance,omitempty"`
	Schema           map[string]any       `json:"schema,omitempty"`
	Objectives       []Objective          `json:"objectives,omitempty"`
	ObjectiveResults []ObjectiveResult    `json:"objective_results,omitempty"`
	Findings         map[string][]Finding `json:"findings,omitempty"`
	Sources          []Source             `json:"sources,omitempty"`
	Warnings         []string             `json:"warnings,omitempty"`
	ExcludedDomains  []string             `json:"excluded_domains,omitempty"`
	Rounds           int                  `json:"rounds"`
}

// SynthesisResult is final structured synthesis output.
type SynthesisResult struct {
	Output  any    `json:"output,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// Synthesizer builds a final structured output for the target schema.
type Synthesizer interface {
	Synthesize(ctx context.Context, req SynthesisRequest) (SynthesisResult, error)
}

// Result is the full output of a deep research run.
// Rounds is the total number of search rounds executed (1-indexed: a single-pass run has Rounds=1).
type Result struct {
	Query                string               `json:"query"`
	Objectives           []ObjectiveResult    `json:"objectives"`
	Findings             map[string][]Finding `json:"findings"`
	Sources              []Source             `json:"sources"`
	Summary              string               `json:"summary"`
	Rounds               int                  `json:"rounds"`
	SchemaSatisfied      bool                 `json:"schema_satisfied"`
	MissingObjectiveKeys []string             `json:"missing_objective_keys,omitempty"`
	Output               any                  `json:"output,omitempty"`
	Warnings             []string             `json:"warnings,omitempty"`
	StartedAt            time.Time            `json:"started_at"`
	FinishedAt           time.Time            `json:"finished_at"`
}

// Searcher supplies web search capabilities.
type Searcher interface {
	Search(ctx context.Context, req SearchRequest) ([]SearchHit, error)
}

// Fetcher supplies optional page/document fetch capabilities.
type Fetcher interface {
	Fetch(ctx context.Context, url string) (FetchedDocument, error)
}
