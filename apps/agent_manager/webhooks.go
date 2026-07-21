package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

// WebhookRouter receives external webhooks and converts them to A2A jobs.
type WebhookRouter struct {
	db gowild_data.Database
}

type ingressWebhookRoute struct {
	provider   string
	companyKey string
	eventPath  string
}

type webhookProviderHandlerFunc func(wr *WebhookRouter, w http.ResponseWriter, r *http.Request, companyID string, config *data.WebhookConfig)

var webhookProviderHandlers = map[string]webhookProviderHandlerFunc{
	"shopify": func(wr *WebhookRouter, w http.ResponseWriter, r *http.Request, companyID string, config *data.WebhookConfig) {
		wr.handleShopifyWebhookWithConfig(w, r, companyID, config)
	},
}

func isWebhookProvider(provider string) bool {
	_, ok := webhookProviderHandlers[provider]
	return ok
}

func parseIngressWebhookRoute(path string) (ingressWebhookRoute, error) {
	trimmed := strings.TrimPrefix(path, "/ingress/webhooks/")
	parts := strings.SplitN(trimmed, "/", 4)
	if len(parts) < 3 {
		return ingressWebhookRoute{}, fmt.Errorf("ingress path must be /ingress/webhooks/{provider}/{company_key}/{event_path}")
	}

	route := ingressWebhookRoute{
		provider:   strings.TrimSpace(strings.ToLower(parts[0])),
		companyKey: strings.TrimSpace(parts[1]),
		eventPath:  strings.TrimSpace(parts[2]),
	}
	if route.provider == "" || route.companyKey == "" || route.eventPath == "" {
		return ingressWebhookRoute{}, fmt.Errorf("provider, company_key, and event_path are required")
	}
	return route, nil
}

// NewWebhookRouter creates a new webhook router.
func NewWebhookRouter(db gowild_data.Database) *WebhookRouter {
	return &WebhookRouter{db: db}
}

// HandleShopify handles legacy incoming Shopify webhooks.
// POST /webhooks/shopify
func (wr *WebhookRouter) HandleShopify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	topic := strings.TrimSpace(r.Header.Get("X-Shopify-Topic"))
	if topic == "" {
		writeError(w, http.StatusBadRequest, "missing X-Shopify-Topic header")
		return
	}
	config, err := wr.getLegacyWebhookConfig(r.Context(), "shopify", topic)
	if err != nil {
		log.Printf("No webhook config for shopify/%s: %v", topic, err)
		writeError(w, http.StatusNotFound, "no webhook config for this event")
		return
	}
	wr.handleShopifyWebhookWithConfig(w, r, "", config)
}

// HandleIngressWebhook handles path-multiplexed webhooks.
// POST /ingress/webhooks/{provider}/{company_key}/{event_path}
func (wr *WebhookRouter) HandleIngressWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	route, err := parseIngressWebhookRoute(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	company, err := data.GetCompanyByWebhookIngressKey(r.Context(), wr.db, route.companyKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve company: "+err.Error())
		return
	}
	if company == nil {
		writeError(w, http.StatusNotFound, "unknown company webhook key")
		return
	}

	config, err := wr.getCompanyWebhookConfigByPath(r.Context(), company.ID, route.provider, route.eventPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load webhook config: "+err.Error())
		return
	}
	if config == nil {
		writeError(w, http.StatusNotFound, "no webhook config for this route")
		return
	}

	if !isWebhookProvider(route.provider) {
		writeError(w, http.StatusNotFound, "unsupported provider")
		return
	}
	handler, ok := webhookProviderHandlers[route.provider]
	if !ok {
		writeError(w, http.StatusNotFound, "unsupported provider")
		return
	}
	handler(wr, w, r, company.ID, config)
}

