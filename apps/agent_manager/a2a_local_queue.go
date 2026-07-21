package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/original-david-knight/go_wild/agent_net"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

const (
	localA2AStatusQueued    = "queued"
	localA2AStatusClaimed   = "claimed"
	localA2AStatusSucceeded = "succeeded"
	localA2AStatusFailed    = "failed"

	localA2ADefaultClaimBatch        = 5
	localA2ADefaultClaimLeaseSeconds = 1800
	localA2AMaxClaimLeaseSeconds     = 3600
)

type localA2AClaimStatusHandlerFunc func(q *localA2AQueue, ctx context.Context, now time.Time, toAgentID string, leaseSeconds int, job *localA2AJob) (map[string]any, error)

var localA2AClaimStatusHandlers = map[string]localA2AClaimStatusHandlerFunc{
	localA2AStatusQueued: func(q *localA2AQueue, ctx context.Context, now time.Time, toAgentID string, leaseSeconds int, job *localA2AJob) (map[string]any, error) {
		return q.claimQueuedJobLocked(ctx, now, toAgentID, leaseSeconds, job)
	},
	localA2AStatusClaimed: func(q *localA2AQueue, _ context.Context, now time.Time, toAgentID string, _ int, job *localA2AJob) (map[string]any, error) {
		return q.claimClaimedJobLocked(now, toAgentID, job)
	},
}

func isLocalA2AClaimStatus(status string) bool {
	_, ok := localA2AClaimStatusHandlers[strings.TrimSpace(status)]
	return ok
}

var managerA2AQueueMu sync.Mutex

var ErrLocalA2AJobNotFound = errors.New("local A2A job not found")

type localA2ARequest struct {
	Protocol       string         `json:"protocol,omitempty"`
	Method         string         `json:"method"`
	Params         map[string]any `json:"params,omitempty"`
	TimeoutSeconds int            `json:"timeout_seconds,omitempty"`
}

// localA2AJob stores manager-local A2A style jobs so workers can claim and complete
// work without any external queue service.
type localA2AJob struct {
	ID             string     `json:"id"`
	FromAgentID    string     `json:"from_agent_id"`
	ToAgentID      string     `json:"to_agent_id"`
	PoolJob        bool       `json:"pool_job,omitempty"`
	IdempotencyKey string     `json:"idempotency_key,omitempty"`
	RequestJSON    string     `json:"request_json"`
	Status         string     `json:"status"`
	ClaimedBy      string     `json:"claimed_by,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	ClaimedAt      *time.Time `json:"claimed_at,omitempty"`
	ResultJSON     string     `json:"result_json,omitempty"`
	ErrorJSON      string     `json:"error_json,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

func (localA2AJob) TableName() string { return "manager_a2a_jobs" }

func init() {
	gowild_data.RegisterFunc(func(db gowild_data.Database) error {
		return db.AddTable(localA2AJob{})
	})
}

type localA2AQueue struct {
	db gowild_data.Database
}

func newLocalA2AQueue(db gowild_data.Database) *localA2AQueue {
	return &localA2AQueue{db: db}
}

func (q *localA2AQueue) Submit(ctx context.Context, fromAgentID, toAgentID, idempotencyKey string, req localA2ARequest) (map[string]any, bool, error) {
	fromAgentID = strings.TrimSpace(fromAgentID)
	toAgentID = strings.TrimSpace(toAgentID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	req.Method = strings.TrimSpace(req.Method)
	if fromAgentID == "" {
		return nil, false, fmt.Errorf("from_agent_id is required")
	}
	if req.Method == "" {
		return nil, false, fmt.Errorf("request.method is required")
	}
	if req.Params == nil {
		req.Params = map[string]any{}
	}
	if strings.TrimSpace(req.Protocol) == "" {
		req.Protocol = gowild_agent_net.A2AProtocolV1
	}

	// Every job is a pool job. Jobs sit in a shared queue and get assigned
	// to an agent only at claim/delivery time (one job per agent at a time).
	toAgentID = ""
	isPoolJob := true

	table := q.db.Table(localA2AJob{})
	if idempotencyKey != "" {
		where := map[string]any{
			"from_agent_id":   fromAgentID,
			"idempotency_key": idempotencyKey,
			"request_json":    mustJSON(req),
		}
		if !isPoolJob {
			where["to_agent_id"] = toAgentID
		}
		existing, err := table.Query(ctx, gowild_data.QueryOpts{
			Where:     where,
			OrderBy:   "created_at",
			OrderDesc: true,
			Limit:     1,
		})
		if err != nil {
			return nil, false, err
		}
		if len(existing) > 0 {
			return q.toResponse(existing[0].(*localA2AJob)), true, nil
		}
	}

	requestJSON, err := json.Marshal(req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to encode request: %w", err)
	}

	now := time.Now().UTC()
	job := &localA2AJob{
		ID:             uuid.New().String(),
		FromAgentID:    fromAgentID,
		ToAgentID:      toAgentID,
		PoolJob:        isPoolJob,
		IdempotencyKey: idempotencyKey,
		RequestJSON:    string(requestJSON),
		Status:         localA2AStatusQueued,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := table.Insert(ctx, job); err != nil {
		return nil, false, err
	}
	return q.toResponse(job), false, nil
}

func (q *localA2AQueue) GetJob(ctx context.Context, jobID string) (map[string]any, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}
	var job localA2AJob
	if err := q.db.Table(localA2AJob{}).Get(ctx, jobID, &job); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("job not found: %w", ErrLocalA2AJobNotFound)
		}
		return nil, fmt.Errorf("job not found: %w", err)
	}
	return q.toResponse(&job), nil
}

