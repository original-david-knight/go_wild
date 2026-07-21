package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

type testPolymarketSnapshotClient struct {
	positions []polymarket.Position
	orders    []polymarket.Order
}

func (c *testPolymarketSnapshotClient) GetPositions(context.Context) ([]polymarket.Position, error) {
	return c.positions, nil
}

func (c *testPolymarketSnapshotClient) GetOrders(context.Context, string) ([]polymarket.Order, error) {
	return c.orders, nil
}

func TestHandlePipelinesCRUD(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	h := NewHandlers(svc, nil, nil, nil, nil)
	h.pipelineEngine = engine

	createBody := `{
		"id":"test_pipeline",
		"name":"Test Pipeline",
		"steps":[
			{
				"on_method":"seed_method",
				"on_status":"succeeded",
				"from_role":"*",
				"to_role":"tester",
				"next_method":"run_test",
				"param_map":{"$":"payload"}
			}
		]
	}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/pipelines", strings.NewReader(createBody))
	createRec := httptest.NewRecorder()
	h.handlePipelines(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d (body=%s)", http.StatusCreated, createRec.Code, createRec.Body.String())
	}

	var stored data.PipelineDefinition
	if err := db.Table(data.PipelineDefinition{}).Get(ctx, "test_pipeline", &stored); err != nil {
		t.Fatalf("expected pipeline definition to be persisted: %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/pipelines", nil)
	listRec := httptest.NewRecorder()
	h.handlePipelines(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d", http.StatusOK, listRec.Code)
	}

	var pipelines []map[string]any
	if err := json.NewDecoder(listRec.Body).Decode(&pipelines); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	found := false
	for _, p := range pipelines {
		if id, _ := p["id"].(string); id == "test_pipeline" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected test_pipeline in list response")
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/pipelines/test_pipeline", nil)
	deleteRec := httptest.NewRecorder()
	h.handlePipelines(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected delete status %d, got %d (body=%s)", http.StatusOK, deleteRec.Code, deleteRec.Body.String())
	}

	results, err := db.Table(data.PipelineDefinition{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"id": "test_pipeline"},
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected test_pipeline to be removed from DB")
	}
}

func TestHandlePipelinesAllowsReservedIDs(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	h := NewHandlers(svc, nil, nil, nil, nil)
	h.pipelineEngine = engine

	createBody := `{
		"id":"new_product",
		"name":"User Defined",
		"steps":[
			{
				"on_method":"seed_method",
				"to_role":"tester",
				"next_method":"run_test"
			}
		]
	}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/pipelines", strings.NewReader(createBody))
	createRec := httptest.NewRecorder()
	h.handlePipelines(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusCreated, createRec.Code, createRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/pipelines/new_product", nil)
	deleteRec := httptest.NewRecorder()
	h.handlePipelines(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusOK, deleteRec.Code, deleteRec.Body.String())
	}
}

func TestHandlePipelineTrigger(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)

	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethod(ctx, "run_test", "handles test method", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}

	targetAgent, err := svc.CreateAgent(ctx, "target")
	if err != nil {
		t.Fatalf("create target agent failed: %v", err)
	}
	targetDataSvc := data.NewAgentService(db, targetAgent.ID)
	if err := targetDataSvc.RegisterCapability(ctx, "tester", "run_test"); err != nil {
		t.Fatalf("register target capability failed: %v", err)
	}

	engine := NewPipelineEngine(db, svc)
	h := NewHandlers(svc, nil, nil, nil, nil)
	h.pipelineEngine = engine

	_, err = engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "trigger_test",
		Name: "Trigger Test",
		Steps: []PipelineStep{
			{
				OnMethod:   "seed_method",
				OnStatus:   "succeeded",
				FromRole:   "*",
				ToRole:     "tester",
				NextMethod: "run_test",
				ParamMap:   map[string]string{"$": "payload"},
			},
		},
	}, true)
	if err != nil {
		t.Fatalf("failed creating pipeline definition: %v", err)
	}

	triggerReq := httptest.NewRequest(http.MethodPost, "/api/pipelines/trigger_test/trigger", strings.NewReader(`{"params":{"foo":"bar"}}`))
	triggerRec := httptest.NewRecorder()
	h.handlePipelines(triggerRec, triggerReq)
	if triggerRec.Code != http.StatusOK {
		t.Fatalf("expected trigger status %d, got %d (body=%s)", http.StatusOK, triggerRec.Code, triggerRec.Body.String())
	}
	engine.WaitInFlight()

	var triggerResp map[string]any
	if err := json.NewDecoder(triggerRec.Body).Decode(&triggerResp); err != nil {
		t.Fatalf("decode trigger response failed: %v", err)
	}
	runID, _ := triggerResp["run_id"].(string)
	triggerJobID, _ := triggerResp["trigger_job_id"].(string)
	if runID == "" || triggerJobID == "" {
		t.Fatalf("expected run_id and trigger_job_id in response, got %v", triggerResp)
	}

	var run data.PipelineRun
	if err := db.Table(data.PipelineRun{}).Get(ctx, runID, &run); err != nil {
		t.Fatalf("expected pipeline run to be persisted: %v", err)
	}
	if run.TriggerJobID != triggerJobID {
		t.Fatalf("expected run trigger job id %q, got %q", triggerJobID, run.TriggerJobID)
	}

	stepRuns, err := db.Table(data.PipelineStepRun{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"run_id": runID},
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("query step runs failed: %v", err)
	}
	if len(stepRuns) != 1 {
		t.Fatalf("expected 1 step run, got %d", len(stepRuns))
	}
	step := stepRuns[0].(*data.PipelineStepRun)
	if strings.TrimSpace(step.A2AJobID) == "" {
		t.Fatalf("expected non-empty step job id")
	}

	jobDoc, err := newLocalA2AQueue(db).GetJob(ctx, step.A2AJobID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	request, _ := jobDoc["request"].(map[string]any)
	if method, _ := request["method"].(string); method != "run_test" {
		t.Fatalf("expected submitted method run_test, got %q", method)
	}
	params, _ := request["params"].(map[string]any)
	if gotFoo, _ := params["foo"].(string); gotFoo != "bar" {
		t.Fatalf("expected params.foo=bar, got %v", params["foo"])
	}
}

