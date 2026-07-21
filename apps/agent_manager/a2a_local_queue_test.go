package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLocalA2AQueueClaimJobAndExtendLease(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	queue := newLocalA2AQueue(db)

	job, _, err := queue.Submit(ctx, "from-agent", "to-agent", "", localA2ARequest{
		Method: "fulfill_order",
		Params: map[string]any{"order_id": "A-123"},
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	jobID, _ := job["job_id"].(string)
	if jobID == "" {
		t.Fatalf("expected job_id")
	}

	claimed, err := queue.ClaimJob(ctx, "to-agent", jobID, 120)
	if err != nil {
		t.Fatalf("ClaimJob failed: %v", err)
	}
	if got, _ := claimed["status"].(string); got != localA2AStatusClaimed {
		t.Fatalf("expected claimed status, got %q", got)
	}
	firstLease, _ := claimed["lease_expires_at"].(string)
	if firstLease == "" {
		t.Fatalf("expected lease_expires_at after claim")
	}

	// Same claimer can call ClaimJob again and still get the job details.
	claimedAgain, err := queue.ClaimJob(ctx, "to-agent", jobID, 120)
	if err != nil {
		t.Fatalf("ClaimJob(claimed by same agent) failed: %v", err)
	}
	if got, _ := claimedAgain["status"].(string); got != localA2AStatusClaimed {
		t.Fatalf("expected claimed status on re-claim, got %q", got)
	}

	extended, err := queue.ExtendLease(ctx, "to-agent", jobID, 300)
	if err != nil {
		t.Fatalf("ExtendLease failed: %v", err)
	}
	secondLease, _ := extended["lease_expires_at"].(string)
	if secondLease == "" {
		t.Fatalf("expected lease_expires_at after extend")
	}

	firstTime, err := time.Parse(time.RFC3339, firstLease)
	if err != nil {
		t.Fatalf("failed to parse first lease: %v", err)
	}
	secondTime, err := time.Parse(time.RFC3339, secondLease)
	if err != nil {
		t.Fatalf("failed to parse second lease: %v", err)
	}
	if !secondTime.After(firstTime) {
		t.Fatalf("expected extended lease to move forward: first=%s second=%s", firstLease, secondLease)
	}
}

func TestLocalA2AQueueClaimJobRejectsBusyAgent(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	queue := newLocalA2AQueue(db)

	// Submit two pool jobs.
	job1, _, err := queue.Submit(ctx, "from-agent", "", "", localA2ARequest{
		Method: "fulfill_order",
	})
	if err != nil {
		t.Fatalf("Submit job1 failed: %v", err)
	}
	job1ID, _ := job1["job_id"].(string)

	job2, _, err := queue.Submit(ctx, "from-agent", "", "", localA2ARequest{
		Method: "fulfill_order",
	})
	if err != nil {
		t.Fatalf("Submit job2 failed: %v", err)
	}
	job2ID, _ := job2["job_id"].(string)

	// Agent claims job1 — should succeed.
	if _, err := queue.ClaimJob(ctx, "worker-agent", job1ID, 120); err != nil {
		t.Fatalf("ClaimJob for job1 failed: %v", err)
	}

	// Agent tries to claim job2 while job1 is active — should be rejected.
	if _, err := queue.ClaimJob(ctx, "worker-agent", job2ID, 120); err == nil {
		t.Fatalf("expected ClaimJob to reject busy agent")
	}
}

func TestLocalA2AQueueClaimJobClaimedByAnotherAgent(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	queue := newLocalA2AQueue(db)
	now := time.Now().UTC()
	lease := now.Add(5 * time.Minute)

	job := &localA2AJob{
		ID:             "job-claimed-other",
		FromAgentID:    "from-agent",
		ToAgentID:      "worker-agent",
		RequestJSON:    `{"method":"fulfill_order","params":{}}`,
		Status:         localA2AStatusClaimed,
		ClaimedBy:      "different-agent",
		ClaimedAt:      &now,
		LeaseExpiresAt: &lease,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := db.Table(localA2AJob{}).Insert(ctx, job); err != nil {
		t.Fatalf("insert claimed job failed: %v", err)
	}

	_, err := queue.ClaimJob(ctx, "worker-agent", job.ID, 120)
	if err == nil {
		t.Fatalf("expected claimed-by-another-agent error")
	}
	if !strings.Contains(err.Error(), "job is claimed by another agent") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocalA2AQueueClaimJobExpiredLeaseIsRequeuedAndReclaimed(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	queue := newLocalA2AQueue(db)
	now := time.Now().UTC()
	expired := now.Add(-1 * time.Minute)

	job := &localA2AJob{
		ID:             "job-lease-expired",
		FromAgentID:    "from-agent",
		ToAgentID:      "worker-agent",
		RequestJSON:    `{"method":"fulfill_order","params":{}}`,
		Status:         localA2AStatusClaimed,
		ClaimedBy:      "worker-agent",
		ClaimedAt:      &now,
		LeaseExpiresAt: &expired,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := db.Table(localA2AJob{}).Insert(ctx, job); err != nil {
		t.Fatalf("insert expired lease job failed: %v", err)
	}

	claimed, err := queue.ClaimJob(ctx, "worker-agent", job.ID, 120)
	if err != nil {
		t.Fatalf("expected expired claim to be requeued and reclaimed, got error: %v", err)
	}
	if got, _ := claimed["status"].(string); got != localA2AStatusClaimed {
		t.Fatalf("expected reclaimed status %q, got %q", localA2AStatusClaimed, got)
	}
}

func TestLocalA2AQueueClaimJobInvalidState(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	queue := newLocalA2AQueue(db)
	now := time.Now().UTC()

	job := &localA2AJob{
		ID:          "job-invalid-state",
		FromAgentID: "from-agent",
		ToAgentID:   "worker-agent",
		RequestJSON: `{"method":"fulfill_order","params":{}}`,
		Status:      "running",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Table(localA2AJob{}).Insert(ctx, job); err != nil {
		t.Fatalf("insert invalid state job failed: %v", err)
	}

	_, err := queue.ClaimJob(ctx, "worker-agent", job.ID, 120)
	if err == nil {
		t.Fatalf("expected invalid job state error")
	}
	if !strings.Contains(err.Error(), `invalid job state "running"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocalA2AClaimStatusRecognition(t *testing.T) {
	if !isLocalA2AClaimStatus(localA2AStatusQueued) {
		t.Fatalf("expected queued status to be recognized claim status")
	}
	if !isLocalA2AClaimStatus(localA2AStatusClaimed) {
		t.Fatalf("expected claimed status to be recognized claim status")
	}
	if isLocalA2AClaimStatus(localA2AStatusSucceeded) {
		t.Fatalf("expected succeeded status to be rejected for claim status")
	}
	if isLocalA2AClaimStatus("not-real") {
		t.Fatalf("expected unknown claim status to be rejected")
	}
}
