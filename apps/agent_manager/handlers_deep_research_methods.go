package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/claudellm"
	deepresearch "github.com/original-david-knight/go_wild/deep_research"
	"github.com/original-david-knight/go_wild/my"
)

var deepResearchRunMethodTest = runDeepResearchMethodTest
var deepResearchRunMethodTestProgress = runDeepResearchMethodTestWithProgress
var buildGeminiDeepResearchPlanner = func() (deepresearch.Planner, error) {
	return deepresearch.NewGeminiPlanner()
}
var buildGeminiDeepResearchChecker = func() (deepresearch.CompletenessChecker, error) {
	return deepresearch.NewGeminiCompletenessChecker()
}
var buildGeminiDeepResearchSynthesizer = func() (deepresearch.Synthesizer, error) {
	return deepresearch.NewGeminiSynthesizer()
}
var buildClaudeDeepResearchPlanner = func() deepresearch.Planner {
	return deepresearch.NewClaudePlanner(newClaudeDeepResearchReasoningClient(
		os.Getenv("CLAUDE_SMART_MODEL"),
		3*time.Minute,
		"deep-research-planner",
	))
}
var buildClaudeDeepResearchChecker = func() deepresearch.CompletenessChecker {
	return deepresearch.NewClaudeCompletenessChecker(newClaudeDeepResearchReasoningClient(
		os.Getenv("CLAUDE_FAST_MODEL"),
		2*time.Minute,
		"deep-research-checker",
	))
}
var buildClaudeDeepResearchSynthesizer = func() deepresearch.Synthesizer {
	return deepresearch.NewClaudeSynthesizer(newClaudeDeepResearchReasoningClient(
		os.Getenv("CLAUDE_SMART_MODEL"),
		5*time.Minute,
		"deep-research-synthesizer",
	))
}
var buildCodexDeepResearchPlanner = func() (deepresearch.Planner, error) {
	return deepresearch.DefaultCodexPlanner()
}
var buildCodexDeepResearchChecker = func() (deepresearch.CompletenessChecker, error) {
	return deepresearch.DefaultCodexCompletenessChecker()
}
var buildCodexDeepResearchSynthesizer = func() (deepresearch.Synthesizer, error) {
	return deepresearch.DefaultCodexSynthesizer()
}

type deepResearchMethodRequest struct {
	Method         string          `json:"method"`
	Description    string          `json:"description"`
	Instructions   string          `json:"instructions"`
	QueryTemplate  *string         `json:"query_template,omitempty"`
	InputSchema    json.RawMessage `json:"input_schema,omitempty"`
	ResearchSchema json.RawMessage `json:"research_schema,omitempty"`
	Options        json.RawMessage `json:"options,omitempty"`
	Enabled        *bool           `json:"enabled,omitempty"`
}

type deepResearchMethodTestRequest struct {
	Query   string          `json:"query,omitempty"`
	Input   map[string]any  `json:"input,omitempty"`
	Options json.RawMessage `json:"options,omitempty"`
}

type deepResearchMethodTestResult struct {
	Method     string              `json:"method"`
	Query      string              `json:"query"`
	Input      map[string]any      `json:"input"`
	Result     deepresearch.Result `json:"result"`
	DurationMS int64               `json:"duration_ms"`
}

type deepResearchCollectionHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request)
type deepResearchMethodHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, method string)

type deepResearchMethodActionRoute struct {
	method  string
	handler deepResearchMethodHandlerFunc
}

func newClaudeDeepResearchReasoningClient(model string, timeout time.Duration, label string) *claudellm.Client {
	return &claudellm.Client{
		Model:           strings.TrimSpace(model),
		Timeout:         timeout,
		Label:           strings.TrimSpace(label),
		Tools:           []string{},
		OutputStylePath: claudellm.ResearchOutputStylePath(),
	}
}

var deepResearchCollectionHandlers = map[string]deepResearchCollectionHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request) {
		h.listDeepResearchMethods(w, r)
	},
	http.MethodPost: func(h *Handlers, w http.ResponseWriter, r *http.Request) {
		h.createDeepResearchMethod(w, r)
	},
}

var deepResearchMethodHandlers = map[string]deepResearchMethodHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request, method string) {
		h.getDeepResearchMethod(w, r, method)
	},
	http.MethodPut: func(h *Handlers, w http.ResponseWriter, r *http.Request, method string) {
		h.updateDeepResearchMethod(w, r, method)
	},
	http.MethodDelete: func(h *Handlers, w http.ResponseWriter, r *http.Request, method string) {
		h.deleteDeepResearchMethod(w, r, method)
	},
}

