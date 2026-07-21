package gowild_agent_net

import "time"

const (
	// A2A protocol version returned in envelopes.
	A2AProtocolV1 = "a2a/1.0"

	// A2A job lifecycle states.
	A2AJobStatusQueued    = "queued"
	A2AJobStatusClaimed   = "claimed"
	A2AJobStatusSucceeded = "succeeded"
	A2AJobStatusFailed    = "failed"
	A2AJobStatusExpired   = "expired"
	A2AJobStatusCancelled = "cancelled"

	// Callback delivery states.
	A2ACallbackStatusNone       = "none"
	A2ACallbackStatusPending    = "pending"
	A2ACallbackStatusRetrying   = "retrying"
	A2ACallbackStatusDelivered  = "delivered"
	A2ACallbackStatusDeadLetter = "dead_lettered"
)

const (
	// A2ARequestMaxBytes caps request/result/error JSON payload size.
	A2ARequestMaxBytes = 256 * 1024

	// A2ADefaultJobTTL is the default time before an unfinished job expires.
	A2ADefaultJobTTL = 24 * time.Hour

	// A2ADefaultTimeoutSeconds applies when request.timeout_seconds is omitted.
	A2ADefaultTimeoutSeconds = 300

	// A2AMaxTimeoutSeconds protects queue capacity from unbounded tasks.
	A2AMaxTimeoutSeconds = 3600

	// A2ADefaultClaimLeaseSeconds is the default lease for claimed jobs.
	A2ADefaultClaimLeaseSeconds = 120

	// A2AMaxClaimLeaseSeconds is the maximum lease extension allowed.
	A2AMaxClaimLeaseSeconds = 600

	// A2ADefaultClaimBatch controls default jobs returned per claim call.
	A2ADefaultClaimBatch = 5

	// A2AMaxClaimBatch protects queue scans and response size.
	A2AMaxClaimBatch = 20

	// A2AMaxRedelivery caps lease-expiration retries before hard failure.
	A2AMaxRedelivery = 10

	// A2ACallbackMaxAttempts caps callback delivery retries.
	A2ACallbackMaxAttempts = 12

	// A2ACallbackRetryWindow limits retries after completion.
	A2ACallbackRetryWindow = 24 * time.Hour
)

// A2ARequestEnvelope is the persisted request body for a queued job.
type A2ARequestEnvelope struct {
	Protocol       string         `json:"protocol"`
	Method         string         `json:"method"`
	Params         map[string]any `json:"params"`
	TimeoutSeconds int            `json:"timeout_seconds,omitempty"`
}

// A2AErrorEnvelope is a structured terminal failure payload.
type A2AErrorEnvelope struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// A2AJob is a durable async A2A request.
type A2AJob struct {
	ID             string `json:"id"`
	FromPublicKey  string `json:"from_public_key"`
	ToPublicKey    string `json:"to_public_key"`
	Status         string `json:"status"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`

	RequestJSON string `json:"request_json"`
	ResultJSON  string `json:"result_json,omitempty"`
	ErrorJSON   string `json:"error_json,omitempty"`

	ClaimedBy      string     `json:"claimed_by,omitempty"`
	ClaimedAt      *time.Time `json:"claimed_at,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	Redelivery     int        `json:"redelivery"`

	CallbackURL      string     `json:"callback_url,omitempty"`
	CallbackStatus   string     `json:"callback_status"`
	CallbackAttempts int        `json:"callback_attempts"`
	NextCallbackAt   *time.Time `json:"next_callback_at,omitempty"`
	LastCallbackErr  string     `json:"last_callback_err,omitempty"`

	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	ExpiresAt   time.Time  `json:"expires_at"`
}

func (j A2AJob) GetID() string { return j.ID }

func (j A2AJob) TableName() string { return "a2a_jobs" }

// A2AJobEvent is an append-only audit trail entry.
type A2AJobEvent struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	EventType string    `json:"event_type"`
	EventJSON string    `json:"event_json,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (e A2AJobEvent) GetID() string { return e.ID }

func (e A2AJobEvent) TableName() string { return "a2a_job_events" }

// A2ASubmitInput contains validated submit data.
type A2ASubmitInput struct {
	ToPublicKey    string
	IdempotencyKey string
	Request        A2ARequestEnvelope
	CallbackURL    string
}

// A2AClaimInput controls recipient claim behavior.
type A2AClaimInput struct {
	MaxJobs      int
	LeaseSeconds int
}