func (wr *WebhookRouter) handleShopifyWebhookWithConfig(w http.ResponseWriter, r *http.Request, companyID string, config *data.WebhookConfig) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	// Verify HMAC signature
	if strings.TrimSpace(config.HMACSecret) == "" {
		writeError(w, http.StatusUnauthorized, "webhook secret not configured")
		return
	}
	hmacHeader := strings.TrimSpace(r.Header.Get("X-Shopify-Hmac-Sha256"))
	if hmacHeader == "" {
		writeError(w, http.StatusUnauthorized, "missing HMAC signature")
		return
	}
	if !verifyShopifyHMAC(body, hmacHeader, config.HMACSecret) {
		writeError(w, http.StatusUnauthorized, "invalid HMAC signature")
		return
	}

	topic := strings.TrimSpace(r.Header.Get("X-Shopify-Topic"))
	if topic == "" {
		topic = strings.TrimSpace(config.Event)
	}
	if strings.TrimSpace(config.Event) != "" && topic != strings.TrimSpace(config.Event) {
		writeError(w, http.StatusBadRequest, "shopify topic mismatch")
		return
	}

	// Deduplicate by event ID
	eventID := strings.TrimSpace(r.Header.Get("X-Shopify-Event-Id"))
	if eventID == "" {
		eventID = uuid.New().String()
	}

	// Check for existing event (idempotent)
	if wr.eventExists(r.Context(), eventID) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
		return
	}

	// Persist event
	now := time.Now()
	event := &data.WebhookEvent{
		ID:          uuid.New().String(),
		EventID:     eventID,
		CompanyID:   strings.TrimSpace(companyID),
		Source:      "shopify",
		Topic:       topic,
		PayloadJSON: string(body),
		Status:      "pending",
		Attempts:    0,
		NextRetryAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	dao := wr.db.Table(data.WebhookEvent{})
	if err := dao.Insert(r.Context(), event); err != nil {
		if isUniqueConstraintViolation(err) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
			return
		}
		log.Printf("Failed to persist webhook event: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to persist event")
		return
	}

	// Fast ACK - async processing happens in ProcessPendingEvents
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted", "event_id": event.ID})
}

// ProcessPendingEvents runs as a background goroutine, processing pending webhook events.
func (wr *WebhookRouter) ProcessPendingEvents(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			events := wr.getPendingEvents(ctx, 10)
			for _, event := range events {
				wr.processEvent(ctx, event)
			}
		}
	}
}

// processEvent handles a single pending webhook event.
func (wr *WebhookRouter) processEvent(ctx context.Context, event *data.WebhookEvent) {
	const maxAttempts = 5

	// Look up webhook config for source+topic
	config, err := wr.getWebhookConfigForEvent(ctx, event)
	if err != nil {
		log.Printf("No config for webhook %s/%s: %v", event.Source, event.Topic, err)
		wr.markEventFailed(ctx, event, maxAttempts)
		return
	}

	// Resolve target agent by role via capabilities
	agentID, err := wr.findTargetAgent(ctx, strings.TrimSpace(event.CompanyID), config.TargetRole, config.TargetMethod)
	if err != nil {
		log.Printf("No agent for capability %s/%s: %v", config.TargetRole, config.TargetMethod, err)
		wr.markEventFailed(ctx, event, maxAttempts)
		return
	}

	// Submit manager-local queued job.
	var payload map[string]any
	json.Unmarshal([]byte(event.PayloadJSON), &payload)
	if err := validatePayloadForMethod(ctx, wr.db, config.TargetMethod, capabilitySchemaInput, payload); err != nil {
		log.Printf("Webhook payload failed schema validation for %s/%s: %v", config.TargetRole, config.TargetMethod, err)
		wr.markEventFailed(ctx, event, maxAttempts)
		return
	}

	_, _, err = newLocalA2AQueue(wr.db).Submit(ctx, "webhook:"+event.Source, agentID, webhookA2AIdempotencyKey(event, config), localA2ARequest{
		Method: config.TargetMethod,
		Params: payload,
	})
	if err != nil {
		log.Printf("Failed to enqueue webhook job %s: %v", event.ID, err)
		wr.markEventFailed(ctx, event, maxAttempts)
		return
	}

	// Mark as delivered
	event.Status = "delivered"
	event.UpdatedAt = time.Now()
	wr.db.Table(data.WebhookEvent{}).Update(ctx, event)
}

// markEventFailed increments attempts and sets retry or dead-letters.
func (wr *WebhookRouter) markEventFailed(ctx context.Context, event *data.WebhookEvent, maxAttempts int) {
	event.Attempts++
	event.UpdatedAt = time.Now()

	if event.Attempts >= maxAttempts {
		event.Status = "dead_letter"
	} else {
		event.Status = "failed"
		// Exponential backoff: 30s, 2min, 8min, 32min
		backoff := time.Duration(math.Pow(4, float64(event.Attempts))) * 30 * time.Second
		event.NextRetryAt = time.Now().Add(backoff)
	}

	wr.db.Table(data.WebhookEvent{}).Update(ctx, event)
}