// isAgentBusyLocked checks if the agent has any active claimed (non-expired) job.
// Must be called while holding managerA2AQueueMu.
func (q *localA2AQueue) isAgentBusyLocked(ctx context.Context, agentID string, excludeJobID string) bool {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return false
	}
	results, err := q.db.Table(localA2AJob{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{
			"status":     localA2AStatusClaimed,
			"claimed_by": agentID,
		},
	})
	if err != nil || len(results) == 0 {
		return false
	}
	now := time.Now().UTC()
	for _, row := range results {
		job := row.(*localA2AJob)
		if job.ID == excludeJobID {
			continue
		}
		if job.LeaseExpiresAt != nil && now.After(*job.LeaseExpiresAt) {
			continue
		}
		return true
	}
	return false
}

// IsAgentBusy checks if the agent has any active claimed (non-expired) job.
func (q *localA2AQueue) IsAgentBusy(ctx context.Context, agentID string) bool {
	managerA2AQueueMu.Lock()
	defer managerA2AQueueMu.Unlock()
	return q.isAgentBusyLocked(ctx, agentID, "")
}

func (q *localA2AQueue) ClaimJobs(ctx context.Context, toAgentID string, maxJobs, leaseSeconds int) ([]map[string]any, error) {
	toAgentID = strings.TrimSpace(toAgentID)
	if toAgentID == "" {
		return nil, fmt.Errorf("to_agent_id is required")
	}
	if leaseSeconds <= 0 {
		leaseSeconds = localA2ADefaultClaimLeaseSeconds
	}
	if leaseSeconds > localA2AMaxClaimLeaseSeconds {
		leaseSeconds = localA2AMaxClaimLeaseSeconds
	}

	managerA2AQueueMu.Lock()
	defer managerA2AQueueMu.Unlock()

	now := time.Now().UTC()
	if err := q.requeueExpiredClaims(ctx, now); err != nil {
		return nil, err
	}

	// Enforce single-job-per-agent: if already busy, return empty.
	if q.isAgentBusyLocked(ctx, toAgentID, "") {
		return []map[string]any{}, nil
	}

	// Query pool jobs (unassigned). Only claim 1 at a time.
	table := q.db.Table(localA2AJob{})
	results, err := table.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{
			"status":      localA2AStatusQueued,
			"to_agent_id": "",
		},
		OrderBy: "created_at",
		Limit:   1,
	})
	if err != nil {
		return nil, err
	}

	lease := now.Add(time.Duration(leaseSeconds) * time.Second)
	out := make([]map[string]any, 0, len(results))
	for _, row := range results {
		job := row.(*localA2AJob)
		job.ToAgentID = toAgentID
		job.Status = localA2AStatusClaimed
		job.ClaimedBy = toAgentID
		job.UpdatedAt = now
		job.ClaimedAt = &now
		job.LeaseExpiresAt = &lease
		if err := table.Update(ctx, job); err != nil {
			return nil, err
		}
		out = append(out, q.toResponse(job))
	}
	return out, nil
}

