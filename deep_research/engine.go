package deepresearch

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Engine executes schema-guided recursive research rounds.
type Engine struct {
	searcher    Searcher
	fetcher     Fetcher
	planner     Planner
	checker     CompletenessChecker
	synthesizer Synthesizer
	nowFn       func() time.Time
}

func newEngine(searcher Searcher, fetcher Fetcher) *Engine {
	return &Engine{
		searcher: searcher,
		fetcher:  fetcher,
		nowFn:    time.Now,
	}
}

func newEngineWithPlanner(searcher Searcher, fetcher Fetcher, planner Planner) *Engine {
	engine := newEngine(searcher, fetcher)
	engine.planner = planner
	return engine
}

// NewEngineWithReasoning constructs an engine with optional planner/checker/synthesizer.
func NewEngineWithReasoning(
	searcher Searcher,
	fetcher Fetcher,
	planner Planner,
	checker CompletenessChecker,
	synthesizer Synthesizer,
) *Engine {
	engine := newEngineWithPlanner(searcher, fetcher, planner)
	engine.checker = checker
	engine.synthesizer = synthesizer
	return engine
}

// Run executes deep research over the request query/objectives/schema.
// It returns partial results alongside an error when the context is canceled.
func (e *Engine) Run(ctx context.Context, req Request) (Result, error) {
	if e == nil || e.searcher == nil {
		return Result{}, fmt.Errorf("searcher is required")
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return Result{}, fmt.Errorf("query is required")
	}

	opts := req.Options.withDefaults()
	guidance := strings.TrimSpace(req.Guidance)
	objectives := gatherObjectives(req.Objectives)
	if len(objectives) == 0 {
		objectives = []Objective{{
			Key:      "research",
			Required: true,
		}}
	}

	startedAt := e.nowFn()
	scratch := newScratchpad(objectives)
	objectiveByKey := make(map[string]Objective, len(objectives))
	for _, objective := range objectives {
		key := strings.TrimSpace(objective.Key)
		if key == "" {
			continue
		}
		objectiveByKey[key] = objective
	}
	warnings := []string{}
	warningSeen := map[string]struct{}{}
	addWarning := func(warning string) bool {
		warning = strings.TrimSpace(warning)
		if warning == "" {
			return false
		}
		if _, exists := warningSeen[warning]; exists {
			return false
		}
		warningSeen[warning] = struct{}{}
		warnings = append(warnings, warning)
		return true
	}
	if len(opts.ExcludedDomains) > 0 {
		_ = addWarning("excluded_domains: " + strings.Join(opts.ExcludedDomains, ", "))
	}
	unsatisfied := objectives
	rounds := 0
	emitProgress(req.Progress, ProgressEvent{
		Stage: "run_start",
		Query: query,
	})

	// depth is the 0-indexed recursion level; rounds is depth+1 (1-indexed count for external consumers).
	for depth := 0; depth <= opts.MaxDepth && len(unsatisfied) > 0; depth++ {
		if err := ctx.Err(); err != nil {
			result := e.finalizeResult(query, objectives, scratch, warnings, rounds, startedAt, opts.MinEvidencePerObjective)
			return result, err
		}
		rounds = depth + 1
		emitProgress(req.Progress, ProgressEvent{
			Stage: "round_start",
			Query: query,
			Round: rounds,
			Depth: depth,
		})
		plannedQueries := map[string][]string{}
		if e.planner != nil {
			planStart := time.Now()
			currentFindings, currentSources := scratch.snapshot()
			plan, err := e.planner.Plan(ctx, PlanningRequest{
				Query:             query,
				Guidance:          guidance,
				Schema:            req.Schema,
				Objectives:        objectives,
				MissingObjectives: unsatisfied,
				Findings:          currentFindings,
				Sources:           currentSources,
				Warnings:          append([]string(nil), warnings...),
				ExcludedDomains:   append([]string(nil), opts.ExcludedDomains...),
				Depth:             depth,
				Round:             rounds,
			})
			log.Printf("[deep-research] engine: planner depth=%d took %s err=%v queries=%d", depth, time.Since(planStart).Round(time.Millisecond), err, len(plan.Queries))
			if err != nil {
				warning := fmt.Sprintf("planner failed at depth %d: %v", depth, err)
				if addWarning(warning) {
					emitProgress(req.Progress, ProgressEvent{
						Stage:   "warning",
						Query:   query,
						Round:   rounds,
						Depth:   depth,
						Warning: warning,
					})
				}
			} else {
				for _, pq := range plan.Queries {
					key := strings.TrimSpace(pq.ObjectiveKey)
					value := strings.TrimSpace(pq.Query)
					if key == "" || value == "" {
						continue
					}
					plannedQueries[key] = append(plannedQueries[key], value)
					emitProgress(req.Progress, ProgressEvent{
						Stage:        "planned_query",
						Query:        value,
						Round:        rounds,
						Depth:        depth,
						ObjectiveKey: key,
					})
				}
			}
		}

		roundStart := time.Now()
		findings, roundWarnings := e.executeRound(ctx, query, guidance, unsatisfied, opts, depth, rounds, plannedQueries, req.Progress)
		log.Printf("[deep-research] engine: executeRound depth=%d took %s findings=%d warnings=%d", depth, time.Since(roundStart).Round(time.Millisecond), len(findings), len(roundWarnings))
		scratch.addMany(findings)
		for _, warning := range roundWarnings {
			if addWarning(warning) {
				emitProgress(req.Progress, ProgressEvent{
					Stage:   "warning",
					Query:   query,
					Round:   rounds,
					Depth:   depth,
					Warning: warning,
				})
			}
		}
		unsatisfied = unresolvedObjectives(objectives, scratch, opts.MinEvidencePerObjective)

		if e.checker != nil {
			checkerStart := time.Now()
			objectiveResults := scratch.objectiveResults(objectives, opts.MinEvidencePerObjective)
			currentFindings, currentSources := scratch.snapshot()
			check, err := e.checker.Check(ctx, CompletenessRequest{
				Query:            query,
				Guidance:         guidance,
				Schema:           req.Schema,
				Objectives:       objectives,
				ObjectiveResults: objectiveResults,
				Findings:         currentFindings,
				Sources:          currentSources,
				Warnings:         append([]string(nil), warnings...),
				ExcludedDomains:  append([]string(nil), opts.ExcludedDomains...),
				Depth:            depth,
				Round:            rounds,
			})
			log.Printf("[deep-research] engine: checker depth=%d took %s complete=%v err=%v", depth, time.Since(checkerStart).Round(time.Millisecond), check.Complete, err)
			if err != nil {
				warning := fmt.Sprintf("completeness checker failed at depth %d: %v", depth, err)
				if addWarning(warning) {
					emitProgress(req.Progress, ProgressEvent{
						Stage:   "warning",
						Query:   query,
						Round:   rounds,
						Depth:   depth,
						Warning: warning,
					})
				}
			} else {
				if strings.TrimSpace(check.Reasoning) != "" {
					reasoningWarning := "checker_reasoning: " + strings.TrimSpace(check.Reasoning)
					if addWarning(reasoningWarning) {
						emitProgress(req.Progress, ProgressEvent{
							Stage:   "warning",
							Query:   query,
							Round:   rounds,
							Depth:   depth,
							Warning: reasoningWarning,
						})
					}
				}
				unsatisfied, objectives = mergeMissingObjectives(
					unsatisfied,
					check.MissingObjectives,
					objectiveByKey,
					objectives,
				)
				if check.Complete {
					emitProgress(req.Progress, ProgressEvent{
						Stage: "round_complete",
						Query: query,
						Round: rounds,
						Depth: depth,
					})
					unsatisfied = nil
					break
				}
			}
		}
		emitProgress(req.Progress, ProgressEvent{
			Stage: "round_complete",
			Query: query,
			Round: rounds,
			Depth: depth,
		})
	}

	result := e.finalizeResult(query, objectives, scratch, warnings, rounds, startedAt, opts.MinEvidencePerObjective)
	result.MissingObjectiveKeys = objectiveKeys(unsatisfied)
	result.SchemaSatisfied = len(result.MissingObjectiveKeys) == 0

	if e.synthesizer != nil && len(req.Schema) > 0 {
		synth, err := e.synthesizer.Synthesize(ctx, SynthesisRequest{
			Query:            query,
			Guidance:         guidance,
			Schema:           req.Schema,
			Objectives:       objectives,
			ObjectiveResults: result.Objectives,
			Findings:         result.Findings,
			Sources:          result.Sources,
			Warnings:         append([]string(nil), result.Warnings...),
			ExcludedDomains:  append([]string(nil), opts.ExcludedDomains...),
			Rounds:           result.Rounds,
		})
		if err != nil {
			warning := fmt.Sprintf("synthesizer failed: %v", err)
			if addWarning(warning) {
				result.Warnings = append(result.Warnings, warning)
				emitProgress(req.Progress, ProgressEvent{
					Stage:   "warning",
					Query:   query,
					Round:   rounds,
					Depth:   max(0, rounds-1),
					Warning: warning,
				})
			}
		} else {
			result.Output = synth.Output
			if strings.TrimSpace(synth.Summary) != "" {
				result.Summary = strings.TrimSpace(synth.Summary)
			}
		}
	}
	emitProgress(req.Progress, ProgressEvent{
		Stage: "run_complete",
		Query: query,
		Round: rounds,
		Depth: max(0, rounds-1),
	})
	return result, nil
}