// getWebhookConfig looks up the webhook config for a source+event.
func (wr *WebhookRouter) getLegacyWebhookConfig(ctx context.Context, source, event string) (*data.WebhookConfig, error) {
	dao := wr.db.Table(data.WebhookConfig{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"company_id": "", "source": source, "event": event, "enabled": true},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no webhook config for %s/%s", source, event)
	}
	return results[0].(*data.WebhookConfig), nil
}

func (wr *WebhookRouter) getCompanyWebhookConfigByPath(ctx context.Context, companyID, source, eventPath string) (*data.WebhookConfig, error) {
	source = strings.TrimSpace(strings.ToLower(source))
	eventPath = normalizeWebhookEventPath(eventPath)
	dao := wr.db.Table(data.WebhookConfig{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{
			"company_id": companyID,
			"source":     source,
			"event_path": eventPath,
			"enabled":    true,
		},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].(*data.WebhookConfig), nil
}

func (wr *WebhookRouter) getWebhookConfigForEvent(ctx context.Context, event *data.WebhookEvent) (*data.WebhookConfig, error) {
	if event == nil {
		return nil, fmt.Errorf("webhook event is nil")
	}
	companyID := strings.TrimSpace(event.CompanyID)
	if companyID != "" {
		dao := wr.db.Table(data.WebhookConfig{})
		results, err := dao.Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{
				"company_id": companyID,
				"source":     strings.TrimSpace(event.Source),
				"event":      strings.TrimSpace(event.Topic),
				"enabled":    true,
			},
			Limit: 1,
		})
		if err != nil {
			return nil, err
		}
		if len(results) > 0 {
			return results[0].(*data.WebhookConfig), nil
		}
	}
	return wr.getLegacyWebhookConfig(ctx, strings.TrimSpace(event.Source), strings.TrimSpace(event.Topic))
}

// findTargetAgent finds the agent ID that provides the given role/method capability.
func (wr *WebhookRouter) findTargetAgent(ctx context.Context, companyID, role, method string) (string, error) {
	// Use a temporary AgentService (agent-agnostic) to search across all capabilities
	svc := data.NewAgentService(wr.db, "")
	if strings.TrimSpace(companyID) != "" {
		return svc.FindAgentByCapabilityInCompany(ctx, role, method, strings.TrimSpace(companyID))
	}
	return svc.FindAgentByCapability(ctx, role, method)
}

// eventExists checks if a webhook event with the given external event ID already exists.
func (wr *WebhookRouter) eventExists(ctx context.Context, eventID string) bool {
	dao := wr.db.Table(data.WebhookEvent{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"event_id": eventID},
		Limit: 1,
	})
	return err == nil && len(results) > 0
}

// getPendingEvents returns up to limit events that are ready for processing.
func (wr *WebhookRouter) getPendingEvents(ctx context.Context, limit int) []*data.WebhookEvent {
	dao := wr.db.Table(data.WebhookEvent{})
	now := time.Now()

	// Get pending events
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"status": "pending"},
		OrderBy: "created_at",
		Limit:   limit,
	})
	if err != nil {
		return nil
	}

	// Also get failed events ready for retry
	failed, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"status": "failed"},
		OrderBy: "next_retry_at",
		Limit:   limit,
	})
	if err == nil {
		results = append(results, failed...)
	}

	var events []*data.WebhookEvent
	for _, r := range results {
		event := r.(*data.WebhookEvent)
		if event.Status == "pending" || event.NextRetryAt.Before(now) {
			events = append(events, event)
			if len(events) >= limit {
				break
			}
		}
	}
	return events
}

// verifyShopifyHMAC verifies the Shopify webhook HMAC signature.
func verifyShopifyHMAC(body []byte, hmacHeader string, secret string) bool {
	expectedSig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(hmacHeader))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	computed := mac.Sum(nil)
	return hmac.Equal(computed, expectedSig)
}

func webhookA2AIdempotencyKey(event *data.WebhookEvent, config *data.WebhookConfig) string {
	baseEventID := strings.TrimSpace(event.EventID)
	if baseEventID == "" {
		baseEventID = strings.TrimSpace(event.ID)
	}
	return fmt.Sprintf(
		"webhook:%s:%s:%s:%s:%s:%s",
		strings.TrimSpace(event.CompanyID),
		strings.TrimSpace(event.Source),
		strings.TrimSpace(event.Topic),
		strings.TrimSpace(config.TargetRole),
		strings.TrimSpace(config.TargetMethod),
		baseEventID,
	)
}

func isUniqueConstraintViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key value") ||
		strings.Contains(msg, "constraint failed")
}