// ClaimJob claims a specific queued job for the target agent.
func (q *localA2AQueue) ClaimJob(ctx context.Context, toAgentID, jobID string, leaseSeconds int) (map[string]any, error) {
	toAgentID = strings.TrimSpace(toAgentID)
	jobID = strings.TrimSpace(jobID)
	if toAgentID == "" {
		return nil, fmt.Errorf("to_agent_id is required")
	}
	if jobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}
	if leaseSeconds <= 0 {
		leaseSeconds = localA2ADefaultClaimLeaseSeconds
	}
	if leaseSeconds > localA2AMaxClaimLeaseSeconds {
		leaseSeconds = localA2AMaxClaimLeaseSeconds
	}

	managerA2AQueueMu.Lock()
	defer managerA2AQueueMu.Unlock()

	now := time.Now().UTC()
	if err := q.requeueExpiredClaims(ctx, now); err != nil {
		return nil, err
	}

	table := q.db.Table(localA2AJob{})
	var job localA2AJob
	if err := table.Get(ctx, jobID, &job); err != nil {
		return nil, fmt.Errorf("job not found: %w", err)
	}
	// Pool jobs (empty ToAgentID) get assigned to the claiming agent.
	// Non-pool jobs must match the target agent.
	if strings.TrimSpace(job.ToAgentID) == "" {
		job.ToAgentID = toAgentID
	} else if strings.TrimSpace(job.ToAgentID) != toAgentID {
		return nil, fmt.Errorf("job is assigned to another agent")
	}
	status := strings.TrimSpace(job.Status)
	if !isLocalA2AClaimStatus(status) {
		return nil, fmt.Errorf("invalid job state %q", status)
	}
	handler, ok := localA2AClaimStatusHandlers[status]
	if !ok {
		return nil, fmt.Errorf("invalid job state %q", status)
	}
	return handler(q, ctx, now, toAgentID, leaseSeconds, &job)
}

func (q *localA2AQueue) claimQueuedJobLocked(ctx context.Context, now time.Time, toAgentID string, leaseSeconds int, job *localA2AJob) (map[string]any, error) {
	// Enforce single-job-per-agent: reject if agent already has a different claimed job.
	if q.isAgentBusyLocked(ctx, toAgentID, strings.TrimSpace(job.ID)) {
		return nil, fmt.Errorf("agent already has a claimed job")
	}
	lease := now.Add(time.Duration(leaseSeconds) * time.Second)
	job.Status = localA2AStatusClaimed
	job.ClaimedBy = toAgentID
	job.ClaimedAt = &now
	job.LeaseExpiresAt = &lease
	job.UpdatedAt = now
	if err := q.db.Table(localA2AJob{}).Update(ctx, job); err != nil {
		return nil, err
	}
	return q.toResponse(job), nil
}

func (q *localA2AQueue) claimClaimedJobLocked(now time.Time, toAgentID string, job *localA2AJob) (map[string]any, error) {
	if strings.TrimSpace(job.ClaimedBy) != "" && strings.TrimSpace(job.ClaimedBy) != toAgentID {
		return nil, fmt.Errorf("job is claimed by another agent")
	}
	if job.LeaseExpiresAt != nil && now.After(*job.LeaseExpiresAt) {
		return nil, fmt.Errorf("job lease expired")
	}
	return q.toResponse(job), nil
}