func (e *Engine) finalizeResult(
	query string,
	objectives []Objective,
	scratch *scratchpad,
	warnings []string,
	rounds int,
	startedAt time.Time,
	minEvidence int,
) Result {
	objectiveResults := scratch.objectiveResults(objectives, minEvidence)
	findings, sources := scratch.snapshot()
	return Result{
		Query:      query,
		Objectives: objectiveResults,
		Findings:   findings,
		Sources:    sources,
		Summary:    buildSummary(query, objectiveResults),
		Rounds:     rounds,
		Warnings:   warnings,
		StartedAt:  startedAt,
		FinishedAt: e.nowFn(),
	}
}

func (e *Engine) executeRound(
	ctx context.Context,
	baseQuery string,
	guidance string,
	objectives []Objective,
	opts Options,
	depth int,
	round int,
	plannedQueries map[string][]string,
	progress func(ProgressEvent),
) ([]Finding, []string) {
	if len(objectives) == 0 {
		return nil, nil
	}

	// Build the list of (objective, query) pairs to search.
	// The planner may return multiple queries per objective.
	type searchJob struct {
		objective Objective
		query     string
	}
	var jobs []searchJob
	for _, objective := range objectives {
		queries := plannedQueries[objective.Key]
		if len(queries) == 0 {
			jobs = append(jobs, searchJob{objective, buildObjectiveQuery(baseQuery, objective, depth)})
		} else {
			for _, q := range queries {
				jobs = append(jobs, searchJob{objective, q})
			}
		}
	}

	findings := make([]Finding, 0, len(jobs)*opts.SearchResultsPerQuery)
	warnings := make([]string, 0)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, opts.MaxWorkers)
	limiter := rate.NewLimiter(rate.Limit(opts.SearchesPerSecond), max(1, int(opts.SearchesPerSecond)))
	now := e.nowFn()

	for _, job := range jobs {
		job := job
		wg.Add(1)
		sem <- struct{}{}

		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			if err := ctx.Err(); err != nil {
				return
			}

			emitProgress(progress, ProgressEvent{
				Stage:        "search_start",
				Query:        job.query,
				Round:        round,
				Depth:        depth,
				ObjectiveKey: job.objective.Key,
			})
			if err := limiter.Wait(ctx); err != nil {
				return
			}
			searchStart := time.Now()
			hits, err := e.searcher.Search(ctx, SearchRequest{
				Query:           job.query,
				ObjectiveKey:    job.objective.Key,
				Depth:           depth,
				Limit:           opts.SearchResultsPerQuery,
				Guidance:        guidance,
				ExcludedDomains: append([]string(nil), opts.ExcludedDomains...),
			})
			log.Printf("[deep-research] engine: search objective=%s query=%.80s took %s hits=%d err=%v", job.objective.Key, job.query, time.Since(searchStart).Round(time.Millisecond), len(hits), err)
			if err != nil {
				mu.Lock()
				warnings = append(warnings, fmt.Sprintf("search failed for %s: %v", job.objective.Key, err))
				mu.Unlock()
				return
			}
			if len(hits) > opts.SearchResultsPerQuery {
				hits = hits[:opts.SearchResultsPerQuery]
			}

			localFindings := make([]Finding, 0, len(hits))
			localWarnings := make([]string, 0)
			for rank, hit := range hits {
				hit.URL = strings.TrimSpace(hit.URL)
				if hit.URL == "" {
					continue
				}
				if isDomainExcluded(hit.URL, opts.ExcludedDomains) {
					localWarnings = append(localWarnings, fmt.Sprintf("excluded source skipped for %s: %s", job.objective.Key, hit.URL))
					continue
				}
				finding := Finding{
					ObjectiveKey: job.objective.Key,
					Query:        job.query,
					URL:          hit.URL,
					Domain:       domainFromURL(hit.URL),
					Title:        strings.TrimSpace(hit.Title),
					Snippet:      strings.TrimSpace(hit.Snippet),
					Depth:        depth,
					Rank:         rank,
					RetrievedAt:  now,
					PublishedAt:  hit.PublishedAt,
				}
				finding.Score = round3(scoreForHit(baseQuery, job.objective, hit, rank, depth, now))

				if e.fetcher != nil {
					if err := limiter.Wait(ctx); err != nil {
						continue
					}
					fetchStart := time.Now()
					doc, err := e.fetcher.Fetch(ctx, hit.URL)
					log.Printf("[deep-research] engine: fetch url=%.100s took %s err=%v chars=%d", hit.URL, time.Since(fetchStart).Round(time.Millisecond), err, len(doc.Content))
					if err != nil {
						localWarnings = append(localWarnings, fmt.Sprintf("fetch failed for %s: %v", hit.URL, err))
						// Keep the finding if the searcher already provided
						// substantial content, or if the page is gone (404)
						// and we at least have a search snippet.
						var httpErr *fetchHTTPError
						is404 := errors.As(err, &httpErr) && httpErr.StatusCode == 404
						keepFinding := false
						if len(finding.Snippet) >= 200 {
							// Substantial searcher content → promote to excerpt.
							finding.Excerpt = truncateText(finding.Snippet, opts.MaxExcerptChars)
							finding.Snippet = ""
							keepFinding = true
						} else if is404 && finding.Snippet != "" {
							// 404: page gone, keep with snippet-only evidence.
							keepFinding = true
						}
						if keepFinding {
							localFindings = append(localFindings, finding)
							emitProgress(progress, ProgressEvent{
								Stage:        "source",
								Query:        job.query,
								Round:        round,
								Depth:        depth,
								ObjectiveKey: job.objective.Key,
								URL:          finding.URL,
								Title:        finding.Title,
								Rank:         rank,
							})
						}
						continue
					}
					if title := strings.TrimSpace(doc.Title); title != "" {
						finding.Title = title
					}
					excerpt := truncateText(doc.Content, opts.MaxExcerptChars)
					if excerpt == "" {
						localWarnings = append(localWarnings, fmt.Sprintf("fetched content was empty for %s", hit.URL))
						continue
					}
					finding.Snippet = ""
					finding.Excerpt = excerpt
					finding.Score = round3(min(1, finding.Score+0.08))
				}
				localFindings = append(localFindings, finding)
				emitProgress(progress, ProgressEvent{
					Stage:        "source",
					Query:        job.query,
					Round:        round,
					Depth:        depth,
					ObjectiveKey: job.objective.Key,
					URL:          finding.URL,
					Title:        finding.Title,
					Rank:         rank,
				})
			}

			mu.Lock()
			findings = append(findings, localFindings...)
			warnings = append(warnings, localWarnings...)
			mu.Unlock()
		}()
	}

	wg.Wait()
	warnings = collapseFetchWarnings(warnings)
	return findings, warnings
}

