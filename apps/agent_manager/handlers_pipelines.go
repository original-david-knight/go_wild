package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

type pipelineStepRequest struct {
	Runner     string            `json:"runner,omitempty"`
	OnMethod   string            `json:"on_method"`
	OnStatus   string            `json:"on_status,omitempty"`
	FromRole   string            `json:"from_role,omitempty"`
	ToAgentID  string            `json:"to_agent_id,omitempty"`
	ToRole     string            `json:"to_role"`
	NextMethod string            `json:"next_method"`
	ParamMap   map[string]string `json:"param_map,omitempty"`
	FanOut     bool              `json:"fan_out,omitempty"`
	FanOutKey  string            `json:"fan_out_key,omitempty"`
}

type upsertPipelineRequest struct {
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	ScopeMode      string                `json:"scope_mode,omitempty"`
	ScopeCompanyID string                `json:"scope_company_id,omitempty"`
	Schedule       string                `json:"schedule,omitempty"`
	Enabled        *bool                 `json:"enabled,omitempty"`
	Steps          []pipelineStepRequest `json:"steps"`
}

type triggerPipelineRequest struct {
	Params map[string]any `json:"params"`
	Result map[string]any `json:"result"` // legacy alias
}

type triggerPipelinePolymarketRequest struct {
	CompanyID        string `json:"company_id,omitempty"`
	IncludePositions *bool  `json:"include_positions,omitempty"`
	IncludeOrders    *bool  `json:"include_orders,omitempty"`
	OrderMarket      string `json:"order_market,omitempty"`
	Market           string `json:"market,omitempty"` // legacy alias for order_market
}