func TestHandlePipelineTriggerPolymarket(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)

	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethod(ctx, "run_test", "handles test method", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}

	targetAgent, err := svc.CreateAgent(ctx, "target")
	if err != nil {
		t.Fatalf("create target agent failed: %v", err)
	}
	targetDataSvc := data.NewAgentService(db, targetAgent.ID)
	if err := targetDataSvc.RegisterCapability(ctx, "tester", "run_test"); err != nil {
		t.Fatalf("register target capability failed: %v", err)
	}

	company, err := svc.CreateCompany(ctx, "poly-company", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	engine := NewPipelineEngine(db, svc)
	h := NewHandlers(svc, nil, nil, nil, nil)
	h.pipelineEngine = engine

	_, err = engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "poly_trigger_test",
		Name: "Polymarket Trigger Test",
		Steps: []PipelineStep{
			{
				OnMethod:   "seed_method",
				OnStatus:   "succeeded",
				FromRole:   "*",
				ToRole:     "tester",
				NextMethod: "run_test",
				ParamMap:   map[string]string{"$": "payload"},
			},
		},
	}, true)
	if err != nil {
		t.Fatalf("failed creating pipeline definition: %v", err)
	}

	prevClientFactory := getPipelinePolymarketSnapshotClient
	t.Cleanup(func() {
		getPipelinePolymarketSnapshotClient = prevClientFactory
	})
	getPipelinePolymarketSnapshotClient = func(_ context.Context, _ *Handlers, companyID string) (polymarketPipelineSnapshotClient, string, error) {
		if companyID != company.ID {
			t.Fatalf("expected company_id %q, got %q", company.ID, companyID)
		}
		return &testPolymarketSnapshotClient{
			positions: []polymarket.Position{
				{Asset: "asset-1", ConditionID: "cond-1", Outcome: "Yes", Size: 12, CurrentValue: 120},
				{Asset: "asset-2", ConditionID: "cond-2", Outcome: "No", Size: 8, CurrentValue: 80},
			},
			orders: []polymarket.Order{
				{ID: "order-1", Market: "cond-3", AssetID: "asset-3", Side: "BUY"},
			},
		}, companyID, nil
	}
	h.companyWalletBalancesLoader = func(context.Context, string) (map[string]any, error) {
		return map[string]any{
			"polygon_usdce": map[string]any{
				"ok":      true,
				"balance": 200.0,
			},
		}, nil
	}

	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/poly_trigger_test/actions/trigger-polymarket", strings.NewReader(`{"company_id":"`+company.ID+`"}`))
	rec := httptest.NewRecorder()
	h.handlePipelines(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusOK, rec.Code, rec.Body.String())
	}
	engine.WaitInFlight()

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if got, _ := resp["triggered_count"].(float64); int(got) != 3 {
		t.Fatalf("expected triggered_count 3, got %v", resp["triggered_count"])
	}

	runs, err := db.Table(data.PipelineRun{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"pipeline_id": "poly_trigger_test"},
	})
	if err != nil {
		t.Fatalf("query runs failed: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("expected 3 pipeline runs, got %d", len(runs))
	}

	jobs, err := db.Table(localA2AJob{}).GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll local_a2a_jobs failed: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("expected 3 queued jobs, got %d", len(jobs))
	}
	jobByCondition := make(map[string]map[string]any, len(jobs))
	for _, row := range jobs {
		job := row.(*localA2AJob)
		var request map[string]any
		if err := json.Unmarshal([]byte(job.RequestJSON), &request); err != nil {
			t.Fatalf("decode request_json failed: %v", err)
		}
		params, _ := request["params"].(map[string]any)
		if got, _ := params["source"].(string); got != "polymarket" {
			t.Fatalf("expected params.source polymarket, got %q", got)
		}
		itemType, _ := params["item_type"].(string)
		if itemType != "position" && itemType != "order" {
			t.Fatalf("expected params.item_type position/order, got %q", itemType)
		}
		conditionID, _ := params["condition_id"].(string)
		jobByCondition[conditionID] = params
	}

	positionParams := jobByCondition["cond-1"]
	if positionParams == nil {
		t.Fatalf("expected cond-1 trigger payload, got %#v", jobByCondition)
	}
	if got, _ := positionParams["aum"].(float64); got != 400 {
		t.Fatalf("expected cond-1 aum 400, got %v", positionParams["aum"])
	}
	if got, _ := positionParams["max_allowed"].(float64); got != 20 {
		t.Fatalf("expected cond-1 max_allowed 20, got %v", positionParams["max_allowed"])
	}
	if got, _ := positionParams["current_position"].(float64); got != 12 {
		t.Fatalf("expected cond-1 current_position 12, got %v", positionParams["current_position"])
	}
	if got, _ := positionParams["remaining_capacity"].(float64); got != 8 {
		t.Fatalf("expected cond-1 remaining_capacity 8, got %v", positionParams["remaining_capacity"])
	}

	orderParams := jobByCondition["cond-3"]
	if orderParams == nil {
		t.Fatalf("expected cond-3 trigger payload, got %#v", jobByCondition)
	}
	if got, _ := orderParams["current_position"].(float64); got != 0 {
		t.Fatalf("expected cond-3 current_position 0, got %v", orderParams["current_position"])
	}
	if got, _ := orderParams["remaining_capacity"].(float64); got != 20 {
		t.Fatalf("expected cond-3 remaining_capacity 20, got %v", orderParams["remaining_capacity"])
	}
}

