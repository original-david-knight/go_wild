package gowild_agent_net

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/data"
	"github.com/google/uuid"
)

var (
	ErrA2AJobNotFound         = errors.New("a2a job not found")
	ErrA2AForbidden           = errors.New("forbidden")
	ErrA2AInvalidState        = errors.New("invalid a2a job state")
	ErrA2ALeaseExpired        = errors.New("a2a job lease expired")
	ErrA2ARecipientNotPremium = errors.New("recipient is not premium")
)

// A2ACleanupResult summarizes queue maintenance work.
type A2ACleanupResult struct {
	Expired       int
	Requeued      int
	FailedLeases  int
	CallbacksDue  int
	CallbacksDead int
}

// SubmitA2AJob creates a durable async request.
// Returns (job, true, nil) when idempotency_key matched an existing job.
func (s *Service) SubmitA2AJob(ctx context.Context, fromPubKey string, input A2ASubmitInput) (*A2AJob, bool, error) {
	toPubKey := strings.TrimSpace(input.ToPublicKey)
	if fromPubKey == "" || toPubKey == "" {
		return nil, false, fmt.Errorf("from_public_key and to_public_key are required")
	}
	if fromPubKey == toPubKey {
		return nil, false, fmt.Errorf("cannot send a2a request to self")
	}
	if _, err := DecodePublicKey(toPubKey); err != nil {
		return nil, false, fmt.Errorf("invalid to_public_key: %w", err)
	}

	isPremium, err := s.IsPremium(ctx, toPubKey)
	if err != nil {
		return nil, false, fmt.Errorf("failed to check recipient premium status: %w", err)
	}
	if !isPremium {
		return nil, false, ErrA2ARecipientNotPremium
	}
	isRevoked, err := s.IsKeyRevoked(ctx, toPubKey)
	if err != nil {
		return nil, false, fmt.Errorf("failed to check recipient key status: %w", err)
	}
	if isRevoked {
		return nil, false, fmt.Errorf("recipient key is revoked")
	}

	req := input.Request
	if req.Protocol == "" {
		req.Protocol = A2AProtocolV1
	}
	if req.Method == "" {
		return nil, false, fmt.Errorf("request.method is required")
	}
	if req.Params == nil {
		req.Params = map[string]any{}
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = A2ADefaultTimeoutSeconds
	}
	if req.TimeoutSeconds > A2AMaxTimeoutSeconds {
		req.TimeoutSeconds = A2AMaxTimeoutSeconds
	}

	callbackURL := strings.TrimSpace(input.CallbackURL)
	if callbackURL != "" {
		u, err := url.Parse(callbackURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, false, fmt.Errorf("invalid callback_url")
		}
		if strings.ToLower(u.Scheme) != "https" {
			return nil, false, fmt.Errorf("callback_url must use https")
		}
	}

	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey != "" {
		existing, err := s.findA2AByIdempotency(ctx, fromPubKey, idempotencyKey)
		if err != nil {
			return nil, false, err
		}
		if existing != nil {
			return existing, true, nil
		}
	}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to serialize request: %w", err)
	}
	if len(reqJSON) > A2ARequestMaxBytes {
		return nil, false, fmt.Errorf("request too large: %d bytes exceeds maximum %d", len(reqJSON), A2ARequestMaxBytes)
	}

	now := time.Now().UTC()
	job := &A2AJob{
		ID:             uuid.NewString(),
		FromPublicKey:  fromPubKey,
		ToPublicKey:    toPubKey,
		Status:         A2AJobStatusQueued,
		IdempotencyKey: idempotencyKey,
		RequestJSON:    string(reqJSON),
		CallbackURL:    callbackURL,
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now.Add(A2ADefaultJobTTL),
	}
	if callbackURL == "" {
		job.CallbackStatus = A2ACallbackStatusNone
	} else {
		job.CallbackStatus = A2ACallbackStatusPending
	}

	if err := s.db.Table(A2AJob{}).Insert(ctx, job); err != nil {
		return nil, false, fmt.Errorf("failed to create a2a job: %w", err)
	}

	s.insertA2AEvent(ctx, job.ID, "submitted", map[string]any{
		"from_public_key": fromPubKey,
		"to_public_key":   toPubKey,
		"method":          req.Method,
		"protocol":        req.Protocol,
	})

	return job, false, nil
}