type initialPipelineRequest struct {
	ToRole string         `json:"to_role"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

type pipelineStepResponse struct {
	Runner     string            `json:"runner,omitempty"`
	OnMethod   string            `json:"on_method"`
	OnStatus   string            `json:"on_status"`
	FromRole   string            `json:"from_role"`
	ToAgentID  string            `json:"to_agent_id,omitempty"`
	ToRole     string            `json:"to_role"`
	NextMethod string            `json:"next_method"`
	ParamMap   map[string]string `json:"param_map"`
	FanOut     bool              `json:"fan_out,omitempty"`
	FanOutKey  string            `json:"fan_out_key,omitempty"`
}

type pipelineResponse struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	ScopeMode      string                 `json:"scope_mode"`
	ScopeCompanyID string                 `json:"scope_company_id,omitempty"`
	Schedule       string                 `json:"schedule,omitempty"`
	BuiltIn        bool                   `json:"built_in"`
	Enabled        bool                   `json:"enabled"`
	Steps          []pipelineStepResponse `json:"steps"`
}

type pipelineCapabilityResponse struct {
	AgentID     string `json:"agent_id"`
	AgentName   string `json:"agent_name"`
	Role        string `json:"role"`
	Method      string `json:"method"`
	Description string `json:"description"`
}

type pipelineCollectionHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request)
type pipelineRouteHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request)
type pipelineIDHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, pipelineID string)

type pipelineIDActionRoute struct {
	method  string
	handler pipelineIDHandlerFunc
}

var errPipelineJobNotFound = errors.New("pipeline job not found")
var errPipelineJobNotCancelable = errors.New("pipeline job is not cancellable")

type polymarketPipelineSnapshotClient interface {
	GetPositions(ctx context.Context) ([]polymarket.Position, error)
	GetOrders(ctx context.Context, market string) ([]polymarket.Order, error)
}

var getPipelinePolymarketSnapshotClient = func(ctx context.Context, h *Handlers, companyID string) (polymarketPipelineSnapshotClient, string, error) {
	if h == nil || h.service == nil {
		return nil, "", fmt.Errorf("service is not configured")
	}
	if h.polymarketHelper == nil {
		h.polymarketHelper = NewBrokerPolymarketHandler(h.service)
	}
	client, resolvedCompanyID, err := h.polymarketHelper.getClientForCompany(ctx, companyID)
	if err != nil {
		return nil, "", err
	}
	return client, resolvedCompanyID, nil
}

var pipelineCollectionHandlers = map[string]pipelineCollectionHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request) {
		h.listPipelines(w, r)
	},
	http.MethodPost: func(h *Handlers, w http.ResponseWriter, r *http.Request) {
		h.upsertPipeline(w, r)
	},
}

var pipelineStaticRouteHandlers = map[pipelineRouteKind]pipelineRouteHandlerFunc{
	pipelineRouteCapabilities: func(h *Handlers, w http.ResponseWriter, r *http.Request) {
		h.listPipelineCapabilities(w, r)
	},
	pipelineRouteInitialRequest: func(h *Handlers, w http.ResponseWriter, r *http.Request) {
		h.submitInitialPipelineRequest(w, r)
	},
}

var pipelineStaticRouteMethods = map[pipelineRouteKind]string{
	pipelineRouteCapabilities:   http.MethodGet,
	pipelineRouteInitialRequest: http.MethodPost,
}

var pipelineIDHandlers = map[string]pipelineIDHandlerFunc{
	http.MethodDelete: func(h *Handlers, w http.ResponseWriter, r *http.Request, pipelineID string) {
		h.deletePipeline(w, r, pipelineID)
	},
}

var pipelineIDActionHandlers = map[string]pipelineIDActionRoute{
	"trigger": {
		method: http.MethodPost,
		handler: func(h *Handlers, w http.ResponseWriter, r *http.Request, pipelineID string) {
			h.triggerPipeline(w, r, pipelineID)
		},
	},
	"trigger-polymarket": {
		method: http.MethodPost,
		handler: func(h *Handlers, w http.ResponseWriter, r *http.Request, pipelineID string) {
			h.triggerPipelinePolymarket(w, r, pipelineID)
		},
	},
}

func isPipelineCollectionMethod(method string) bool {
	_, ok := pipelineCollectionHandlers[method]
	return ok
}

func isPipelineDefinitionMethod(method string) bool {
	_, ok := pipelineIDHandlers[method]
	return ok
}

// handlePipelines handles:
// - GET /api/pipelines
// - POST /api/pipelines
// - DELETE /api/pipelines/{id}
// - POST /api/pipelines/{id}/trigger
// - POST /api/pipelines/{id}/trigger-polymarket
// - POST /api/pipelines/{id}/actions/{action}
func (h *Handlers) handlePipelines(w http.ResponseWriter, r *http.Request) {
	if h.pipelineEngine == nil {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeError(w, http.StatusNotImplemented, "pipeline engine not configured")
		return
	}

	route, err := parsePipelineRoute(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch route.kind {
	case pipelineRouteCollection:
		if !isPipelineCollectionMethod(r.Method) {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler, ok := pipelineCollectionHandlers[r.Method]
		if !ok {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler(h, w, r)
	case pipelineRouteCapabilities, pipelineRouteInitialRequest:
		expectedMethod, ok := pipelineStaticRouteMethods[route.kind]
		if !ok || expectedMethod != r.Method {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler, ok := pipelineStaticRouteHandlers[route.kind]
		if !ok {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler(h, w, r)
	case pipelineRouteDefinition:
		if !isPipelineDefinitionMethod(r.Method) {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler, ok := pipelineIDHandlers[r.Method]
		if !ok {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler(h, w, r, route.pipelineID)
	case pipelineRouteTrigger, pipelineRouteTriggerPolymkt, pipelineRouteAction:
		actionRoute, ok := pipelineIDActionHandlers[route.action]
		if !ok {
			writeError(w, http.StatusNotFound, "unknown pipeline action")
			return
		}
		if actionRoute.method != r.Method {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		actionRoute.handler(h, w, r, route.pipelineID)
	default:
		writeError(w, http.StatusNotFound, "unknown pipeline route")
	}
}

func (h *Handlers) listPipelines(w http.ResponseWriter, r *http.Request) {
	defs, err := h.service.ListPipelineDefinitions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := make([]pipelineResponse, 0, len(defs))
	for _, def := range defs {
		scopeMode := strings.TrimSpace(def.ScopeMode)
		scopeCompanyID := strings.TrimSpace(def.ScopeCompanyID)
		if scopeMode == "" {
			scopeMode = "global"
		}
		if scopeMode != "company" {
			scopeCompanyID = ""
		}
		var steps []PipelineStep
		if def.StepsJSON != "" {
			// List endpoints should not fail hard if a user stored an invalid pipeline.
			_ = json.Unmarshal([]byte(def.StepsJSON), &steps)
		}

		stepResp := make([]pipelineStepResponse, 0, len(steps))
		for _, step := range steps {
			paramMap := map[string]string{}
			for k, v := range step.ParamMap {
				paramMap[k] = v
			}
			stepResp = append(stepResp, pipelineStepResponse{
				Runner:     normalizePipelineStepRunner(step.Runner),
				OnMethod:   step.OnMethod,
				OnStatus:   step.OnStatus,
				FromRole:   step.FromRole,
				ToAgentID:  step.ToAgentID,
				ToRole:     step.ToRole,
				NextMethod: step.NextMethod,
				ParamMap:   paramMap,
				FanOut:     step.FanOut,
				FanOutKey:  step.FanOutKey,
			})
		}

		resp = append(resp, pipelineResponse{
			ID:             def.ID,
			Name:           def.Name,
			ScopeMode:      scopeMode,
			ScopeCompanyID: scopeCompanyID,
			Schedule:       strings.TrimSpace(def.Schedule),
			BuiltIn:        false,
			Enabled:        def.Enabled,
			Steps:          stepResp,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) upsertPipeline(w http.ResponseWriter, r *http.Request) {
	var req upsertPipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	steps := make([]PipelineStep, len(req.Steps))
	for i, step := range req.Steps {
		paramMap := map[string]string{}
		for k, v := range step.ParamMap {
			paramMap[k] = v
		}
		steps[i] = PipelineStep{
			Runner:     step.Runner,
			OnMethod:   step.OnMethod,
			OnStatus:   step.OnStatus,
			FromRole:   step.FromRole,
			ToAgentID:  step.ToAgentID,
			ToRole:     step.ToRole,
			NextMethod: step.NextMethod,
			ParamMap:   paramMap,
			FanOut:     step.FanOut,
			FanOutKey:  step.FanOutKey,
		}
	}
	pipeline := Pipeline{
		ID:             req.ID,
		Name:           req.Name,
		ScopeMode:      req.ScopeMode,
		ScopeCompanyID: req.ScopeCompanyID,
		Schedule:       req.Schedule,
		Steps:          steps,
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	created, err := h.pipelineEngine.UpsertPipelineDefinition(r.Context(), pipeline, enabled)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if created {
		writeJSON(w, http.StatusCreated, map[string]any{"status": "created"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "updated"})
}

func (h *Handlers) deletePipeline(w http.ResponseWriter, r *http.Request, pipelineID string) {
	if err := h.pipelineEngine.DeletePipelineDefinition(r.Context(), pipelineID); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrPipelineNotFound):
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handlers) triggerPipeline(w http.ResponseWriter, r *http.Request, pipelineID string) {
	var req triggerPipelineRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}

	payload := req.Params
	if payload == nil {
		payload = req.Result
	}
	triggerJobID, runID, err := h.pipelineEngine.TriggerPipeline(r.Context(), pipelineID, payload)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrPipelineNotFound) {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "no steps") {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "triggered",
		"pipeline_id":    pipelineID,
		"trigger_job_id": triggerJobID,
		"run_id":         runID,
	})
}

func (h *Handlers) triggerPipelinePolymarket(w http.ResponseWriter, r *http.Request, pipelineID string) {
	var req triggerPipelinePolymarketRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}

	includePositions := req.IncludePositions == nil || *req.IncludePositions
	includeOrders := req.IncludeOrders == nil || *req.IncludeOrders
	if !includePositions && !includeOrders {
		writeError(w, http.StatusBadRequest, "at least one of include_positions or include_orders must be true")
		return
	}

	pipeline, ok := h.pipelineEngine.GetPipeline(pipelineID)
	if !ok {
		writeError(w, http.StatusNotFound, ErrPipelineNotFound.Error())
		return
	}

	companyID, err := resolvePolymarketPipelineCompanyID(pipeline, req.CompanyID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	client, resolvedCompanyID, err := getPipelinePolymarketSnapshotClient(r.Context(), h, companyID)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "required") {
			status = http.StatusBadRequest
		} else if isPolymarketAuthError(err) {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, err.Error())
		return
	}

	var positions []polymarket.Position
	if includePositions {
		positions, err = client.GetPositions(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list polymarket positions: "+err.Error())
			return
		}
	}

	var orders []polymarket.Order
	if includeOrders {
		orderMarket := strings.TrimSpace(req.OrderMarket)
		if orderMarket == "" {
			orderMarket = strings.TrimSpace(req.Market)
		}
		orders, err = client.GetOrders(r.Context(), orderMarket)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list polymarket orders: "+err.Error())
			return
		}
	}

	sort.SliceStable(positions, func(i, j int) bool {
		left, _ := builtinPolymarketPositionReevaluationPriority(positions[i])
		right, _ := builtinPolymarketPositionReevaluationPriority(positions[j])
		return left > right
	})
	sort.SliceStable(orders, func(i, j int) bool {
		left, _ := builtinPolymarketOrderReevaluationPriority(orders[i])
		right, _ := builtinPolymarketOrderReevaluationPriority(orders[j])
		return left > right
	})

	reviewContext := buildBuiltinPolymarketReviewContext(positions, 0, 0, false)
	if h != nil && h.companyWalletBalancesLoader != nil {
		if balances, balancesErr := h.companyWalletBalancesLoader(r.Context(), resolvedCompanyID); balancesErr == nil {
			usdcBalance := balanceAmountFromSnapshot(balances, "polygon_usdce")
			liquidUSDBalance := usdcBalance + balanceAmountFromSnapshot(balances, "polygon_usdte")
			reviewContext = buildBuiltinPolymarketReviewContext(positions, usdcBalance, liquidUSDBalance, true)
		}
	}

	triggered := make([]map[string]any, 0, len(positions)+len(orders))
	for i, position := range positions {
		triggerJobID, runID, err := h.pipelineEngine.TriggerPipeline(
			r.Context(),
			pipelineID,
			polymarketPositionTriggerPayload(resolvedCompanyID, position, reviewContext),
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed triggering pipeline for position %d: %v", i, err))
			return
		}
		triggered = append(triggered, map[string]any{
			"item_type":      "position",
			"index":          i,
			"condition_id":   strings.TrimSpace(position.ConditionID),
			"asset":          strings.TrimSpace(position.Asset),
			"run_id":         runID,
			"trigger_job_id": triggerJobID,
		})
	}

	for i, order := range orders {
		triggerJobID, runID, err := h.pipelineEngine.TriggerPipeline(
			r.Context(),
			pipelineID,
			polymarketOrderTriggerPayload(resolvedCompanyID, order, reviewContext),
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed triggering pipeline for order %d: %v", i, err))
			return
		}
		triggered = append(triggered, map[string]any{
			"item_type":      "order",
			"index":          i,
			"order_id":       strings.TrimSpace(order.ID),
			"condition_id":   strings.TrimSpace(order.Market),
			"asset":          strings.TrimSpace(order.AssetID),
			"run_id":         runID,
			"trigger_job_id": triggerJobID,
		})
	}

	status := "triggered"
	if len(triggered) == 0 {
		status = "no_items"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":          status,
		"pipeline_id":     pipelineID,
		"company_id":      resolvedCompanyID,
		"positions_found": len(positions),
		"orders_found":    len(orders),
		"triggered_count": len(triggered),
		"triggered":       triggered,
	})
}

func resolvePolymarketPipelineCompanyID(pipeline Pipeline, requestedCompanyID string) (string, error) {
	requestedCompanyID = strings.TrimSpace(requestedCompanyID)
	scopeMode := strings.TrimSpace(pipeline.ScopeMode)
	scopeCompanyID := strings.TrimSpace(pipeline.ScopeCompanyID)

	if scopeMode == "company" {
		if scopeCompanyID == "" {
			return "", fmt.Errorf("pipeline %q is company-scoped but missing scope_company_id", pipeline.ID)
		}
		if requestedCompanyID != "" && requestedCompanyID != scopeCompanyID {
			return "", fmt.Errorf("company_id must match pipeline scope_company_id for pipeline %q", pipeline.ID)
		}
		return scopeCompanyID, nil
	}

	if requestedCompanyID == "" {
		return "", fmt.Errorf("company_id is required for polymarket snapshot trigger on global pipelines")
	}
	return requestedCompanyID, nil
}

func polymarketPositionTriggerPayload(companyID string, position polymarket.Position, reviewContext builtinPolymarketReviewContext) map[string]any {
	priority, reason := builtinPolymarketPositionReevaluationPriority(position)
	payload := map[string]any{
		"source":                "polymarket",
		"item_type":             "position",
		"company_id":            strings.TrimSpace(companyID),
		"condition_id":          strings.TrimSpace(position.ConditionID),
		"asset":                 strings.TrimSpace(position.Asset),
		"position":              position,
		"reevaluation_priority": priority,
		"reevaluation_reason":   reason,
		"reevaluation_notional": roundBuiltinPolymarketFloat(math.Max(position.CurrentValue, 0), 2),
	}
	applyBuiltinPolymarketReviewContextToItem(payload, strings.TrimSpace(position.ConditionID), reviewContext)
	return payload
}

func polymarketOrderTriggerPayload(companyID string, order polymarket.Order, reviewContext builtinPolymarketReviewContext) map[string]any {
	priority, reason := builtinPolymarketOrderReevaluationPriority(order)
	payload := map[string]any{
		"source":                "polymarket",
		"item_type":             "order",
		"company_id":            strings.TrimSpace(companyID),
		"condition_id":          strings.TrimSpace(order.Market),
		"asset":                 strings.TrimSpace(order.AssetID),
		"order":                 order,
		"reevaluation_priority": priority,
		"reevaluation_reason":   reason,
		"reevaluation_notional": roundBuiltinPolymarketFloat(builtinPolymarketOrderRemainingNotional(order), 2),
	}
	applyBuiltinPolymarketReviewContextToItem(payload, strings.TrimSpace(order.Market), reviewContext)
	return payload
}

func (h *Handlers) listPipelineCapabilities(w http.ResponseWriter, r *http.Request) {
	companyFilterID := strings.TrimSpace(r.URL.Query().Get("company_id"))

	agents, err := h.service.ListAgents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents: "+err.Error())
		return
	}

	methods, err := h.service.ListA2AMethods(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list methods: "+err.Error())
		return
	}
	methodByName := make(map[string]string, len(methods))
	for _, m := range methods {
		methodByName[strings.TrimSpace(m.Method)] = strings.TrimSpace(m.Description)
	}

	resp := make([]pipelineCapabilityResponse, 0)
	for _, agent := range agents {
		if companyFilterID != "" {
			member, err := h.service.GetCompanyMemberForAgent(r.Context(), agent.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to resolve company membership for agent "+agent.ID+": "+err.Error())
				return
			}
			if member == nil || strings.TrimSpace(member.CompanyID) != companyFilterID {
				continue
			}
		}
		caps, err := h.service.GetCapabilities(r.Context(), agent.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list capabilities for agent "+agent.ID+": "+err.Error())
			return
		}
		for _, cap := range caps {
			desc := methodByName[strings.TrimSpace(cap.Method)]
			resp = append(resp, pipelineCapabilityResponse{
				AgentID:     agent.ID,
				AgentName:   agent.Name,
				Role:        cap.Role,
				Method:      cap.Method,
				Description: desc,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"capabilities": resp})
}

func (h *Handlers) submitInitialPipelineRequest(w http.ResponseWriter, r *http.Request) {
	var req initialPipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	jobID, targetAgentID, err := h.pipelineEngine.SubmitInitialRequest(r.Context(), req.ToRole, req.Method, req.Params)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "failed to submit") || strings.Contains(err.Error(), "failed to resolve") {
			status = http.StatusInternalServerError
		}
		writeError(w, status, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "submitted",
		"job_id":          jobID,
		"target_agent_id": targetAgentID,
		"to_role":         req.ToRole,
		"method":          req.Method,
	})
}

// handlePipelineRuns handles GET /api/pipelines/runs and GET /api/pipelines/runs/{id}.
func (h *Handlers) handlePipelineRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if h.pipelineEngine == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	// Check if we have a run ID
	path := strings.TrimPrefix(r.URL.Path, "/api/pipelines/runs")
	path = strings.TrimPrefix(path, "/")

	if path != "" {
		// GET /api/pipelines/runs/{id}
		h.getPipelineRunDetail(w, r, path)
		return
	}

	// GET /api/pipelines/runs
	runs, err := h.pipelineEngine.GetPipelineRuns(r.Context(), 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, runs)
}

// getPipelineRunDetail returns a single pipeline run with its steps enriched with job data.
func (h *Handlers) getPipelineRunDetail(w http.ResponseWriter, r *http.Request, runID string) {
	enriched, err := h.pipelineEngine.GetPipelineRunDetailEnriched(r.Context(), runID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, enriched)
}

// handlePipelineJobs handles:
// - GET  /api/pipelines/jobs          — list active jobs (queued/claimed/running)
// - DELETE /api/pipelines/jobs        — cancel all queued/claimed jobs
// - DELETE /api/pipelines/jobs/{id}   — cancel a queued/claimed job
func (h *Handlers) handlePipelineJobs(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/pipelines/jobs")
	path = strings.TrimPrefix(path, "/")

	if path != "" {
		if r.Method != http.MethodDelete {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.cancelPipelineJob(w, r, strings.TrimSpace(path))
		return
	}

	if r.Method == http.MethodDelete {
		h.cancelAllPipelineJobs(w, r)
		return
	}

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	db := h.pipelineEngine.DB()
	if db == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	dao := db.Table(localA2AJob{})

	// Fetch active jobs ordered by most recent first.
	results, err := dao.Query(r.Context(), gowild_data.QueryOpts{
		WhereIn:   map[string][]any{"status": {localA2AStatusQueued, localA2AStatusClaimed, "running"}},
		OrderBy:   "created_at",
		OrderDesc: true,
		Limit:     100,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Build a cache of agent ID → (agent name, company name) for enrichment.
	agentNames := map[string]string{}
	companyNames := map[string]string{}
	resolveAgent := func(agentID string) {
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			return
		}
		if _, ok := agentNames[agentID]; ok {
			return
		}
		agent, err := h.service.GetAgent(r.Context(), agentID)
		if err == nil && agent != nil {
			agentNames[agentID] = agent.Name
		}
		member, err := h.service.GetCompanyMemberForAgent(r.Context(), agentID)
		if err == nil && member != nil {
			cid := strings.TrimSpace(member.CompanyID)
			if _, ok := companyNames[agentID]; !ok {
				company, err := h.service.GetCompany(r.Context(), cid)
				if err == nil && company != nil {
					companyNames[agentID] = company.Name
				}
			}
		}
	}

	jobs := make([]map[string]any, 0, len(results))
	systemSvc := data.NewAgentService(db, "system")
	for _, row := range results {
		job := row.(*localA2AJob)
		resolveAgent(job.ToAgentID)
		resolveAgent(job.FromAgentID)
		entry := map[string]any{
			"id":              job.ID,
			"from_agent":      job.FromAgentID,
			"from_agent_name": agentNames[strings.TrimSpace(job.FromAgentID)],
			"to_agent":        job.ToAgentID,
			"to_agent_name":   agentNames[strings.TrimSpace(job.ToAgentID)],
			"company":         companyNames[strings.TrimSpace(job.ToAgentID)],
			"status":          job.Status,
			"created_at":      job.CreatedAt,
			"cancelable":      job.Status == localA2AStatusQueued || job.Status == localA2AStatusClaimed || job.Status == "running",
		}
		if job.ClaimedAt != nil {
			entry["claimed_at"] = *job.ClaimedAt
		}
		// Extract method and params from request JSON.
		var req map[string]any
		if json.Unmarshal([]byte(job.RequestJSON), &req) == nil {
			method := ""
			if m, _ := req["method"].(string); m != "" {
				method = m
				entry["method"] = method
			}
			if params, ok := req["params"].(map[string]any); ok && len(params) > 0 {
				entry["params"] = sanitizePipelineStepParams(r.Context(), systemSvc, method, params)
			}
		}
		jobs = append(jobs, entry)
	}

	writeJSON(w, http.StatusOK, jobs)
}

// cancelPipelineJob marks a queued or claimed job as failed (cancelled).
func (h *Handlers) cancelPipelineJob(w http.ResponseWriter, r *http.Request, jobID string) {
	db := h.pipelineEngine.DB()
	if db == nil {
		writeError(w, http.StatusNotImplemented, "pipeline engine not configured")
		return
	}

	dao := db.Table(localA2AJob{})
	status, err := h.cancelPipelineJobByID(r.Context(), dao, jobID)
	if err != nil {
		switch {
		case errors.Is(err, errPipelineJobNotFound):
			writeError(w, http.StatusNotFound, "job not found")
		case errors.Is(err, errPipelineJobNotCancelable):
			writeError(w, http.StatusConflict, "job is already "+status)
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (h *Handlers) cancelAllPipelineJobs(w http.ResponseWriter, r *http.Request) {
	db := h.pipelineEngine.DB()
	if db == nil {
		writeError(w, http.StatusNotImplemented, "pipeline engine not configured")
		return
	}

	dao := db.Table(localA2AJob{})
	const batchSize = 200
	cancelled := 0
	skipped := 0

	for {
		rows, err := dao.Query(r.Context(), gowild_data.QueryOpts{
			WhereIn: map[string][]any{
				"status": {localA2AStatusQueued, localA2AStatusClaimed, "running"},
			},
			OrderBy: "created_at",
			Limit:   batchSize,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			job := row.(*localA2AJob)
			_, err := h.cancelPipelineJobByID(r.Context(), dao, job.ID)
			if err == nil {
				cancelled++
				continue
			}
			if errors.Is(err, errPipelineJobNotFound) || errors.Is(err, errPipelineJobNotCancelable) {
				skipped++
				continue
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(rows) < batchSize {
			break
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "cancelled",
		"cancelled": cancelled,
		"skipped":   skipped,
	})
}

func (h *Handlers) cancelPipelineJobByID(ctx context.Context, dao gowild_data.TableDAO, jobID string) (string, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return "", errPipelineJobNotFound
	}

	var job localA2AJob
	if err := dao.Get(ctx, jobID, &job); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errPipelineJobNotFound
		}
		return "", fmt.Errorf("%w: %v", errPipelineJobNotFound, err)
	}

	status := strings.TrimSpace(job.Status)
	if status != localA2AStatusQueued && status != localA2AStatusClaimed && status != "running" {
		return status, errPipelineJobNotCancelable
	}

	now := time.Now().UTC()
	job.Status = localA2AStatusFailed
	job.UpdatedAt = now
	job.CompletedAt = &now
	job.LeaseExpiresAt = nil
	job.ErrorJSON = `{"message":"cancelled by user"}`
	if err := dao.Update(ctx, &job); err != nil {
		return status, err
	}

	return status, nil
}
