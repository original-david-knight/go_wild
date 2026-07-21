package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/agent_net"
)

type a2aSubmitRequest struct {
	ToPublicKey    string `json:"to_public_key"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	Request        struct {
		Protocol       string         `json:"protocol,omitempty"`
		Method         string         `json:"method"`
		Params         map[string]any `json:"params"`
		TimeoutSeconds int            `json:"timeout_seconds,omitempty"`
	} `json:"request"`
	Callback *struct {
		URL string `json:"url"`
	} `json:"callback,omitempty"`
}

type a2aClaimRequest struct {
	MaxJobs      int `json:"max_jobs,omitempty"`
	LeaseSeconds int `json:"lease_seconds,omitempty"`
}

type a2aCompleteRequest struct {
	Status string                             `json:"status"`
	Result map[string]any                     `json:"result,omitempty"`
	Error  *gowild_agent_net.A2AErrorEnvelope `json:"error,omitempty"`
}

type a2aLeaseRequest struct {
	LeaseSeconds int `json:"lease_seconds,omitempty"`
}

type a2aJobResponse struct {
	JobID          string                              `json:"job_id"`
	Status         string                              `json:"status"`
	FromPublicKey  string                              `json:"from_public_key"`
	ToPublicKey    string                              `json:"to_public_key"`
	Request        gowild_agent_net.A2ARequestEnvelope `json:"request,omitempty"`
	Result         map[string]any                      `json:"result,omitempty"`
	Error          *gowild_agent_net.A2AErrorEnvelope  `json:"error,omitempty"`
	ClaimedAt      *string                             `json:"claimed_at,omitempty"`
	ClaimedBy      string                              `json:"claimed_by,omitempty"`
	LeaseExpiresAt *string                             `json:"lease_expires_at,omitempty"`
	CreatedAt      string                              `json:"created_at"`
	CompletedAt    *string                             `json:"completed_at,omitempty"`
	ExpiresAt      string                              `json:"expires_at"`
}

func (h *Handlers) HandleA2ASubmitJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeBadRequest(w, "Method not allowed")
		return
	}

	agentID := GetAgentID(r.Context())
	body := GetCachedBody(r.Context())
	if body == nil {
		writeBadRequest(w, "Request body is required")
		return
	}

	var req a2aSubmitRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeBadRequest(w, "Invalid JSON: "+err.Error())
		return
	}

	input := gowild_agent_net.A2ASubmitInput{
		ToPublicKey:    req.ToPublicKey,
		IdempotencyKey: req.IdempotencyKey,
		Request: gowild_agent_net.A2ARequestEnvelope{
			Protocol:       req.Request.Protocol,
			Method:         req.Request.Method,
			Params:         req.Request.Params,
			TimeoutSeconds: req.Request.TimeoutSeconds,
		},
	}
	if req.Callback != nil {
		input.CallbackURL = req.Callback.URL
	}

	job, replay, err := h.service.SubmitA2AJob(r.Context(), agentID, input)
	if err != nil {
		errMsg := err.Error()
		switch {
		case errors.Is(err, gowild_agent_net.ErrA2ARecipientNotPremium):
			writeMessageError(w, http.StatusBadRequest, ErrCodeRecipientNotPremium, "Recipient is not a premium agent")
		case strings.Contains(errMsg, "too large"):
			writeMessageError(w, http.StatusBadRequest, ErrCodeA2APayloadTooBig, errMsg)
		default:
			writeBadRequest(w, errMsg)
		}
		return
	}

	statusCode := http.StatusCreated
	if replay {
		statusCode = http.StatusOK
	}
	writeJSON(w, statusCode, map[string]any{
		"job_id":            job.ID,
		"status":            job.Status,
		"created_at":        job.CreatedAt.Format(time.RFC3339),
		"expires_at":        job.ExpiresAt.Format(time.RFC3339),
		"idempotent_replay": replay,
	})
}

func (h *Handlers) HandleA2AGetJob(w http.ResponseWriter, r *http.Request, jobID string) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "Method not allowed")
		return
	}
	agentID := GetAgentID(r.Context())
	job, err := h.service.GetA2AJobForAgent(r.Context(), agentID, jobID)
	if err != nil {
		switch {
		case errors.Is(err, gowild_agent_net.ErrA2AJobNotFound):
			writeMessageError(w, http.StatusNotFound, ErrCodeA2AJobNotFound, "A2A job not found")
		case errors.Is(err, gowild_agent_net.ErrA2AForbidden):
			writeMessageError(w, http.StatusForbidden, ErrCodeA2AForbidden, "Not authorized for this A2A job")
		default:
			writeInternalError(w, "Failed to load A2A job: "+err.Error())
		}
		return
	}

	resp, err := a2aJobToResponse(job)
	if err != nil {
		writeInternalError(w, "Failed to parse A2A job payloads")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) HandleA2AClaimJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeBadRequest(w, "Method not allowed")
		return
	}
	agentID := GetAgentID(r.Context())

	var req a2aClaimRequest
	body := GetCachedBody(r.Context())
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeBadRequest(w, "Invalid JSON: "+err.Error())
			return
		}
	}

	jobs, err := h.service.ClaimA2AJobs(r.Context(), agentID, gowild_agent_net.A2AClaimInput{
		MaxJobs:      req.MaxJobs,
		LeaseSeconds: req.LeaseSeconds,
	})
	if err != nil {
		writeInternalError(w, "Failed to claim A2A jobs: "+err.Error())
		return
	}

	resp := make([]a2aJobResponse, 0, len(jobs))
	for _, job := range jobs {
		entry, err := a2aJobToResponse(job)
		if err != nil {
			continue
		}
		resp = append(resp, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": resp})
}

func (h *Handlers) HandleA2ACompleteJob(w http.ResponseWriter, r *http.Request, jobID string) {
	if r.Method != http.MethodPost {
		writeBadRequest(w, "Method not allowed")
		return
	}
	agentID := GetAgentID(r.Context())
	body := GetCachedBody(r.Context())
	if body == nil {
		writeBadRequest(w, "Request body is required")
		return
	}

	var req a2aCompleteRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeBadRequest(w, "Invalid JSON: "+err.Error())
		return
	}

	job, err := h.service.CompleteA2AJob(r.Context(), agentID, jobID, req.Status, req.Result, req.Error)
	if err != nil {
		errMsg := err.Error()
		switch {
		case errors.Is(err, gowild_agent_net.ErrA2AJobNotFound):
			writeMessageError(w, http.StatusNotFound, ErrCodeA2AJobNotFound, "A2A job not found")
		case errors.Is(err, gowild_agent_net.ErrA2AForbidden):
			writeMessageError(w, http.StatusForbidden, ErrCodeA2AForbidden, "Not authorized for this A2A job")
		case errors.Is(err, gowild_agent_net.ErrA2ALeaseExpired):
			writeMessageError(w, http.StatusConflict, ErrCodeA2ALeaseExpired, "A2A job lease has expired")
		case errors.Is(err, gowild_agent_net.ErrA2AInvalidState):
			writeMessageError(w, http.StatusConflict, ErrCodeA2AInvalidState, "A2A job is not claimable in its current state")
		case strings.Contains(errMsg, "too large"):
			writeMessageError(w, http.StatusBadRequest, ErrCodeA2APayloadTooBig, errMsg)
		default:
			writeBadRequest(w, errMsg)
		}
		return
	}

	resp, err := a2aJobToResponse(job)
	if err != nil {
		writeInternalError(w, "Failed to parse completed A2A job")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) HandleA2AExtendLease(w http.ResponseWriter, r *http.Request, jobID string) {
	if r.Method != http.MethodPost {
		writeBadRequest(w, "Method not allowed")
		return
	}
	agentID := GetAgentID(r.Context())

	var req a2aLeaseRequest
	body := GetCachedBody(r.Context())
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeBadRequest(w, "Invalid JSON: "+err.Error())
			return
		}
	}

	job, err := h.service.ExtendA2ALease(r.Context(), agentID, jobID, req.LeaseSeconds)
	if err != nil {
		switch {
		case errors.Is(err, gowild_agent_net.ErrA2AJobNotFound):
			writeMessageError(w, http.StatusNotFound, ErrCodeA2AJobNotFound, "A2A job not found")
		case errors.Is(err, gowild_agent_net.ErrA2AForbidden):
			writeMessageError(w, http.StatusForbidden, ErrCodeA2AForbidden, "Not authorized for this A2A job")
		case errors.Is(err, gowild_agent_net.ErrA2ALeaseExpired):
			writeMessageError(w, http.StatusConflict, ErrCodeA2ALeaseExpired, "A2A job lease has expired")
		case errors.Is(err, gowild_agent_net.ErrA2AInvalidState):
			writeMessageError(w, http.StatusConflict, ErrCodeA2AInvalidState, "A2A job is not claimable in its current state")
		default:
			writeBadRequest(w, err.Error())
		}
		return
	}

	resp, err := a2aJobToResponse(job)
	if err != nil {
		writeInternalError(w, "Failed to parse A2A job")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func a2aJobToResponse(job *gowild_agent_net.A2AJob) (a2aJobResponse, error) {
	req, err := gowild_agent_net.DecodeA2ARequest(job)
	if err != nil {
		return a2aJobResponse{}, err
	}
	res, err := gowild_agent_net.DecodeA2AResult(job)
	if err != nil {
		return a2aJobResponse{}, err
	}
	errPayload, err := gowild_agent_net.DecodeA2AError(job)
	if err != nil {
		return a2aJobResponse{}, err
	}

	out := a2aJobResponse{
		JobID:         job.ID,
		Status:        job.Status,
		FromPublicKey: job.FromPublicKey,
		ToPublicKey:   job.ToPublicKey,
		Request:       req,
		Result:        res,
		Error:         errPayload,
		ClaimedBy:     job.ClaimedBy,
		CreatedAt:     job.CreatedAt.Format(time.RFC3339),
		ExpiresAt:     job.ExpiresAt.Format(time.RFC3339),
	}
	if job.ClaimedAt != nil {
		v := job.ClaimedAt.Format(time.RFC3339)
		out.ClaimedAt = &v
	}
	if job.LeaseExpiresAt != nil {
		v := job.LeaseExpiresAt.Format(time.RFC3339)
		out.LeaseExpiresAt = &v
	}
	if job.CompletedAt != nil {
		v := job.CompletedAt.Format(time.RFC3339)
		out.CompletedAt = &v
	}
	return out, nil
}