// GetA2AJobForAgent returns a job if caller is sender or recipient.
func (s *Service) GetA2AJobForAgent(ctx context.Context, agentPubKey, jobID string) (*A2AJob, error) {
	var job A2AJob
	if err := s.db.Table(A2AJob{}).Get(ctx, jobID, &job); err != nil {
		return nil, ErrA2AJobNotFound
	}
	if job.FromPublicKey != agentPubKey && job.ToPublicKey != agentPubKey {
		return nil, ErrA2AForbidden
	}
	return &job, nil
}

// ClaimA2AJobs atomically claims queued jobs for a recipient.
func (s *Service) ClaimA2AJobs(ctx context.Context, recipientPubKey string, input A2AClaimInput) ([]*A2AJob, error) {
	maxJobs := input.MaxJobs
	if maxJobs <= 0 {
		maxJobs = A2ADefaultClaimBatch
	}
	if maxJobs > A2AMaxClaimBatch {
		maxJobs = A2AMaxClaimBatch
	}

	leaseSeconds := input.LeaseSeconds
	if leaseSeconds <= 0 {
		leaseSeconds = A2ADefaultClaimLeaseSeconds
	}
	if leaseSeconds > A2AMaxClaimLeaseSeconds {
		leaseSeconds = A2AMaxClaimLeaseSeconds
	}

	s.a2aMu.Lock()
	defer s.a2aMu.Unlock()

	now := time.Now().UTC()
	if _, err := s.cleanupA2ALocked(ctx, now); err != nil {
		return nil, err
	}

	results, err := s.db.Table(A2AJob{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{
			"to_public_key": recipientPubKey,
			"status":        A2AJobStatusQueued,
		},
		OrderBy: "created_at",
		Limit:   maxJobs * 4,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query queued a2a jobs: %w", err)
	}

	claimed := make([]*A2AJob, 0, maxJobs)
	leaseExpiry := now.Add(time.Duration(leaseSeconds) * time.Second)
	for _, r := range results {
		if len(claimed) >= maxJobs {
			break
		}
		job, ok := r.(*A2AJob)
		if !ok {
			continue
		}
		if now.After(job.ExpiresAt) {
			job.Status = A2AJobStatusExpired
			job.UpdatedAt = now
			if err := s.db.Table(A2AJob{}).Update(ctx, job); err == nil {
				s.insertA2AEvent(ctx, job.ID, "expired", nil)
			}
			continue
		}

		job.Status = A2AJobStatusClaimed
		job.ClaimedBy = recipientPubKey
		job.ClaimedAt = &now
		job.LeaseExpiresAt = &leaseExpiry
		job.UpdatedAt = now

		if err := s.db.Table(A2AJob{}).Update(ctx, job); err != nil {
			return nil, fmt.Errorf("failed to claim a2a job: %w", err)
		}
		s.insertA2AEvent(ctx, job.ID, "claimed", map[string]any{
			"claimed_by":       recipientPubKey,
			"lease_expires_at": leaseExpiry.Format(time.RFC3339),
		})
		claimed = append(claimed, job)
	}

	return claimed, nil
}

// CompleteA2AJob completes a claimed job with success or failure.
func (s *Service) CompleteA2AJob(ctx context.Context, recipientPubKey, jobID, status string, result map[string]any, errPayload *A2AErrorEnvelope) (*A2AJob, error) {
	if status != A2AJobStatusSucceeded && status != A2AJobStatusFailed {
		return nil, fmt.Errorf("status must be %q or %q", A2AJobStatusSucceeded, A2AJobStatusFailed)
	}

	s.a2aMu.Lock()
	defer s.a2aMu.Unlock()

	now := time.Now().UTC()
	var job A2AJob
	if err := s.db.Table(A2AJob{}).Get(ctx, jobID, &job); err != nil {
		return nil, ErrA2AJobNotFound
	}
	if job.ToPublicKey != recipientPubKey {
		return nil, ErrA2AForbidden
	}
	if job.Status != A2AJobStatusClaimed || job.ClaimedBy != recipientPubKey {
		return nil, ErrA2AInvalidState
	}
	if job.LeaseExpiresAt == nil || now.After(*job.LeaseExpiresAt) {
		return nil, ErrA2ALeaseExpired
	}

	job.Status = status
	job.CompletedAt = &now
	job.UpdatedAt = now
	job.LeaseExpiresAt = nil
	job.ClaimedBy = ""

	switch status {
	case A2AJobStatusSucceeded:
		if result == nil {
			result = map[string]any{}
		}
		blob, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize result: %w", err)
		}
		if len(blob) > A2ARequestMaxBytes {
			return nil, fmt.Errorf("result too large: %d bytes exceeds maximum %d", len(blob), A2ARequestMaxBytes)
		}
		job.ResultJSON = string(blob)
		job.ErrorJSON = ""
	case A2AJobStatusFailed:
		if errPayload == nil {
			return nil, fmt.Errorf("error payload required for failed status")
		}
		if strings.TrimSpace(errPayload.Message) == "" {
			return nil, fmt.Errorf("error.message is required")
		}
		blob, err := json.Marshal(errPayload)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize error payload: %w", err)
		}
		if len(blob) > A2ARequestMaxBytes {
			return nil, fmt.Errorf("error payload too large: %d bytes exceeds maximum %d", len(blob), A2ARequestMaxBytes)
		}
		job.ErrorJSON = string(blob)
		job.ResultJSON = ""
	}

	if job.CallbackURL != "" {
		job.CallbackStatus = A2ACallbackStatusPending
		job.NextCallbackAt = &now
		job.LastCallbackErr = ""
	} else {
		job.CallbackStatus = A2ACallbackStatusNone
		job.NextCallbackAt = nil
	}

	if err := s.db.Table(A2AJob{}).Update(ctx, &job); err != nil {
		return nil, fmt.Errorf("failed to update a2a job: %w", err)
	}
	s.insertA2AEvent(ctx, job.ID, "completed", map[string]any{"status": status})
	return &job, nil
}