func TestHandlePipelineJobsIncludesRunningClaudeJobs(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	h := NewHandlers(svc, nil, nil, nil, nil)
	h.pipelineEngine = engine

	targetAgent, err := svc.CreateAgent(ctx, "claude-active-target")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	engine.recordPipelineJob(
		ctx,
		"claude-job-running",
		targetAgent.ID,
		localA2ARequest{
			Method: "inspect_market",
			Params: map[string]any{
				"condition_id": "cond-active",
			},
		},
		"running",
		nil,
		nil,
		time.Time{},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/pipelines/jobs", nil)
	rec := httptest.NewRecorder()
	h.handlePipelineJobs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusOK, rec.Code, rec.Body.String())
	}

	var jobs []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&jobs); err != nil {
		t.Fatalf("decode jobs failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 active job, got %d", len(jobs))
	}
	if got, _ := jobs[0]["status"].(string); got != "running" {
		t.Fatalf("job status = %q, want running", got)
	}
	if got, _ := jobs[0]["method"].(string); got != "inspect_market" {
		t.Fatalf("job method = %q, want inspect_market", got)
	}
	if cancelable, ok := jobs[0]["cancelable"].(bool); !ok || !cancelable {
		t.Fatalf("cancelable = %#v, want true", jobs[0]["cancelable"])
	}
	params, _ := jobs[0]["params"].(map[string]any)
	if got, _ := params["condition_id"].(string); got != "cond-active" {
		t.Fatalf("params.condition_id = %q, want cond-active", got)
	}
}

func TestHandlePipelineTriggerBuiltinStep(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	h := NewHandlers(svc, nil, nil, nil, nil)
	h.pipelineEngine = engine

	const builtinMethod = "builtin_test_echo"
	builtinPipelineMethodHandlers[builtinMethod] = func(_ context.Context, _ *PipelineEngine, _ *data.PipelineRun, _ PipelineStep, params map[string]any) (map[string]any, error) {
		return map[string]any{
			"status": "ok",
			"echo":   params,
		}, nil
	}
	t.Cleanup(func() {
		delete(builtinPipelineMethodHandlers, builtinMethod)
	})

	_, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "builtin_trigger_test",
		Name: "Builtin Trigger Test",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerBuiltin,
				OnMethod:   "seed_method",
				OnStatus:   "succeeded",
				FromRole:   "*",
				NextMethod: builtinMethod,
				ParamMap:   map[string]string{"$": "payload"},
			},
		},
	}, true)
	if err != nil {
		t.Fatalf("failed creating builtin pipeline definition: %v", err)
	}

	triggerReq := httptest.NewRequest(http.MethodPost, "/api/pipelines/builtin_trigger_test/trigger", strings.NewReader(`{"params":{"foo":"bar"}}`))
	triggerRec := httptest.NewRecorder()
	h.handlePipelines(triggerRec, triggerReq)
	if triggerRec.Code != http.StatusOK {
		t.Fatalf("expected trigger status %d, got %d (body=%s)", http.StatusOK, triggerRec.Code, triggerRec.Body.String())
	}
	engine.WaitInFlight()

	var triggerResp map[string]any
	if err := json.NewDecoder(triggerRec.Body).Decode(&triggerResp); err != nil {
		t.Fatalf("decode trigger response failed: %v", err)
	}
	runID, _ := triggerResp["run_id"].(string)
	if strings.TrimSpace(runID) == "" {
		t.Fatalf("expected non-empty run_id")
	}

	var run data.PipelineRun
	if err := db.Table(data.PipelineRun{}).Get(ctx, runID, &run); err != nil {
		t.Fatalf("expected pipeline run to be persisted: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("expected builtin-only pipeline run status completed, got %q", run.Status)
	}

	stepRuns, err := db.Table(data.PipelineStepRun{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"run_id": runID},
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("query step runs failed: %v", err)
	}
	if len(stepRuns) != 1 {
		t.Fatalf("expected 1 step run, got %d", len(stepRuns))
	}
	step := stepRuns[0].(*data.PipelineStepRun)
	if !strings.HasPrefix(step.A2AJobID, "builtin-") {
		t.Fatalf("expected builtin step run synthetic job id, got %q", step.A2AJobID)
	}

	jobs, err := db.Table(localA2AJob{}).GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll local_a2a_jobs failed: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected no local A2A jobs for builtin step, got %d", len(jobs))
	}
}