func (q *localA2AQueue) CompleteJob(ctx context.Context, toAgentID, jobID, status string, result map[string]any, errPayload *gowild_agent_net.A2AErrorEnvelope) (map[string]any, error) {
	toAgentID = strings.TrimSpace(toAgentID)
	jobID = strings.TrimSpace(jobID)
	status = strings.ToLower(strings.TrimSpace(status))
	if jobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}
	if status != localA2AStatusSucceeded && status != localA2AStatusFailed {
		return nil, fmt.Errorf("status must be succeeded or failed")
	}

	managerA2AQueueMu.Lock()
	defer managerA2AQueueMu.Unlock()

	table := q.db.Table(localA2AJob{})
	var job localA2AJob
	if err := table.Get(ctx, jobID, &job); err != nil {
		return nil, fmt.Errorf("job not found: %w", err)
	}

	if job.Status != localA2AStatusClaimed {
		return nil, fmt.Errorf("invalid job state %q", job.Status)
	}
	if strings.TrimSpace(job.ClaimedBy) != "" && strings.TrimSpace(job.ClaimedBy) != toAgentID {
		return nil, fmt.Errorf("job is claimed by another agent")
	}
	if job.LeaseExpiresAt != nil && time.Now().UTC().After(*job.LeaseExpiresAt) {
		return nil, fmt.Errorf("job lease expired")
	}

	now := time.Now().UTC()
	job.Status = status
	job.UpdatedAt = now
	job.CompletedAt = &now
	job.ClaimedBy = toAgentID
	job.LeaseExpiresAt = nil

	if status == localA2AStatusSucceeded {
		if result == nil {
			result = map[string]any{}
		}
		blob, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("failed to encode result: %w", err)
		}
		job.ResultJSON = string(blob)
		job.ErrorJSON = ""
	} else {
		if errPayload == nil || strings.TrimSpace(errPayload.Message) == "" {
			return nil, fmt.Errorf("error.message is required when status=failed")
		}
		blob, err := json.Marshal(errPayload)
		if err != nil {
			return nil, fmt.Errorf("failed to encode error payload: %w", err)
		}
		job.ErrorJSON = string(blob)
		job.ResultJSON = ""
	}

	if err := table.Update(ctx, &job); err != nil {
		return nil, err
	}
	return q.toResponse(&job), nil
}

func (q *localA2AQueue) ExtendLease(ctx context.Context, toAgentID, jobID string, leaseSeconds int) (map[string]any, error) {
	toAgentID = strings.TrimSpace(toAgentID)
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}
	if leaseSeconds <= 0 {
		leaseSeconds = localA2ADefaultClaimLeaseSeconds
	}
	if leaseSeconds > localA2AMaxClaimLeaseSeconds {
		leaseSeconds = localA2AMaxClaimLeaseSeconds
	}

	managerA2AQueueMu.Lock()
	defer managerA2AQueueMu.Unlock()

	table := q.db.Table(localA2AJob{})
	var job localA2AJob
	if err := table.Get(ctx, jobID, &job); err != nil {
		return nil, fmt.Errorf("job not found: %w", err)
	}
	if job.Status != localA2AStatusClaimed {
		return nil, fmt.Errorf("invalid job state %q", job.Status)
	}
	if strings.TrimSpace(job.ClaimedBy) != "" && strings.TrimSpace(job.ClaimedBy) != toAgentID {
		return nil, fmt.Errorf("job is claimed by another agent")
	}
	if job.LeaseExpiresAt != nil && time.Now().UTC().After(*job.LeaseExpiresAt) {
		return nil, fmt.Errorf("job lease expired")
	}

	now := time.Now().UTC()
	nextLease := now.Add(time.Duration(leaseSeconds) * time.Second)
	job.UpdatedAt = now
	job.LeaseExpiresAt = &nextLease
	if strings.TrimSpace(job.ClaimedBy) == "" {
		job.ClaimedBy = toAgentID
	}
	if err := table.Update(ctx, &job); err != nil {
		return nil, err
	}
	return q.toResponse(&job), nil
}

// RequeueAgentClaims moves all claimed jobs for a given agent back to queued.
// Call this on agent restart so previously claimed jobs get re-delivered
// instead of waiting for the lease to expire.
func (q *localA2AQueue) RequeueAgentClaims(ctx context.Context, toAgentID string) (int, error) {
	toAgentID = strings.TrimSpace(toAgentID)
	if toAgentID == "" {
		return 0, fmt.Errorf("to_agent_id is required")
	}

	managerA2AQueueMu.Lock()
	defer managerA2AQueueMu.Unlock()

	table := q.db.Table(localA2AJob{})
	results, err := table.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{
			"status":      localA2AStatusClaimed,
			"to_agent_id": toAgentID,
		},
	})
	if err != nil {
		return 0, err
	}

	now := time.Now().UTC()
	requeued := 0
	for _, row := range results {
		job := row.(*localA2AJob)
		job.Status = localA2AStatusQueued
		job.ClaimedBy = ""
		job.ClaimedAt = nil
		job.LeaseExpiresAt = nil
		job.UpdatedAt = now
		// Pool jobs go back to unassigned so any eligible agent can pick them up.
		if job.PoolJob {
			job.ToAgentID = ""
		}
		if err := table.Update(ctx, job); err != nil {
			return requeued, err
		}
		requeued++
	}
	return requeued, nil
}