// ExtendA2ALease extends the claim lease for an in-progress job.
func (s *Service) ExtendA2ALease(ctx context.Context, recipientPubKey, jobID string, leaseSeconds int) (*A2AJob, error) {
	if leaseSeconds <= 0 {
		leaseSeconds = A2ADefaultClaimLeaseSeconds
	}
	if leaseSeconds > A2AMaxClaimLeaseSeconds {
		leaseSeconds = A2AMaxClaimLeaseSeconds
	}

	s.a2aMu.Lock()
	defer s.a2aMu.Unlock()

	now := time.Now().UTC()
	var job A2AJob
	if err := s.db.Table(A2AJob{}).Get(ctx, jobID, &job); err != nil {
		return nil, ErrA2AJobNotFound
	}
	if job.ToPublicKey != recipientPubKey {
		return nil, ErrA2AForbidden
	}
	if job.Status != A2AJobStatusClaimed || job.ClaimedBy != recipientPubKey {
		return nil, ErrA2AInvalidState
	}
	if job.LeaseExpiresAt == nil || now.After(*job.LeaseExpiresAt) {
		return nil, ErrA2ALeaseExpired
	}

	lease := now.Add(time.Duration(leaseSeconds) * time.Second)
	job.LeaseExpiresAt = &lease
	job.UpdatedAt = now
	if err := s.db.Table(A2AJob{}).Update(ctx, &job); err != nil {
		return nil, fmt.Errorf("failed to extend a2a lease: %w", err)
	}
	s.insertA2AEvent(ctx, job.ID, "lease_extended", map[string]any{"lease_expires_at": lease.Format(time.RFC3339)})
	return &job, nil
}

// CleanupA2AQueue expires stale jobs and requeues/failed jobs with expired leases.
func (s *Service) CleanupA2AQueue(ctx context.Context) (A2ACleanupResult, error) {
	s.a2aMu.Lock()
	defer s.a2aMu.Unlock()
	return s.cleanupA2ALocked(ctx, time.Now().UTC())
}