func TestHandlePipelineTriggerBuiltinStepFanOut(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	h := NewHandlers(svc, nil, nil, nil, nil)
	h.pipelineEngine = engine

	const builtinMethod = "builtin_test_fanout_echo"
	var captured []map[string]any
	builtinPipelineMethodHandlers[builtinMethod] = func(_ context.Context, _ *PipelineEngine, _ *data.PipelineRun, _ PipelineStep, params map[string]any) (map[string]any, error) {
		copyParams := map[string]any{}
		for k, v := range params {
			copyParams[k] = v
		}
		captured = append(captured, copyParams)
		return map[string]any{"status": "ok"}, nil
	}
	t.Cleanup(func() {
		delete(builtinPipelineMethodHandlers, builtinMethod)
	})

	_, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "builtin_fanout_trigger_test",
		Name: "Builtin FanOut Trigger Test",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerBuiltin,
				OnMethod:   "seed_method",
				OnStatus:   "succeeded",
				FromRole:   "*",
				NextMethod: builtinMethod,
				ParamMap:   map[string]string{"$": "item"},
				FanOut:     true,
				FanOutKey:  "items",
			},
		},
	}, true)
	if err != nil {
		t.Fatalf("failed creating builtin fanout pipeline definition: %v", err)
	}

	triggerReq := httptest.NewRequest(http.MethodPost, "/api/pipelines/builtin_fanout_trigger_test/trigger", strings.NewReader(`{"params":{"items":[{"id":"one"},{"id":"two"},{"id":"three"}]}}`))
	triggerRec := httptest.NewRecorder()
	h.handlePipelines(triggerRec, triggerReq)
	if triggerRec.Code != http.StatusOK {
		t.Fatalf("expected trigger status %d, got %d (body=%s)", http.StatusOK, triggerRec.Code, triggerRec.Body.String())
	}
	engine.WaitInFlight()

	var triggerResp map[string]any
	if err := json.NewDecoder(triggerRec.Body).Decode(&triggerResp); err != nil {
		t.Fatalf("decode trigger response failed: %v", err)
	}
	runID, _ := triggerResp["run_id"].(string)
	if strings.TrimSpace(runID) == "" {
		t.Fatalf("expected non-empty run_id")
	}

	var run data.PipelineRun
	if err := db.Table(data.PipelineRun{}).Get(ctx, runID, &run); err != nil {
		t.Fatalf("expected pipeline run to be persisted: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("expected builtin fanout pipeline run status completed, got %q", run.Status)
	}

	stepRuns, err := db.Table(data.PipelineStepRun{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"run_id": runID},
	})
	if err != nil {
		t.Fatalf("query step runs failed: %v", err)
	}
	if len(stepRuns) != 3 {
		t.Fatalf("expected 3 step runs, got %d", len(stepRuns))
	}
	for _, row := range stepRuns {
		step := row.(*data.PipelineStepRun)
		if step.Status != "succeeded" {
			t.Fatalf("expected step run succeeded, got %q", step.Status)
		}
		if !strings.HasPrefix(step.A2AJobID, "builtin-") {
			t.Fatalf("expected builtin step run synthetic job id, got %q", step.A2AJobID)
		}
	}

	if len(captured) != 3 {
		t.Fatalf("expected 3 builtin fanout invocations, got %d", len(captured))
	}
	for i, params := range captured {
		raw, ok := params["item"]
		if !ok {
			t.Fatalf("expected invocation %d to contain item param", i)
		}
		item, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("expected invocation %d item param to be object, got %T", i, raw)
		}
		id, _ := item["id"].(string)
		if strings.TrimSpace(id) == "" {
			t.Fatalf("expected invocation %d item.id to be populated", i)
		}
	}

	jobs, err := db.Table(localA2AJob{}).GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll local_a2a_jobs failed: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected no local A2A jobs for builtin fanout step, got %d", len(jobs))
	}
}

