package gowild_agentic_loop

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/genai"
)

// countingClient is a fake LLMClient that counts every call it receives, so
// the tests can tell whether the gate actually kept a generation out.
type countingClient struct {
	generate  int
	streaming int
	setModel  int
	getModel  int
	closed    int
	model     string
}

func (c *countingClient) GenerateContent(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig) (*GenerateResponse, error) {
	c.generate++
	return &GenerateResponse{}, nil
}

func (c *countingClient) GenerateContentStreaming(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig, sink func(string)) (*GenerateResponse, error) {
	c.streaming++
	return &GenerateResponse{}, nil
}

func (c *countingClient) SetModel(model string) { c.setModel++; c.model = model }
func (c *countingClient) GetModel() string      { c.getModel++; return c.model }
func (c *countingClient) Close() error          { c.closed++; return nil }

// errGateRefused is the sentinel the tests' gate returns once it stops
// allowing calls; the decorator must surface it unwrapped enough for
// errors.Is to match.
var errGateRefused = errors.New("gate refused")

// switchableGate allows until closed, then refuses with the sentinel.
type switchableGate struct {
	calls  int
	closed bool
}

func (s *switchableGate) gate(ctx context.Context) error {
	s.calls++
	if s.closed {
		return errGateRefused
	}
	return nil
}

// TestGatedClientBlocking checks the blocking path: an open gate lets the
// call through to the inner client, and a closed gate returns the sentinel
// without the inner client ever being reached.
func TestGatedClientBlocking(t *testing.T) {
	inner := &countingClient{}
	sw := &switchableGate{}
	client := NewGatedClient(inner, sw.gate)

	if _, err := client.GenerateContent(context.Background(), nil, nil); err != nil {
		t.Fatalf("open gate: unexpected error %v", err)
	}
	if inner.generate != 1 {
		t.Fatalf("open gate: inner GenerateContent called %d times, want 1", inner.generate)
	}

	sw.closed = true
	_, err := client.GenerateContent(context.Background(), nil, nil)
	if !errors.Is(err, errGateRefused) {
		t.Fatalf("closed gate: got %v, want errors.Is match on the sentinel", err)
	}
	if inner.generate != 1 {
		t.Fatalf("closed gate: inner GenerateContent called %d times, want 1", inner.generate)
	}
	if sw.calls != 2 {
		t.Fatalf("gate consulted %d times, want 2", sw.calls)
	}
}

// TestGatedClientStreaming checks the streaming path enforces the gate
// exactly as the blocking path does.
func TestGatedClientStreaming(t *testing.T) {
	inner := &countingClient{}
	sw := &switchableGate{}
	client := NewGatedClient(inner, sw.gate)
	sink := func(string) {}

	if _, err := client.GenerateContentStreaming(context.Background(), nil, nil, sink); err != nil {
		t.Fatalf("open gate: unexpected error %v", err)
	}
	if inner.streaming != 1 {
		t.Fatalf("open gate: inner GenerateContentStreaming called %d times, want 1", inner.streaming)
	}

	sw.closed = true
	_, err := client.GenerateContentStreaming(context.Background(), nil, nil, sink)
	if !errors.Is(err, errGateRefused) {
		t.Fatalf("closed gate: got %v, want errors.Is match on the sentinel", err)
	}
	if inner.streaming != 1 {
		t.Fatalf("closed gate: inner GenerateContentStreaming called %d times, want 1", inner.streaming)
	}
}

// TestGatedClientWrappedSentinel checks that a gate error wrapped by the
// gate itself still matches through errors.Is, because the decorator returns
// it as-is.
func TestGatedClientWrappedSentinel(t *testing.T) {
	inner := &countingClient{}
	gate := func(context.Context) error {
		return fmt.Errorf("budget for job: %w", errGateRefused)
	}
	client := NewGatedClient(inner, gate)

	_, err := client.GenerateContent(context.Background(), nil, nil)
	if !errors.Is(err, errGateRefused) {
		t.Fatalf("wrapped sentinel: got %v, want errors.Is match", err)
	}
	if inner.generate != 0 {
		t.Fatalf("wrapped sentinel: inner reached %d times, want 0", inner.generate)
	}
}

// TestGatedClientForwarding checks that SetModel, GetModel and Close pass
// straight through to the inner client without invoking the gate.
func TestGatedClientForwarding(t *testing.T) {
	inner := &countingClient{}
	sw := &switchableGate{closed: true}
	client := NewGatedClient(inner, sw.gate)

	client.SetModel("some-model")
	if inner.setModel != 1 || inner.model != "some-model" {
		t.Fatalf("SetModel did not forward: calls=%d model=%q", inner.setModel, inner.model)
	}
	if got := client.GetModel(); got != "some-model" {
		t.Fatalf("GetModel returned %q, want %q", got, "some-model")
	}
	if inner.getModel != 1 {
		t.Fatalf("GetModel forwarded %d times, want 1", inner.getModel)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close returned %v, want nil", err)
	}
	if inner.closed != 1 {
		t.Fatalf("Close forwarded %d times, want 1", inner.closed)
	}
	if sw.calls != 0 {
		t.Fatalf("gate invoked %d times by pass-through methods, want 0", sw.calls)
	}
}