// GetDueA2ACallbackJobs returns jobs ready for callback delivery attempts.
func (s *Service) GetDueA2ACallbackJobs(ctx context.Context, limit int) ([]*A2AJob, error) {
	if limit <= 0 {
		limit = 50
	}

	pending, err := s.db.Table(A2AJob{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"callback_status": A2ACallbackStatusPending},
	})
	if err != nil {
		return nil, err
	}
	retrying, err := s.db.Table(A2AJob{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"callback_status": A2ACallbackStatusRetrying},
	})
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	due := make([]*A2AJob, 0, len(pending)+len(retrying))
	for _, set := range [][]any{pending, retrying} {
		for _, r := range set {
			job, ok := r.(*A2AJob)
			if !ok {
				continue
			}
			if job.CallbackURL == "" {
				continue
			}
			if job.Status != A2AJobStatusSucceeded && job.Status != A2AJobStatusFailed {
				continue
			}
			if job.NextCallbackAt != nil && now.Before(*job.NextCallbackAt) {
				continue
			}
			due = append(due, job)
		}
	}

	sort.SliceStable(due, func(i, j int) bool {
		ia, ja := due[i].NextCallbackAt, due[j].NextCallbackAt
		switch {
		case ia == nil && ja == nil:
			return due[i].CreatedAt.Before(due[j].CreatedAt)
		case ia == nil:
			return true
		case ja == nil:
			return false
		case ia.Equal(*ja):
			return due[i].CreatedAt.Before(due[j].CreatedAt)
		default:
			return ia.Before(*ja)
		}
	})

	if len(due) > limit {
		due = due[:limit]
	}
	return due, nil
}

// RecordA2ACallbackOutcome records callback delivery success/failure.
func (s *Service) RecordA2ACallbackOutcome(ctx context.Context, jobID string, delivered bool, lastErr string) (*A2AJob, error) {
	s.a2aMu.Lock()
	defer s.a2aMu.Unlock()

	now := time.Now().UTC()
	var job A2AJob
	if err := s.db.Table(A2AJob{}).Get(ctx, jobID, &job); err != nil {
		return nil, ErrA2AJobNotFound
	}

	job.CallbackAttempts++
	job.UpdatedAt = now

	if delivered {
		job.CallbackStatus = A2ACallbackStatusDelivered
		job.NextCallbackAt = nil
		job.LastCallbackErr = ""
		if err := s.db.Table(A2AJob{}).Update(ctx, &job); err != nil {
			return nil, err
		}
		s.insertA2AEvent(ctx, job.ID, "callback_delivered", map[string]any{"attempts": job.CallbackAttempts})
		return &job, nil
	}

	job.LastCallbackErr = strings.TrimSpace(lastErr)
	dead := job.CallbackAttempts >= A2ACallbackMaxAttempts
	if job.CompletedAt != nil && now.Sub(*job.CompletedAt) > A2ACallbackRetryWindow {
		dead = true
	}
	if dead {
		job.CallbackStatus = A2ACallbackStatusDeadLetter
		job.NextCallbackAt = nil
		if err := s.db.Table(A2AJob{}).Update(ctx, &job); err != nil {
			return nil, err
		}
		s.insertA2AEvent(ctx, job.ID, "callback_dead_lettered", map[string]any{
			"attempts": job.CallbackAttempts,
			"error":    job.LastCallbackErr,
		})
		return &job, nil
	}

	delay := callbackBackoff(job.CallbackAttempts)
	nextAt := now.Add(delay)
	job.CallbackStatus = A2ACallbackStatusRetrying
	job.NextCallbackAt = &nextAt
	if err := s.db.Table(A2AJob{}).Update(ctx, &job); err != nil {
		return nil, err
	}
	s.insertA2AEvent(ctx, job.ID, "callback_retry_scheduled", map[string]any{
		"attempts":      job.CallbackAttempts,
		"next_callback": nextAt.Format(time.RFC3339),
		"error":         job.LastCallbackErr,
	})
	return &job, nil
}

func callbackBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	// Exponential backoff capped to 30 minutes.
	exp := attempt - 1
	if exp > 10 {
		exp = 10
	}
	base := time.Second * time.Duration(1<<exp)
	if base > 30*time.Minute {
		base = 30 * time.Minute
	}

	// Add 0-20% jitter to avoid callback retry stampedes.
	jitterMax := int64(base / 5)
	if jitterMax <= 0 {
		return base
	}
	n, err := rand.Int(rand.Reader, big.NewInt(jitterMax+1))
	if err != nil {
		return base
	}
	return base + time.Duration(n.Int64())
}