func TestHandlePipelineTriggerBuiltinStepFailedStatusFailsRun(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	h := NewHandlers(svc, nil, nil, nil, nil)
	h.pipelineEngine = engine

	const builtinMethod = "builtin_test_failed_status"
	builtinPipelineMethodHandlers[builtinMethod] = func(_ context.Context, _ *PipelineEngine, _ *data.PipelineRun, _ PipelineStep, params map[string]any) (map[string]any, error) {
		return map[string]any{
			"status": "FAILED: policy blocked",
			"error":  "policy blocked",
			"params": params,
		}, nil
	}
	t.Cleanup(func() {
		delete(builtinPipelineMethodHandlers, builtinMethod)
	})

	_, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "builtin_failed_status_trigger_test",
		Name: "Builtin Failed Status Trigger Test",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerBuiltin,
				OnMethod:   "seed_method",
				OnStatus:   "succeeded",
				FromRole:   "*",
				NextMethod: builtinMethod,
				ParamMap:   map[string]string{"$": "payload"},
			},
		},
	}, true)
	if err != nil {
		t.Fatalf("failed creating builtin pipeline definition: %v", err)
	}

	triggerReq := httptest.NewRequest(http.MethodPost, "/api/pipelines/builtin_failed_status_trigger_test/trigger", strings.NewReader(`{"params":{"foo":"bar"}}`))
	triggerRec := httptest.NewRecorder()
	h.handlePipelines(triggerRec, triggerReq)
	if triggerRec.Code != http.StatusOK {
		t.Fatalf("expected trigger status %d, got %d (body=%s)", http.StatusOK, triggerRec.Code, triggerRec.Body.String())
	}
	engine.WaitInFlight()

	var triggerResp map[string]any
	if err := json.NewDecoder(triggerRec.Body).Decode(&triggerResp); err != nil {
		t.Fatalf("decode trigger response failed: %v", err)
	}
	runID, _ := triggerResp["run_id"].(string)
	if strings.TrimSpace(runID) == "" {
		t.Fatalf("expected non-empty run_id")
	}

	var run data.PipelineRun
	if err := db.Table(data.PipelineRun{}).Get(ctx, runID, &run); err != nil {
		t.Fatalf("expected pipeline run to be persisted: %v", err)
	}
	if run.Status != "failed" {
		t.Fatalf("expected builtin failed-status pipeline run status failed, got %q", run.Status)
	}
	if !strings.Contains(run.FailureReason, "policy blocked") {
		t.Fatalf("expected failure reason to mention policy blocked, got %q", run.FailureReason)
	}

	stepRuns, err := db.Table(data.PipelineStepRun{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"run_id": runID},
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("query step runs failed: %v", err)
	}
	if len(stepRuns) != 1 {
		t.Fatalf("expected 1 step run, got %d", len(stepRuns))
	}
	step := stepRuns[0].(*data.PipelineStepRun)
	if step.Status != "failed" {
		t.Fatalf("expected builtin failed-status step run failed, got %q", step.Status)
	}
}

func TestHandlePipelineCapabilities(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	h := NewHandlers(svc, nil, nil, nil, nil)
	h.pipelineEngine = engine

	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethod(ctx, "find_candidates", "discover products", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}
	if _, err := systemSvc.CreateA2AMethod(ctx, "review_candidate", "review product candidate", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}

	alpha, err := svc.CreateAgent(ctx, "alpha")
	if err != nil {
		t.Fatalf("create alpha failed: %v", err)
	}
	alphaData := data.NewAgentService(db, alpha.ID)
	if err := alphaData.RegisterCapability(ctx, "scout", "find_candidates"); err != nil {
		t.Fatalf("register alpha capability failed: %v", err)
	}

	beta, err := svc.CreateAgent(ctx, "beta")
	if err != nil {
		t.Fatalf("create beta failed: %v", err)
	}
	betaData := data.NewAgentService(db, beta.ID)
	if err := betaData.RegisterCapability(ctx, "curator", "review_candidate"); err != nil {
		t.Fatalf("register beta capability failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/pipelines/capabilities", nil)
	rec := httptest.NewRecorder()
	h.handlePipelines(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusOK, rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	rawCaps, _ := body["capabilities"].([]any)
	if len(rawCaps) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(rawCaps))
	}
}

