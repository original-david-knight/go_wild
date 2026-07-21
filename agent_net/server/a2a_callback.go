package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/agent_net"
)

// A2ACallbackSigner signs callback payloads delivered by this server.
type A2ACallbackSigner struct {
	privateKey ed25519.PrivateKey
	keyID      string
}

func NewA2ACallbackSigner(raw string) *A2ACallbackSigner {
	priv := parseSigningKey(strings.TrimSpace(raw))
	if len(priv) != ed25519.PrivateKeySize {
		_, generated, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil
		}
		priv = generated
	}
	pub := priv.Public().(ed25519.PublicKey)
	return &A2ACallbackSigner{
		privateKey: priv,
		keyID:      base64.RawURLEncoding.EncodeToString(pub),
	}
}

func parseSigningKey(raw string) ed25519.PrivateKey {
	if raw == "" {
		return nil
	}
	candidates := [][]byte{}
	if b, err := base64.RawURLEncoding.DecodeString(raw); err == nil {
		candidates = append(candidates, b)
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil {
		candidates = append(candidates, b)
	}
	if b, err := hex.DecodeString(raw); err == nil {
		candidates = append(candidates, b)
	}
	for _, b := range candidates {
		switch len(b) {
		case ed25519.SeedSize:
			return ed25519.NewKeyFromSeed(b)
		case ed25519.PrivateKeySize:
			return ed25519.PrivateKey(b)
		}
	}
	return nil
}

func (s *A2ACallbackSigner) KeyID() string {
	if s == nil {
		return ""
	}
	return s.keyID
}

func (s *A2ACallbackSigner) Sign(method, path, timestamp string, body []byte) string {
	if s == nil || len(s.privateKey) != ed25519.PrivateKeySize {
		return ""
	}
	sig := gowild_agent_net.SignRequest(s.privateKey, method, path, timestamp, body)
	return gowild_agent_net.EncodeSignature(sig)
}

func (s *Server) processA2ACallbacks(ctx context.Context, max int) {
	if s.a2aSigner == nil || s.service == nil {
		return
	}

	jobs, err := s.service.GetDueA2ACallbackJobs(ctx, max)
	if err != nil {
		log.Printf("a2a callback: failed to load due jobs: %v", err)
		return
	}
	if len(jobs) == 0 {
		return
	}

	for _, job := range jobs {
		payload, err := buildA2ACallbackPayload(job)
		if err != nil {
			_, _ = s.service.RecordA2ACallbackOutcome(ctx, job.ID, false, "failed to serialize callback payload")
			continue
		}

		err = deliverA2ACallback(ctx, s.a2aSigner, job.CallbackURL, job.ID, payload)
		errMsg := ""
		delivered := err == nil
		if err != nil {
			errMsg = err.Error()
		}
		updated, recErr := s.service.RecordA2ACallbackOutcome(ctx, job.ID, delivered, errMsg)
		if recErr != nil {
			log.Printf("a2a callback: failed to record callback outcome for %s: %v", job.ID, recErr)
			continue
		}
		if delivered {
			log.Printf("a2a callback: delivered job %s after %d attempt(s)", job.ID, updated.CallbackAttempts)
		} else if updated.CallbackStatus == gowild_agent_net.A2ACallbackStatusDeadLetter {
			log.Printf("a2a callback: dead-lettered job %s after %d attempt(s): %s", job.ID, updated.CallbackAttempts, errMsg)
		}
	}
}

func buildA2ACallbackPayload(job *gowild_agent_net.A2AJob) ([]byte, error) {
	result, err := gowild_agent_net.DecodeA2AResult(job)
	if err != nil {
		return nil, err
	}
	errPayload, err := gowild_agent_net.DecodeA2AError(job)
	if err != nil {
		return nil, err
	}

	resp := map[string]any{
		"job_id":          job.ID,
		"status":          job.Status,
		"from_public_key": job.FromPublicKey,
		"to_public_key":   job.ToPublicKey,
		"result":          result,
		"error":           errPayload,
	}
	if job.CompletedAt != nil {
		resp["completed_at"] = job.CompletedAt.Format(time.RFC3339)
	}
	return json.Marshal(resp)
}

func deliverA2ACallback(ctx context.Context, signer *A2ACallbackSigner, callbackURL, jobID string, payload []byte) error {
	u, err := url.Parse(callbackURL)
	if err != nil {
		return fmt.Errorf("invalid callback url: %w", err)
	}

	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	signature := signer.Sign(http.MethodPost, path, timestamp, payload)
	if signature == "" {
		return fmt.Errorf("callback signer unavailable")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, callbackURL, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("failed to build callback request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-A2A-Job-ID", jobID)
	req.Header.Set("X-A2A-Timestamp", timestamp)
	req.Header.Set("X-A2A-Sig", signature)
	req.Header.Set("X-A2A-Key-ID", signer.KeyID())

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if len(body) > 0 {
		return fmt.Errorf("callback status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return fmt.Errorf("callback status %d", resp.StatusCode)
}