func (s *Service) cleanupA2ALocked(ctx context.Context, now time.Time) (A2ACleanupResult, error) {
	res := A2ACleanupResult{}
	models, err := s.db.Table(A2AJob{}).GetAll(ctx)
	if err != nil {
		return res, fmt.Errorf("failed to query a2a jobs: %w", err)
	}

	for _, m := range models {
		job, ok := m.(*A2AJob)
		if !ok {
			continue
		}

		if (job.Status == A2AJobStatusQueued || job.Status == A2AJobStatusClaimed) && now.After(job.ExpiresAt) {
			job.Status = A2AJobStatusExpired
			job.UpdatedAt = now
			job.LeaseExpiresAt = nil
			job.ClaimedBy = ""
			if err := s.db.Table(A2AJob{}).Update(ctx, job); err != nil {
				return res, err
			}
			s.insertA2AEvent(ctx, job.ID, "expired", nil)
			res.Expired++
			continue
		}

		if job.Status == A2AJobStatusClaimed && job.LeaseExpiresAt != nil && now.After(*job.LeaseExpiresAt) {
			if job.Redelivery+1 > A2AMaxRedelivery {
				errPayload := A2AErrorEnvelope{
					Code:    "LEASE_TIMEOUT",
					Message: "job exceeded maximum claim retries",
				}
				blob, _ := json.Marshal(errPayload)
				job.Status = A2AJobStatusFailed
				job.ErrorJSON = string(blob)
				job.ClaimedBy = ""
				job.LeaseExpiresAt = nil
				job.UpdatedAt = now
				job.CompletedAt = &now
				if job.CallbackURL != "" {
					job.CallbackStatus = A2ACallbackStatusPending
					job.NextCallbackAt = &now
				}
				if err := s.db.Table(A2AJob{}).Update(ctx, job); err != nil {
					return res, err
				}
				s.insertA2AEvent(ctx, job.ID, "lease_failed_max_retries", map[string]any{"redelivery": job.Redelivery})
				res.FailedLeases++
				continue
			}

			job.Status = A2AJobStatusQueued
			job.ClaimedBy = ""
			job.LeaseExpiresAt = nil
			job.Redelivery++
			job.UpdatedAt = now
			if err := s.db.Table(A2AJob{}).Update(ctx, job); err != nil {
				return res, err
			}
			s.insertA2AEvent(ctx, job.ID, "lease_requeued", map[string]any{"redelivery": job.Redelivery})
			res.Requeued++
		}
	}

	if due, err := s.GetDueA2ACallbackJobs(ctx, 1000); err == nil {
		res.CallbacksDue = len(due)
		for _, job := range due {
			if job.CallbackStatus == A2ACallbackStatusDeadLetter {
				res.CallbacksDead++
			}
		}
	}
	return res, nil
}

func (s *Service) findA2AByIdempotency(ctx context.Context, fromPubKey, key string) (*A2AJob, error) {
	results, err := s.db.Table(A2AJob{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{
			"from_public_key": fromPubKey,
			"idempotency_key": key,
		},
		OrderBy:   "created_at",
		OrderDesc: true,
		Limit:     1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	job, ok := results[0].(*A2AJob)
	if !ok {
		return nil, nil
	}
	return job, nil
}

func (s *Service) insertA2AEvent(ctx context.Context, jobID, eventType string, payload any) {
	event := &A2AJobEvent{
		ID:        uuid.NewString(),
		JobID:     jobID,
		EventType: eventType,
		CreatedAt: time.Now().UTC(),
	}
	if payload != nil {
		if blob, err := json.Marshal(payload); err == nil {
			event.EventJSON = string(blob)
		}
	}
	_ = s.db.Table(A2AJobEvent{}).Insert(ctx, event)
}

// DecodeA2ARequest unmarshals the persisted request envelope.
func DecodeA2ARequest(job *A2AJob) (A2ARequestEnvelope, error) {
	var req A2ARequestEnvelope
	if job == nil || job.RequestJSON == "" {
		return req, nil
	}
	if err := json.Unmarshal([]byte(job.RequestJSON), &req); err != nil {
		return req, err
	}
	return req, nil
}

// DecodeA2AResult unmarshals a terminal success result payload.
func DecodeA2AResult(job *A2AJob) (map[string]any, error) {
	if job == nil || job.ResultJSON == "" {
		return nil, nil
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(job.ResultJSON), &v); err != nil {
		return nil, err
	}
	return v, nil
}

// DecodeA2AError unmarshals a terminal failure payload.
func DecodeA2AError(job *A2AJob) (*A2AErrorEnvelope, error) {
	if job == nil || job.ErrorJSON == "" {
		return nil, nil
	}
	var v A2AErrorEnvelope
	if err := json.Unmarshal([]byte(job.ErrorJSON), &v); err != nil {
		return nil, err
	}
	return &v, nil
}