func TestHandlePipelineInitialRequest(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)

	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethod(ctx, "run_test", "handles test method", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}

	targetAgent, err := svc.CreateAgent(ctx, "target")
	if err != nil {
		t.Fatalf("create target failed: %v", err)
	}
	targetDataSvc := data.NewAgentService(db, targetAgent.ID)
	if err := targetDataSvc.RegisterCapability(ctx, "tester", "run_test"); err != nil {
		t.Fatalf("register target capability failed: %v", err)
	}

	engine := NewPipelineEngine(db, svc)
	h := NewHandlers(svc, nil, nil, nil, nil)
	h.pipelineEngine = engine

	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/initial-request", strings.NewReader(`{
		"to_role":"tester",
		"method":"run_test",
		"params":{"hello":"world"}
	}`))
	rec := httptest.NewRecorder()
	h.handlePipelines(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	jobID, _ := resp["job_id"].(string)
	if strings.TrimSpace(jobID) == "" {
		t.Fatalf("expected non-empty job_id")
	}
	if got, _ := resp["target_agent_id"].(string); got != "target" {
		t.Fatalf("expected target_agent_id target, got %q", got)
	}

	jobDoc, err := newLocalA2AQueue(db).GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	request, _ := jobDoc["request"].(map[string]any)
	if gotMethod, _ := request["method"].(string); gotMethod != "run_test" {
		t.Fatalf("expected method run_test, got %q", gotMethod)
	}
	params, _ := request["params"].(map[string]any)
	if gotHello, _ := params["hello"].(string); gotHello != "world" {
		t.Fatalf("expected params.hello=world, got %v", params["hello"])
	}
}

func TestHandlePipelineInitialRequestRejectsInputSchemaMismatch(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)

	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethod(
		ctx,
		"run_test",
		"handles test method",
		`{"type":"object","required":["hello"],"properties":{"hello":{"type":"string"}},"additionalProperties":false}`,
		`{"type":"object","properties":{"ok":{"type":"boolean"}}}`,
	); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}

	targetAgent, err := svc.CreateAgent(ctx, "target")
	if err != nil {
		t.Fatalf("create target failed: %v", err)
	}
	targetDataSvc := data.NewAgentService(db, targetAgent.ID)
	if err := targetDataSvc.RegisterCapability(ctx, "tester", "run_test"); err != nil {
		t.Fatalf("register target capability failed: %v", err)
	}

	engine := NewPipelineEngine(db, svc)
	h := NewHandlers(svc, nil, nil, nil, nil)
	h.pipelineEngine = engine

	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/initial-request", strings.NewReader(`{
		"to_role":"tester",
		"method":"run_test",
		"params":{"hello":123}
	}`))
	rec := httptest.NewRecorder()
	h.handlePipelines(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusBadRequest, rec.Code, rec.Body.String())
	}

	jobs, err := db.Table(localA2AJob{}).GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll local_a2a_jobs failed: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected no local A2A jobs to be submitted, got %d", len(jobs))
	}
}

func TestHandlePipelinesRejectsCompanyScopeWithoutCompanyID(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	h := NewHandlers(svc, nil, nil, nil, nil)
	h.pipelineEngine = engine

	createBody := `{
		"id":"company_scope_missing_company",
		"name":"Invalid Company Scope",
		"scope_mode":"company",
		"steps":[
			{
				"on_method":"seed_method",
				"to_role":"tester",
				"next_method":"run_test"
			}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/pipelines", strings.NewReader(createBody))
	rec := httptest.NewRecorder()
	h.handlePipelines(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "scope_company_id is required") {
		t.Fatalf("expected scope_company_id validation error, got %s", rec.Body.String())
	}
}

func TestHandlePipelinesPersistsAndReturnsCompanyScopeFields(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	h := NewHandlers(svc, nil, nil, nil, nil)
	h.pipelineEngine = engine

	company, err := svc.CreateCompany(ctx, "Pipeline Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	createBody := `{
		"id":"company_scope_roundtrip",
		"name":"Company Scope Roundtrip",
		"scope_mode":"company",
		"scope_company_id":"` + company.ID + `",
		"steps":[
			{
				"on_method":"seed_method",
				"to_role":"tester",
				"next_method":"run_test"
			}
		]
	}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/pipelines", strings.NewReader(createBody))
	createRec := httptest.NewRecorder()
	h.handlePipelines(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusCreated, createRec.Code, createRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/pipelines", nil)
	listRec := httptest.NewRecorder()
	h.handlePipelines(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, listRec.Code, listRec.Body.String())
	}

	var pipelines []map[string]any
	if err := json.NewDecoder(listRec.Body).Decode(&pipelines); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	var found map[string]any
	for _, p := range pipelines {
		if id, _ := p["id"].(string); id == "company_scope_roundtrip" {
			found = p
			break
		}
	}
	if found == nil {
		t.Fatalf("expected pipeline company_scope_roundtrip in list response")
	}
	if got, _ := found["scope_mode"].(string); got != "company" {
		t.Fatalf("expected scope_mode company, got %q", got)
	}
	if got, _ := found["scope_company_id"].(string); got != company.ID {
		t.Fatalf("expected scope_company_id %q, got %q", company.ID, got)
	}
}

func TestHandlePipelineCapabilitiesSupportsCompanyFilter(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	h := NewHandlers(svc, nil, nil, nil, nil)
	h.pipelineEngine = engine

	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethod(ctx, "find_candidates", "discover products", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}

	agentA, err := svc.CreateAgent(ctx, "agent-a")
	if err != nil {
		t.Fatalf("CreateAgent(agent-a) failed: %v", err)
	}
	agentASvc := data.NewAgentService(db, agentA.ID)
	if err := agentASvc.RegisterCapability(ctx, "scout", "find_candidates"); err != nil {
		t.Fatalf("RegisterCapability(agent-a) failed: %v", err)
	}

	agentB, err := svc.CreateAgent(ctx, "agent-b")
	if err != nil {
		t.Fatalf("CreateAgent(agent-b) failed: %v", err)
	}
	agentBSvc := data.NewAgentService(db, agentB.ID)
	if err := agentBSvc.RegisterCapability(ctx, "scout", "find_candidates"); err != nil {
		t.Fatalf("RegisterCapability(agent-b) failed: %v", err)
	}

	companyA, err := svc.CreateCompany(ctx, "Company A", "", "")
	if err != nil {
		t.Fatalf("CreateCompany(company A) failed: %v", err)
	}
	companyB, err := svc.CreateCompany(ctx, "Company B", "", "")
	if err != nil {
		t.Fatalf("CreateCompany(company B) failed: %v", err)
	}
	if err := svc.AddAgentToCompany(ctx, companyA.ID, agentA.ID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(agent-a) failed: %v", err)
	}
	if err := svc.AddAgentToCompany(ctx, companyB.ID, agentB.ID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(agent-b) failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/pipelines/capabilities?company_id="+companyA.ID, nil)
	rec := httptest.NewRecorder()
	h.handlePipelines(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	rawCaps, _ := body["capabilities"].([]any)
	if len(rawCaps) != 1 {
		t.Fatalf("expected 1 capability for company filter, got %d", len(rawCaps))
	}
	capMap, ok := rawCaps[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected capability row type: %T", rawCaps[0])
	}
	if got, _ := capMap["agent_id"].(string); got != agentA.ID {
		t.Fatalf("expected filtered capability agent_id %q, got %q", agentA.ID, got)
	}
}

func TestHandlePipelinesMethodGuards(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	h := NewHandlers(svc, nil, nil, nil, nil)
	h.pipelineEngine = engine

	collectionReq := httptest.NewRequest(http.MethodPut, "/api/pipelines", nil)
	collectionRec := httptest.NewRecorder()
	h.handlePipelines(collectionRec, collectionReq)
	if collectionRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected collection wrong-method status %d, got %d body=%s", http.StatusMethodNotAllowed, collectionRec.Code, collectionRec.Body.String())
	}

	capsReq := httptest.NewRequest(http.MethodPost, "/api/pipelines/capabilities", nil)
	capsRec := httptest.NewRecorder()
	h.handlePipelines(capsRec, capsReq)
	if capsRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected capabilities wrong-method status %d, got %d body=%s", http.StatusMethodNotAllowed, capsRec.Code, capsRec.Body.String())
	}

	initialReq := httptest.NewRequest(http.MethodGet, "/api/pipelines/initial-request", nil)
	initialRec := httptest.NewRecorder()
	h.handlePipelines(initialRec, initialReq)
	if initialRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected initial-request wrong-method status %d, got %d body=%s", http.StatusMethodNotAllowed, initialRec.Code, initialRec.Body.String())
	}

	triggerReq := httptest.NewRequest(http.MethodGet, "/api/pipelines/pipeline-a/trigger", nil)
	triggerRec := httptest.NewRecorder()
	h.handlePipelines(triggerRec, triggerReq)
	if triggerRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected trigger wrong-method status %d, got %d body=%s", http.StatusMethodNotAllowed, triggerRec.Code, triggerRec.Body.String())
	}

	polyTriggerReq := httptest.NewRequest(http.MethodGet, "/api/pipelines/pipeline-a/trigger-polymarket", nil)
	polyTriggerRec := httptest.NewRecorder()
	h.handlePipelines(polyTriggerRec, polyTriggerReq)
	if polyTriggerRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected polymarket trigger wrong-method status %d, got %d body=%s", http.StatusMethodNotAllowed, polyTriggerRec.Code, polyTriggerRec.Body.String())
	}

	actionTriggerReq := httptest.NewRequest(http.MethodGet, "/api/pipelines/pipeline-a/actions/trigger", nil)
	actionTriggerRec := httptest.NewRecorder()
	h.handlePipelines(actionTriggerRec, actionTriggerReq)
	if actionTriggerRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected action trigger wrong-method status %d, got %d body=%s", http.StatusMethodNotAllowed, actionTriggerRec.Code, actionTriggerRec.Body.String())
	}
}