// collapseFetchWarnings groups repetitive "fetch failed for URL: HTTP 404" warnings
// into a single summary line to reduce noise.
func collapseFetchWarnings(warnings []string) []string {
	const prefix = "fetch failed for "
	const http404 = "HTTP 404"

	var collapsed []string
	var urls404 []string

	for _, w := range warnings {
		if strings.HasPrefix(w, prefix) && strings.Contains(w, http404) {
			// Extract the URL portion between "fetch failed for " and ":"
			rest := w[len(prefix):]
			if idx := strings.Index(rest, ":"); idx > 0 {
				urls404 = append(urls404, strings.TrimSpace(rest[:idx]))
			} else {
				urls404 = append(urls404, rest)
			}
		} else {
			collapsed = append(collapsed, w)
		}
	}

	if len(urls404) == 0 {
		return warnings
	}
	if len(urls404) <= 3 {
		summary := fmt.Sprintf("fetch failed (HTTP 404) for %d URLs: %s", len(urls404), strings.Join(urls404, ", "))
		collapsed = append(collapsed, summary)
		return collapsed
	}
	shown := strings.Join(urls404[:3], ", ")
	summary := fmt.Sprintf("fetch failed (HTTP 404) for %d URLs: %s (and %d more)", len(urls404), shown, len(urls404)-3)
	collapsed = append(collapsed, summary)
	return collapsed
}