var deepResearchMethodActionHandlers = map[string]deepResearchMethodActionRoute{
	"test": {
		method: http.MethodPost,
		handler: func(h *Handlers, w http.ResponseWriter, r *http.Request, method string) {
			h.testDeepResearchMethod(w, r, method)
		},
	},
	"test-stream": {
		method: http.MethodPost,
		handler: func(h *Handlers, w http.ResponseWriter, r *http.Request, method string) {
			h.streamTestDeepResearchMethod(w, r, method)
		},
	},
}

func isDeepResearchCollectionMethod(method string) bool {
	_, ok := deepResearchCollectionHandlers[method]
	return ok
}

func isDeepResearchMethodMethod(method string) bool {
	_, ok := deepResearchMethodHandlers[method]
	return ok
}

func isDeepResearchMethodAction(action string) bool {
	_, ok := deepResearchMethodActionHandlers[action]
	return ok
}

func (h *Handlers) handleDeepResearchMethods(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/deep-research-methods")
	path = strings.TrimPrefix(path, "/")

	method := ""
	action := ""
	if path != "" {
		parts := strings.Split(path, "/")
		method = strings.TrimSpace(parts[0])
		if len(parts) > 1 {
			action = strings.TrimSpace(parts[1])
		}
	}

	if method == "" {
		if !isDeepResearchCollectionMethod(r.Method) {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler, ok := deepResearchCollectionHandlers[r.Method]
		if !ok {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler(h, w, r)
		return
	}

	if action != "" {
		if !isDeepResearchMethodAction(action) {
			writeError(w, http.StatusNotFound, "unknown deep research methods route")
			return
		}
		route, ok := deepResearchMethodActionHandlers[action]
		if !ok {
			writeError(w, http.StatusNotFound, "unknown deep research methods route")
			return
		}
		if r.Method != route.method {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		route.handler(h, w, r, method)
		return
	}

	if !isDeepResearchMethodMethod(r.Method) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler, ok := deepResearchMethodHandlers[r.Method]
	if !ok {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler(h, w, r, method)
}

func (h *Handlers) listDeepResearchMethods(w http.ResponseWriter, r *http.Request) {
	methods, err := h.service.ListDeepResearchMethods(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list deep research methods: "+err.Error())
		return
	}
	out := make([]map[string]any, 0, len(methods))
	for i := range methods {
		out = append(out, deepResearchMethodToResponse(&methods[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"methods": out})
}

func (h *Handlers) getDeepResearchMethod(w http.ResponseWriter, r *http.Request, method string) {
	m, err := h.service.GetDeepResearchMethod(r.Context(), method)
	if err != nil {
		writeError(w, http.StatusNotFound, "deep research method not found: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, deepResearchMethodToResponse(m))
}

func (h *Handlers) createDeepResearchMethod(w http.ResponseWriter, r *http.Request) {
	var req deepResearchMethodRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	method := strings.TrimSpace(req.Method)
	if method == "" {
		writeError(w, http.StatusBadRequest, "method is required")
		return
	}
	if !methodTokenRe.MatchString(method) {
		writeError(w, http.StatusBadRequest, "method contains invalid characters (use a simple token like market_research)")
		return
	}

	queryTemplate := ""
	if req.QueryTemplate != nil {
		queryTemplate = strings.TrimSpace(*req.QueryTemplate)
	}

	inputSchemaJSON, err := normalizeCapabilitySchema(req.InputSchema, "input_schema")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	researchSchemaJSON, err := normalizeCapabilitySchema(req.ResearchSchema, "research_schema")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	optionsJSON, err := normalizeDeepResearchOptions(req.Options)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	m, err := h.service.CreateDeepResearchMethod(
		r.Context(),
		method,
		req.Description,
		req.Instructions,
		queryTemplate,
		inputSchemaJSON,
		researchSchemaJSON,
		optionsJSON,
		enabled,
	)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "already exists") {
			status = http.StatusConflict
		}
		writeError(w, status, "failed to create deep research method: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, deepResearchMethodToResponse(m))
}

func (h *Handlers) updateDeepResearchMethod(w http.ResponseWriter, r *http.Request, method string) {
	if strings.TrimSpace(method) == "" {
		writeError(w, http.StatusBadRequest, "method is required")
		return
	}
	existing, err := h.service.GetDeepResearchMethod(r.Context(), method)
	if err != nil || existing == nil {
		writeError(w, http.StatusNotFound, "deep research method not found")
		return
	}

	var req deepResearchMethodRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	queryTemplate := strings.TrimSpace(existing.QueryTemplate)
	if req.QueryTemplate != nil {
		queryTemplate = strings.TrimSpace(*req.QueryTemplate)
	}

	inputSchemaJSON := existing.InputSchemaJSON
	researchSchemaJSON := existing.ResearchSchemaJSON
	optionsJSON := existing.OptionsJSON

	if req.InputSchema != nil {
		normalized, err := normalizeCapabilitySchema(req.InputSchema, "input_schema")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		inputSchemaJSON = normalized
	}
	if req.ResearchSchema != nil {
		normalized, err := normalizeCapabilitySchema(req.ResearchSchema, "research_schema")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		researchSchemaJSON = normalized
	}
	if req.Options != nil {
		normalized, err := normalizeDeepResearchOptions(req.Options)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		optionsJSON = normalized
	}

	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	m, err := h.service.UpdateDeepResearchMethod(
		r.Context(),
		method,
		req.Description,
		req.Instructions,
		queryTemplate,
		inputSchemaJSON,
		researchSchemaJSON,
		optionsJSON,
		enabled,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update deep research method: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, deepResearchMethodToResponse(m))
}

func (h *Handlers) deleteDeepResearchMethod(w http.ResponseWriter, r *http.Request, method string) {
	if err := h.service.DeleteDeepResearchMethod(r.Context(), method); err != nil {
		writeError(w, http.StatusNotFound, "deep research method not found: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handlers) testDeepResearchMethod(w http.ResponseWriter, r *http.Request, method string) {
	m, err := h.service.GetDeepResearchMethod(r.Context(), method)
	if err != nil {
		writeError(w, http.StatusNotFound, "deep research method not found: "+err.Error())
		return
	}
	if !m.Enabled {
		writeError(w, http.StatusBadRequest, "deep research method is disabled")
		return
	}

	var req deepResearchMethodTestRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}
	if req.Input == nil {
		req.Input = map[string]any{}
	}

	started := time.Now()
	timeout := deepResearchTimeoutForMethod(m)
	testCtx, cancel := detachedDeepResearchContext(r.Context(), h.shutdownCtx, timeout)
	defer cancel()

	out, err := deepResearchRunMethodTest(testCtx, m, req)
	if err != nil {
		if errors.Is(err, deepresearch.ErrMissingConfig) {
			writeError(w, http.StatusInternalServerError, "deep research misconfigured: "+err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "deep research test failed: "+err.Error())
		return
	}
	_ = h.service.MarkDeepResearchMethodTested(r.Context(), m.Method)
	out.DurationMS = time.Since(started).Milliseconds()

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"method": m.Method,
		"test":   out,
	})
}

func (h *Handlers) streamTestDeepResearchMethod(w http.ResponseWriter, r *http.Request, method string) {
	m, err := h.service.GetDeepResearchMethod(r.Context(), method)
	if err != nil {
		writeError(w, http.StatusNotFound, "deep research method not found: "+err.Error())
		return
	}
	if !m.Enabled {
		writeError(w, http.StatusBadRequest, "deep research method is disabled")
		return
	}

	var req deepResearchMethodTestRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}
	if req.Input == nil {
		req.Input = map[string]any{}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported by response writer")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	writeEvent := func(event deepResearchTestStreamEvent) bool {
		if err := writeDeepResearchTestStreamEvent(w, flusher, event); err != nil {
			return false
		}
		return true
	}

	if !writeEvent(deepResearchTestStreamEvent{
		Type:    "start",
		Method:  m.Method,
		Message: "Deep research test started",
	}) {
		return
	}

	started := time.Now()
	timeout := deepResearchTimeoutForMethod(m)
	testCtx, cancel := detachedDeepResearchContext(r.Context(), h.shutdownCtx, timeout)
	defer cancel()

	progressCh := make(chan deepresearch.ProgressEvent, 256)
	doneCh := make(chan struct {
		out *deepResearchMethodTestResult
		err error
	}, 1)

	go func() {
		out, err := deepResearchRunMethodTestProgress(testCtx, m, req, func(event deepresearch.ProgressEvent) {
			select {
			case progressCh <- event:
			default:
			}
		})
		doneCh <- struct {
			out *deepResearchMethodTestResult
			err error
		}{out: out, err: err}
	}()

	for {
		select {
		case event := <-progressCh:
			copyEvent := event
			if !writeEvent(deepResearchTestStreamEvent{
				Type:   "progress",
				Method: m.Method,
				Event:  &copyEvent,
			}) {
				return
			}
		case done := <-doneCh:
			if done.err != nil {
				_ = writeEvent(deepResearchTestStreamEvent{
					Type:   "error",
					Method: m.Method,
					Error:  "deep research test failed: " + done.err.Error(),
				})
				return
			}
			_ = h.service.MarkDeepResearchMethodTested(r.Context(), m.Method)
			done.out.DurationMS = time.Since(started).Milliseconds()
			_ = writeEvent(deepResearchTestStreamEvent{
				Type:   "done",
				Method: m.Method,
				Test:   done.out,
			})
			return
		case <-r.Context().Done():
			return
		}
	}
}

type deepResearchTestStreamEvent struct {
	Type    string                        `json:"type"`
	Method  string                        `json:"method,omitempty"`
	Message string                        `json:"message,omitempty"`
	Event   *deepresearch.ProgressEvent   `json:"event,omitempty"`
	Test    *deepResearchMethodTestResult `json:"test,omitempty"`
	Error   string                        `json:"error,omitempty"`
}

func writeDeepResearchTestStreamEvent(w io.Writer, flusher http.Flusher, event deepResearchTestStreamEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if _, err := w.Write(payload); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// detachedDeepResearchContext returns a context for a deep-research run that
// inherits VALUES from parent (trace IDs, request-scoped metadata) but is
// decoupled from parent's cancellation. The returned context cancels only on
// the method's own timeout or (when shutdownCtx is non-nil) the manager's
// SIGTERM signal — not on HTTP request lifecycle events like the 5-minute
// WriteTimeout, which would otherwise kill multi-minute codex planner runs
// mid-flight. When shutdownCtx is nil (tests, or callers without a lifecycle
// signal) only the timeout is active. The returned CancelFunc must always be
// invoked via defer so both the AfterFunc registration and the child ctx are
// released.
func detachedDeepResearchContext(parent, shutdownCtx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), timeout)
	if shutdownCtx == nil {
		return ctx, cancel
	}
	stop := context.AfterFunc(shutdownCtx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func deepResearchTimeoutForMethod(m *DeepResearchMethod) time.Duration {
	const defaultTimeout = 900 * time.Second
	if m == nil {
		return defaultTimeout
	}
	opts, err := parseDeepResearchOptions(m.OptionsJSON)
	if err != nil || opts.TimeoutSeconds <= 0 {
		return defaultTimeout
	}
	return time.Duration(opts.TimeoutSeconds) * time.Second
}

func deepResearchMethodToResponse(m *DeepResearchMethod) map[string]any {
	if m == nil {
		return nil
	}
	resp := map[string]any{
		"method":         m.Method,
		"description":    m.Description,
		"instructions":   m.Instructions,
		"query_template": m.QueryTemplate,
		"enabled":        m.Enabled,
		"created_at":     m.CreatedAt.Format(time.RFC3339),
		"updated_at":     m.UpdatedAt.Format(time.RFC3339),
	}
	if !m.LastTestedAt.IsZero() {
		resp["last_tested_at"] = m.LastTestedAt.Format(time.RFC3339)
	}
	if parsed, err := parseCapabilitySchema(m.InputSchemaJSON); err == nil && parsed != nil {
		resp["input_schema"] = parsed
	}
	if parsed, err := parseCapabilitySchema(m.ResearchSchemaJSON); err == nil && parsed != nil {
		resp["research_schema"] = parsed
	}
	if parsed, err := parseCapabilitySchema(m.OptionsJSON); err == nil && parsed != nil {
		resp["options"] = parsed
	}
	return resp
}

func normalizeDeepResearchOptions(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return "", nil
	}

	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return "", fmt.Errorf("options must be valid JSON: %w", err)
	}
	if _, ok := parsed.(map[string]any); !ok {
		return "", fmt.Errorf("options must be a JSON object")
	}

	var opts deepresearch.Options
	if err := json.Unmarshal([]byte(trimmed), &opts); err != nil {
		return "", fmt.Errorf("options failed to parse: %w", err)
	}

	normalized, err := json.Marshal(parsed)
	if err != nil {
		return "", fmt.Errorf("options failed to serialize: %w", err)
	}
	return string(normalized), nil
}

func runDeepResearchMethodTest(ctx context.Context, method *DeepResearchMethod, req deepResearchMethodTestRequest) (*deepResearchMethodTestResult, error) {
	return runDeepResearchMethodTestWithProgress(ctx, method, req, nil)
}

func runDeepResearchMethodTestWithProgress(
	ctx context.Context,
	method *DeepResearchMethod,
	req deepResearchMethodTestRequest,
	progress func(deepresearch.ProgressEvent),
) (*deepResearchMethodTestResult, error) {
	// Limit to one deep research job at a time.
	sema := gowild_my.EnvSemaphore("DEEP_RESEARCH_MAX_CONCURRENT", 1)
	if err := sema.Acquire(ctx); err != nil {
		return nil, err
	}
	defer sema.Release()

	if method == nil {
		return nil, fmt.Errorf("method is required")
	}
	input := req.Input
	if input == nil {
		input = map[string]any{}
	}

	if strings.TrimSpace(method.InputSchemaJSON) != "" {
		if err := validatePayloadAgainstCapabilitySchema(method.InputSchemaJSON, input); err != nil {
			return nil, fmt.Errorf("input payload failed input schema for method %q: %w", method.Method, err)
		}
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		query = deepResearchQueryFromTemplate(strings.TrimSpace(method.QueryTemplate), input)
	}
	if query == "" {
		return nil, fmt.Errorf("query is empty after resolving deep research input")
	}

	var schema map[string]any
	if strings.TrimSpace(method.ResearchSchemaJSON) != "" {
		parsed, err := parseCapabilitySchema(method.ResearchSchemaJSON)
		if err != nil {
			return nil, fmt.Errorf("invalid research schema: %w", err)
		}
		schemaMap, ok := parsed.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("research_schema must be a JSON object")
		}
		schema = schemaMap
	}

	methodOptions, err := parseDeepResearchOptions(method.OptionsJSON)
	if err != nil {
		return nil, err
	}
	overrideOptions, err := parseDeepResearchOptions(strings.TrimSpace(string(req.Options)))
	if err != nil {
		return nil, err
	}
	options := mergeDeepResearchOptions(methodOptions, overrideOptions)
	guidance := strings.TrimSpace(method.Instructions)
	if guidance == "" {
		guidance = strings.TrimSpace(method.Description)
	}
	if options.SearchResultsPerQuery == 0 {
		options.SearchResultsPerQuery = 10
	}
	if options.MaxExcerptChars == 0 {
		options.MaxExcerptChars = 12000
	}

	var searcher deepresearch.Searcher
	var searchProvider string
	var planner deepresearch.Planner
	var checker deepresearch.CompletenessChecker
	var synthesizer deepresearch.Synthesizer
	var plannerErr, checkerErr, synthesizerErr error

	switch options.LLMBackend {
	case "claude":
		// Claude handles search, planning, completeness, and synthesis.
		// Page fetches still run only through the engine's Go read_webpage fetcher.
		var searcherErr error
		searcher, searchProvider, searcherErr = newDeepResearchSearcher("claude")
		if searcherErr != nil {
			return nil, searcherErr
		}
		planner = buildClaudeDeepResearchPlanner()
		checker = buildClaudeDeepResearchChecker()
		synthesizer = buildClaudeDeepResearchSynthesizer()
	case "gemini":
		var searcherErr error
		searcher, searchProvider, searcherErr = newDeepResearchSearcher("gemini")
		if searcherErr != nil {
			return nil, searcherErr
		}
		planner, plannerErr = buildGeminiDeepResearchPlanner()
		checker, checkerErr = buildGeminiDeepResearchChecker()
		synthesizer, synthesizerErr = buildGeminiDeepResearchSynthesizer()
	case "codex":
		var searcherErr error
		searcher, searchProvider, searcherErr = newDeepResearchSearcher("codex")
		if searcherErr != nil {
			return nil, searcherErr
		}
		if planner, err = buildCodexDeepResearchPlanner(); err != nil {
			return nil, err
		}
		if checker, err = buildCodexDeepResearchChecker(); err != nil {
			return nil, err
		}
		if synthesizer, err = buildCodexDeepResearchSynthesizer(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("deep research searcher unavailable: llm_backend %q is not supported (set GEMINI_API_KEY for gemini, configure agent model_provider for claude or openai)", options.LLMBackend)
	}

	fetcher, fetchProvider, err := newDeepResearchFetcher()
	if err != nil {
		return nil, err
	}
	engine := deepresearch.NewEngineWithReasoning(searcher, fetcher, planner, checker, synthesizer)

	result, err := engine.Run(ctx, deepresearch.Request{
		Query:    query,
		Schema:   schema,
		Options:  options,
		Guidance: guidance,
		Progress: progress,
	})
	if err != nil {
		return nil, err
	}
	if plannerErr != nil {
		result.Warnings = append(result.Warnings, "planner unavailable: "+plannerErr.Error())
	}
	if checkerErr != nil {
		result.Warnings = append(result.Warnings, "completeness_checker unavailable: "+checkerErr.Error())
	}
	if synthesizerErr != nil && len(schema) > 0 {
		result.Warnings = append(result.Warnings, "synthesizer unavailable: "+synthesizerErr.Error())
	}
	if strings.TrimSpace(searchProvider) != "" {
		result.Warnings = append(result.Warnings, "search_provider: "+strings.TrimSpace(searchProvider))
	}
	if strings.TrimSpace(fetchProvider) != "" {
		result.Warnings = append(result.Warnings, "fetch_provider: "+strings.TrimSpace(fetchProvider))
	}

	return &deepResearchMethodTestResult{
		Method: method.Method,
		Query:  query,
		Input:  input,
		Result: result,
	}, nil
}

var deepResearchTemplateVarRe = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)

func deepResearchQueryFromTemplate(template string, input map[string]any) string {
	template = strings.TrimSpace(template)
	if template == "" {
		if q, _ := input["query"].(string); strings.TrimSpace(q) != "" {
			return strings.TrimSpace(q)
		}
		return deepResearchQueryFromInput(input)
	}
	query := deepResearchTemplateVarRe.ReplaceAllStringFunc(template, func(raw string) string {
		matches := deepResearchTemplateVarRe.FindStringSubmatch(raw)
		if len(matches) < 2 {
			return ""
		}
		value, ok := deepResearchTemplateLookup(input, strings.TrimSpace(matches[1]))
		if !ok {
			return ""
		}
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	})
	query = strings.Join(strings.Fields(query), " ")
	if query == "" {
		if q, _ := input["query"].(string); strings.TrimSpace(q) != "" {
			return strings.TrimSpace(q)
		}
		return deepResearchQueryFromInput(input)
	}
	return query
}

func deepResearchQueryFromInput(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	if q, _ := input["query"].(string); strings.TrimSpace(q) != "" {
		return strings.TrimSpace(q)
	}

	payload, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	encoded := strings.TrimSpace(string(payload))
	if encoded == "" || encoded == "{}" {
		return ""
	}
	return encoded
}

func deepResearchTemplateLookup(input map[string]any, path string) (any, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, false
	}
	parts := strings.Split(path, ".")
	var current any = input
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := asMap[part]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func parseDeepResearchOptions(raw string) (deepresearch.Options, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return deepresearch.Options{}, nil
	}
	var opts deepresearch.Options
	if err := json.Unmarshal([]byte(raw), &opts); err != nil {
		return deepresearch.Options{}, fmt.Errorf("options failed to parse: %w", err)
	}
	return opts, nil
}

func mergeDeepResearchOptions(base, override deepresearch.Options) deepresearch.Options {
	out := base
	if override.MaxDepth > 0 {
		out.MaxDepth = override.MaxDepth
	}
	if override.MaxWorkers > 0 {
		out.MaxWorkers = override.MaxWorkers
	}
	if override.SearchResultsPerQuery > 0 {
		out.SearchResultsPerQuery = override.SearchResultsPerQuery
	}
	if override.MinEvidencePerObjective > 0 {
		out.MinEvidencePerObjective = override.MinEvidencePerObjective
	}
	if override.MaxExcerptChars > 0 {
		out.MaxExcerptChars = override.MaxExcerptChars
	}
	out.ExcludedDomains = deepResearchMergeDomains(out.ExcludedDomains, override.ExcludedDomains)
	if strings.TrimSpace(override.LLMBackend) != "" {
		out.LLMBackend = strings.TrimSpace(override.LLMBackend)
	}
	return out
}

func deepResearchMergeDomains(parts ...[]string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, part := range parts {
		for _, domain := range part {
			domain = strings.ToLower(strings.TrimSpace(domain))
			domain = strings.TrimPrefix(domain, "www.")
			if domain == "" {
				continue
			}
			if _, ok := seen[domain]; ok {
				continue
			}
			seen[domain] = struct{}{}
			out = append(out, domain)
		}
	}
	return out
}