func TestHandlePipelinesUnknownRoute(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	h := NewHandlers(svc, nil, nil, nil, nil)
	h.pipelineEngine = engine

	req := httptest.NewRequest(http.MethodGet, "/api/pipelines/pipeline-a/not-real", nil)
	rec := httptest.NewRecorder()
	h.handlePipelines(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d body=%s", http.StatusNotFound, rec.Code, rec.Body.String())
	}
}

func TestHandlePipelinesEngineNotConfigured(t *testing.T) {
	h := &Handlers{}

	listReq := httptest.NewRequest(http.MethodGet, "/api/pipelines", nil)
	listRec := httptest.NewRecorder()
	h.handlePipelines(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected unconfigured GET status %d, got %d body=%s", http.StatusOK, listRec.Code, listRec.Body.String())
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/pipelines", strings.NewReader(`{}`))
	createRec := httptest.NewRecorder()
	h.handlePipelines(createRec, createReq)
	if createRec.Code != http.StatusNotImplemented {
		t.Fatalf("expected unconfigured POST status %d, got %d body=%s", http.StatusNotImplemented, createRec.Code, createRec.Body.String())
	}
}

func TestPipelineRouteRecognitionHelpers(t *testing.T) {
	if !isPipelineCollectionMethod(http.MethodGet) || !isPipelineCollectionMethod(http.MethodPost) {
		t.Fatalf("expected GET/POST to be recognized pipeline collection methods")
	}
	if isPipelineCollectionMethod(http.MethodDelete) {
		t.Fatalf("expected DELETE to be rejected pipeline collection method")
	}

	if !isPipelineDefinitionMethod(http.MethodDelete) {
		t.Fatalf("expected DELETE to be recognized pipeline definition method")
	}
	if isPipelineDefinitionMethod(http.MethodPost) {
		t.Fatalf("expected POST to be rejected pipeline definition method")
	}
}

func TestParsePipelineRoute(t *testing.T) {
	route, err := parsePipelineRoute("/api/pipelines")
	if err != nil {
		t.Fatalf("expected collection route parse success, got %v", err)
	}
	if route.kind != pipelineRouteCollection || route.pipelineID != "" {
		t.Fatalf("unexpected collection route parse: %#v", route)
	}

	route, err = parsePipelineRoute("/api/pipelines/capabilities")
	if err != nil {
		t.Fatalf("expected capabilities route parse success, got %v", err)
	}
	if route.kind != pipelineRouteCapabilities {
		t.Fatalf("unexpected capabilities route parse: %#v", route)
	}

	route, err = parsePipelineRoute("/api/pipelines/initial-request")
	if err != nil {
		t.Fatalf("expected initial-request route parse success, got %v", err)
	}
	if route.kind != pipelineRouteInitialRequest {
		t.Fatalf("unexpected initial-request route parse: %#v", route)
	}

	route, err = parsePipelineRoute("/api/pipelines/pipeline-a")
	if err != nil {
		t.Fatalf("expected definition route parse success, got %v", err)
	}
	if route.kind != pipelineRouteDefinition || route.pipelineID != "pipeline-a" {
		t.Fatalf("unexpected definition route parse: %#v", route)
	}

	route, err = parsePipelineRoute("/api/pipelines/pipeline-a/trigger")
	if err != nil {
		t.Fatalf("expected trigger route parse success, got %v", err)
	}
	if route.kind != pipelineRouteTrigger || route.pipelineID != "pipeline-a" {
		t.Fatalf("unexpected trigger route parse: %#v", route)
	}

	route, err = parsePipelineRoute("/api/pipelines/pipeline-a/trigger-polymarket")
	if err != nil {
		t.Fatalf("expected trigger-polymarket route parse success, got %v", err)
	}
	if route.kind != pipelineRouteTriggerPolymkt || route.pipelineID != "pipeline-a" {
		t.Fatalf("unexpected trigger-polymarket route parse: %#v", route)
	}

	route, err = parsePipelineRoute("/api/pipelines/pipeline-a/actions/trigger-polymarket")
	if err != nil {
		t.Fatalf("expected action route parse success, got %v", err)
	}
	if route.kind != pipelineRouteAction || route.pipelineID != "pipeline-a" || route.action != "trigger-polymarket" {
		t.Fatalf("unexpected action route parse: %#v", route)
	}

	route, err = parsePipelineRoute("/api/pipelines/pipeline-a/not-real")
	if err != nil {
		t.Fatalf("expected unknown route parse success, got %v", err)
	}
	if route.kind != pipelineRouteUnknown || route.pipelineID != "pipeline-a" {
		t.Fatalf("unexpected unknown route parse: %#v", route)
	}

	if _, err := parsePipelineRoute("/api/pipelines/   "); err == nil {
		t.Fatalf("expected missing pipeline ID to fail")
	}
}

func TestResolvePolymarketPipelineCompanyID(t *testing.T) {
	t.Run("global_requires_company_id", func(t *testing.T) {
		_, err := resolvePolymarketPipelineCompanyID(Pipeline{ID: "p1", ScopeMode: "global"}, "")
		if err == nil || !strings.Contains(err.Error(), "company_id is required") {
			t.Fatalf("expected company_id required error, got %v", err)
		}
	})

	t.Run("global_accepts_explicit_company", func(t *testing.T) {
		got, err := resolvePolymarketPipelineCompanyID(Pipeline{ID: "p1", ScopeMode: "global"}, "co-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "co-1" {
			t.Fatalf("expected co-1, got %q", got)
		}
	})

	t.Run("company_scope_defaults_to_pipeline_company", func(t *testing.T) {
		got, err := resolvePolymarketPipelineCompanyID(Pipeline{ID: "p1", ScopeMode: "company", ScopeCompanyID: "co-2"}, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "co-2" {
			t.Fatalf("expected co-2, got %q", got)
		}
	})

	t.Run("company_scope_rejects_mismatch", func(t *testing.T) {
		_, err := resolvePolymarketPipelineCompanyID(Pipeline{ID: "p1", ScopeMode: "company", ScopeCompanyID: "co-2"}, "co-3")
		if err == nil || !strings.Contains(err.Error(), "must match") {
			t.Fatalf("expected mismatch error, got %v", err)
		}
	})
}