func emitProgress(progress func(ProgressEvent), event ProgressEvent) {
	if progress == nil {
		return
	}
	progress(event)
}

func unresolvedObjectives(objectives []Objective, scratch *scratchpad, minEvidence int) []Objective {
	missing := make([]Objective, 0, len(objectives))
	for _, objective := range objectives {
		if scratch.count(objective.Key) < minEvidence {
			missing = append(missing, objective)
		}
	}
	return missing
}

func gatherObjectives(input []Objective) []Objective {
	manual := normalizeObjectives(input)
	if len(manual) > 0 {
		return dedupeObjectives(manual)
	}
	// Single objective — the planner decides how to break the research into
	// sub-queries based on the schema (which is passed separately).
	return nil
}

func normalizeObjectives(input []Objective) []Objective {
	if len(input) == 0 {
		return nil
	}
	out := make([]Objective, 0, len(input))
	for _, objective := range input {
		key := strings.TrimSpace(objective.Key)
		if key == "" {
			continue
		}
		objective.Key = key
		objective.Description = strings.TrimSpace(objective.Description)
		out = append(out, objective)
	}
	return out
}

func objectiveKeys(objectives []Objective) []string {
	if len(objectives) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(objectives))
	for _, objective := range objectives {
		key := strings.TrimSpace(objective.Key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func mergeMissingObjectives(
	current []Objective,
	missing []MissingObjective,
	objectiveByKey map[string]Objective,
	objectives []Objective,
) ([]Objective, []Objective) {
	if len(missing) == 0 {
		return current, objectives
	}

	merged := append([]Objective(nil), current...)
	seen := map[string]struct{}{}
	for _, objective := range merged {
		key := strings.TrimSpace(objective.Key)
		if key != "" {
			seen[key] = struct{}{}
		}
	}

	for _, item := range missing {
		key := strings.TrimSpace(item.ObjectiveKey)
		if key == "" {
			continue
		}

		objective, ok := objectiveByKey[key]
		if !ok {
			objective = Objective{
				Key:         key,
				Description: strings.TrimSpace(item.Question),
				Required:    true,
			}
			objectiveByKey[key] = objective
			objectives = append(objectives, objective)
		} else if strings.TrimSpace(objective.Description) == "" && strings.TrimSpace(item.Question) != "" {
			objective.Description = strings.TrimSpace(item.Question)
			objectiveByKey[key] = objective
			for i := range objectives {
				if strings.TrimSpace(objectives[i].Key) == key {
					objectives[i].Description = objective.Description
					break
				}
			}
		}

		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, objective)
	}

	return dedupeObjectives(merged), objectives
}

func buildObjectiveQuery(baseQuery string, objective Objective, depth int) string {
	parts := []string{strings.TrimSpace(baseQuery), strings.TrimSpace(objective.Key), strings.TrimSpace(objective.Description)}
	if depth > 0 {
		parts = append(parts, "detailed", "evidence", "source")
	}
	compact := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			compact = append(compact, part)
		}
	}
	return strings.Join(compact, " ")
}

func buildSummary(query string, objectiveResults []ObjectiveResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Research query: %s\n", query)
	for _, objective := range objectiveResults {
		fmt.Fprintf(&b, "- %s [%s]", objective.Objective.Key, objective.Status)
		if objective.BestFinding != nil {
			title := strings.TrimSpace(objective.BestFinding.Title)
			if title != "" {
				fmt.Fprintf(&b, ": %s", title)
			}
			fmt.Fprintf(&b, " (%s)", objective.BestFinding.URL)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func truncateText(text string, max int) string {
	text = strings.TrimSpace(text)
	if text == "" || max <= 0 {
		return ""
	}
	if len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	return text[:max-3] + "..."
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func isDomainExcluded(rawURL string, excludedDomains []string) bool {
	if len(excludedDomains) == 0 {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(domainFromURL(rawURL)))
	host = strings.TrimPrefix(host, "www.")
	if host == "" {
		return false
	}
	for _, domain := range excludedDomains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		domain = strings.TrimPrefix(domain, "www.")
		if domain == "" {
			continue
		}
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}
