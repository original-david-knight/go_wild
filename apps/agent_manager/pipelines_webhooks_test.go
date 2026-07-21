package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

func TestEnsureSchemaEnforcesWebhookEventIDUniqueness(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	dao := db.Table(data.WebhookEvent{})

	now := time.Now()
	first := &data.WebhookEvent{
		ID:          "event-1",
		EventID:     "shopify-event-123",
		Source:      "shopify",
		Topic:       "orders/create",
		PayloadJSON: `{"id":"1001"}`,
		Status:      "pending",
		Attempts:    0,
		NextRetryAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	second := &data.WebhookEvent{
		ID:          "event-2",
		EventID:     "shopify-event-123",
		Source:      "shopify",
		Topic:       "orders/create",
		PayloadJSON: `{"id":"1001"}`,
		Status:      "pending",
		Attempts:    0,
		NextRetryAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := dao.Insert(ctx, first); err != nil {
		t.Fatalf("first insert failed: %v", err)
	}
	if err := dao.Insert(ctx, second); err == nil {
		t.Fatalf("expected duplicate event_id insert to fail")
	}
}

func TestHandleA2ACallbackRejectsWithoutVerificationConfig(t *testing.T) {
	t.Setenv("A2A_CALLBACK_ALLOWED_KEY_IDS", "")

	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)

	payload := map[string]any{
		"job_id":  "job-1",
		"status":  "succeeded",
		"request": map[string]any{"method": "shopify_order_created", "params": map[string]any{}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/pipeline/callbacks/a2a", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	engine.HandleA2ACallback(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestWebhookCallbackPipelineChain(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	service := NewAgentService(db)

	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethod(ctx, "shopify_order_created", "order trigger", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}
	if _, err := systemSvc.CreateA2AMethod(ctx, "fulfill_order", "fulfillment", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}

	sourceAgent, err := service.CreateAgent(ctx, "source")
	if err != nil {
		t.Fatalf("create source agent failed: %v", err)
	}
	fulfillerAgent, err := service.CreateAgent(ctx, "fulfiller")
	if err != nil {
		t.Fatalf("create fulfiller agent failed: %v", err)
	}
	sourceDataSvc := data.NewAgentService(db, sourceAgent.ID)
	if err := sourceDataSvc.RegisterCapability(ctx, "orders", "shopify_order_created"); err != nil {
		t.Fatalf("register source capability failed: %v", err)
	}
	fulfillerDataSvc := data.NewAgentService(db, fulfillerAgent.ID)
	if err := fulfillerDataSvc.RegisterCapability(ctx, "fulfiller", "fulfill_order"); err != nil {
		t.Fatalf("register fulfiller capability failed: %v", err)
	}
	company, err := service.CreateCompany(ctx, "Webhook Co", "", "")
	if err != nil {
		t.Fatalf("create company failed: %v", err)
	}
	if err := service.AddAgentToCompany(ctx, company.ID, sourceAgent.ID, "orders"); err != nil {
		t.Fatalf("add source agent to company failed: %v", err)
	}
	if err := service.AddAgentToCompany(ctx, company.ID, fulfillerAgent.ID, "fulfiller"); err != nil {
		t.Fatalf("add fulfiller agent to company failed: %v", err)
	}
	if strings.TrimSpace(company.WebhookIngressKey) == "" {
		t.Fatalf("expected company webhook ingress key")
	}

	config := &data.WebhookConfig{
		ID:           "cfg-1",
		CompanyID:    company.ID,
		Source:       "shopify",
		Event:        "orders/create",
		EventPath:    "orders_create",
		TargetRole:   "orders",
		TargetMethod: "shopify_order_created",
		Enabled:      true,
		HMACSecret:   "shopify-secret",
	}
	if err := db.Table(data.WebhookConfig{}).Insert(ctx, config); err != nil {
		t.Fatalf("insert webhook config failed: %v", err)
	}

	stepsJSON, err := json.Marshal([]PipelineStep{
		{
			OnMethod:   "shopify_order_created",
			OnStatus:   "succeeded",
			FromRole:   "*",
			ToRole:     "fulfiller",
			NextMethod: "fulfill_order",
			ParamMap:   map[string]string{"$": "order"},
		},
	})
	if err != nil {
		t.Fatalf("marshal pipeline steps failed: %v", err)
	}
	systemDataSvc := data.NewAgentService(db, "system")
	if err := systemDataSvc.UpsertPipelineDefinition(ctx, &data.PipelineDefinition{
		ID:        "order_fulfillment",
		Name:      "Order to Delivery",
		StepsJSON: string(stepsJSON),
		Enabled:   true,
	}); err != nil {
		t.Fatalf("upsert pipeline definition failed: %v", err)
	}

	webhookRouter := NewWebhookRouter(db)
	pipelineEngine := NewPipelineEngine(db, service)

	shopifyPayload := []byte(`{"id":"order-1001","line_items":[]}`)
	mac := hmac.New(sha256.New, []byte(config.HMACSecret))
	_, _ = mac.Write(shopifyPayload)
	shopifySig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	webhookReq := httptest.NewRequest(http.MethodPost, "/ingress/webhooks/shopify/"+company.WebhookIngressKey+"/orders_create", bytes.NewReader(shopifyPayload))
	webhookReq.Header.Set("X-Shopify-Topic", "orders/create")
	webhookReq.Header.Set("X-Shopify-Hmac-Sha256", shopifySig)
	webhookReq.Header.Set("X-Shopify-Event-Id", "shopify-event-1")
	webhookRec := httptest.NewRecorder()
	webhookRouter.HandleIngressWebhook(webhookRec, webhookReq)
	if webhookRec.Code != http.StatusOK {
		t.Fatalf("webhook status = %d, body = %s", webhookRec.Code, webhookRec.Body.String())
	}

	pending := webhookRouter.getPendingEvents(ctx, 10)
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending webhook event, got %d", len(pending))
	}
	webhookRouter.processEvent(ctx, pending[0])

	var persisted data.WebhookEvent
	if err := db.Table(data.WebhookEvent{}).Get(ctx, pending[0].ID, &persisted); err != nil {
		t.Fatalf("failed loading persisted webhook event: %v", err)
	}
	if persisted.Status != "delivered" {
		t.Fatalf("expected webhook event status delivered, got %q", persisted.Status)
	}

	queue := newLocalA2AQueue(db)
	claimed, err := queue.ClaimJobs(ctx, sourceAgent.ID, 1, 120)
	if err != nil {
		t.Fatalf("ClaimJobs failed: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("expected 1 claimed webhook job, got %d", len(claimed))
	}
	claimedJobID, _ := claimed[0]["job_id"].(string)
	if claimedJobID == "" {
		t.Fatalf("expected claimed webhook job_id")
	}

	completed, err := queue.CompleteJob(ctx, sourceAgent.ID, claimedJobID, "succeeded", map[string]any{
		"id": "order-1001",
	}, nil)
	if err != nil {
		t.Fatalf("CompleteJob failed: %v", err)
	}
	pipelineEngine.RecordCompletion(completed)

	runs, err := pipelineEngine.GetPipelineRuns(ctx, 10)
	if err != nil {
		t.Fatalf("GetPipelineRuns failed: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 pipeline run, got %d", len(runs))
	}
	run := runs[0]
	if run.PipelineID != "order_fulfillment" {
		t.Fatalf("expected pipeline %q, got %q", "order_fulfillment", run.PipelineID)
	}
	if run.TriggerJobID != claimedJobID {
		t.Fatalf("expected trigger job %q, got %q", claimedJobID, run.TriggerJobID)
	}
	if run.Status != "running" {
		t.Fatalf("expected pipeline run status running, got %q", run.Status)
	}

	stepRuns, err := db.Table(data.PipelineStepRun{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"run_id": run.ID},
	})
	if err != nil {
		t.Fatalf("query step runs failed: %v", err)
	}
	if len(stepRuns) != 1 {
		t.Fatalf("expected 1 step run, got %d", len(stepRuns))
	}
	step := stepRuns[0].(*data.PipelineStepRun)
	if step.A2AJobID == "" {
		t.Fatalf("expected non-empty step job_id")
	}
	if step.Status != "running" {
		t.Fatalf("expected step status running, got %q", step.Status)
	}

	nextJob, err := queue.GetJob(ctx, step.A2AJobID)
	if err != nil {
		t.Fatalf("GetJob(next step) failed: %v", err)
	}
	nextRequest, _ := nextJob["request"].(map[string]any)
	if method, _ := nextRequest["method"].(string); method != "fulfill_order" {
		t.Fatalf("expected chained method fulfill_order, got %q", method)
	}
}

func TestValidatePipelineCallbackConfigurationRequiresAllowedKeys(t *testing.T) {
	t.Setenv("PIPELINE_CALLBACK_URL", "https://ingress.example/ingress/callbacks/a2a")
	t.Setenv("A2A_CALLBACK_ALLOWED_KEY_IDS", "")

	_, _, err := validatePipelineCallbackConfiguration()
	if err == nil {
		t.Fatalf("expected callback configuration validation to fail when allowed keys are missing")
	}
}

func TestPipelineA2ACallbackURLWithErrorRejectsInvalidIngressPublicURL(t *testing.T) {
	t.Setenv("PIPELINE_CALLBACK_URL", "")
	t.Setenv("INGRESS_PUBLIC_URL", "http://localhost:8890")

	_, err := pipelineA2ACallbackURLWithError()
	if err == nil {
		t.Fatalf("expected INGRESS_PUBLIC_URL validation error")
	}
}