// RequeueClaimedJob moves a claimed job back to queued for the target agent.
// Used when a claimed job cannot be delivered to the agent runtime.
// Pool jobs are returned to the unassigned pool so any eligible agent can pick them up.
func (q *localA2AQueue) RequeueClaimedJob(ctx context.Context, toAgentID, jobID string) error {
	toAgentID = strings.TrimSpace(toAgentID)
	jobID = strings.TrimSpace(jobID)
	if toAgentID == "" {
		return fmt.Errorf("to_agent_id is required")
	}
	if jobID == "" {
		return fmt.Errorf("job_id is required")
	}

	managerA2AQueueMu.Lock()
	defer managerA2AQueueMu.Unlock()

	table := q.db.Table(localA2AJob{})
	var job localA2AJob
	if err := table.Get(ctx, jobID, &job); err != nil {
		return fmt.Errorf("job not found: %w", err)
	}
	if strings.TrimSpace(job.ToAgentID) != toAgentID {
		return fmt.Errorf("job is assigned to another agent")
	}
	if strings.TrimSpace(job.Status) != localA2AStatusClaimed {
		return nil
	}

	job.Status = localA2AStatusQueued
	job.ClaimedBy = ""
	job.ClaimedAt = nil
	job.LeaseExpiresAt = nil
	job.UpdatedAt = time.Now().UTC()
	// Pool jobs go back to unassigned so any eligible agent can pick them up.
	if job.PoolJob {
		job.ToAgentID = ""
	}
	return table.Update(ctx, &job)
}

func (q *localA2AQueue) requeueExpiredClaims(ctx context.Context, now time.Time) error {
	table := q.db.Table(localA2AJob{})
	results, err := table.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"status": localA2AStatusClaimed},
	})
	if err != nil {
		return err
	}
	for _, row := range results {
		job := row.(*localA2AJob)
		if job.LeaseExpiresAt == nil || now.Before(*job.LeaseExpiresAt) {
			continue
		}
		job.Status = localA2AStatusQueued
		job.ClaimedBy = ""
		job.ClaimedAt = nil
		job.LeaseExpiresAt = nil
		job.UpdatedAt = now
		// Pool jobs go back to unassigned so any eligible agent can pick them up.
		if job.PoolJob {
			job.ToAgentID = ""
		}
		if err := table.Update(ctx, job); err != nil {
			return err
		}
	}
	return nil
}

func (q *localA2AQueue) toResponse(job *localA2AJob) map[string]any {
	request := map[string]any{}
	if strings.TrimSpace(job.RequestJSON) != "" {
		_ = json.Unmarshal([]byte(job.RequestJSON), &request)
	}

	result := map[string]any{}
	if strings.TrimSpace(job.ResultJSON) != "" {
		_ = json.Unmarshal([]byte(job.ResultJSON), &result)
	}

	errPayload := map[string]any{}
	if strings.TrimSpace(job.ErrorJSON) != "" {
		_ = json.Unmarshal([]byte(job.ErrorJSON), &errPayload)
	}

	resp := map[string]any{
		"id":              job.ID,
		"job_id":          job.ID,
		"from_public_key": job.FromAgentID,
		"to_public_key":   job.ToAgentID,
		"status":          job.Status,
		"request":         request,
		"request_json":    strings.TrimSpace(job.RequestJSON),
		"created_at":      job.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":      job.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if strings.TrimSpace(job.ResultJSON) != "" {
		resp["result"] = result
		resp["result_json"] = strings.TrimSpace(job.ResultJSON)
	}
	if strings.TrimSpace(job.ErrorJSON) != "" {
		resp["error"] = errPayload
		resp["error_json"] = strings.TrimSpace(job.ErrorJSON)
	}
	if job.CompletedAt != nil {
		resp["completed_at"] = job.CompletedAt.UTC().Format(time.RFC3339)
	}
	if job.LeaseExpiresAt != nil {
		resp["lease_expires_at"] = job.LeaseExpiresAt.UTC().Format(time.RFC3339)
	}
	return resp
}

func mustJSON(v any) string {
	blob, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(blob)
}
