package gowild_agentic_loop

import (
	"context"

	"google.golang.org/genai"
)

// NewGatedClient wraps inner so that every generation call must first pass
// gate. Enforcement sits on the client seam itself, so no caller — loop-driven
// or one-shot — can generate without passing the gate. A gate error is
// returned as-is with no call made, so consumers can match sentinel errors
// with errors.Is. SetModel, GetModel and Close forward to the inner client
// without consulting the gate.
func NewGatedClient(inner LLMClient, gate func(context.Context) error) LLMClient {
	return &gatedClient{inner: inner, gate: gate}
}

// gatedClient runs the gate before each generation and forwards everything
// else to the inner client untouched.
type gatedClient struct {
	inner LLMClient
	gate  func(context.Context) error
}

// GenerateContent runs the gate first; on refusal the inner client is never
// reached and the gate's error comes back unchanged.
func (g *gatedClient) GenerateContent(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig) (*GenerateResponse, error) {
	if err := g.gate(ctx); err != nil {
		return nil, err
	}
	return g.inner.GenerateContent(ctx, contents, config)
}

// GenerateContentStreaming passes the gate exactly as the blocking call does,
// then streams through the inner client.
func (g *gatedClient) GenerateContentStreaming(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig, sink func(string)) (*GenerateResponse, error) {
	if err := g.gate(ctx); err != nil {
		return nil, err
	}
	return g.inner.GenerateContentStreaming(ctx, contents, config, sink)
}

func (g *gatedClient) SetModel(model string) { g.inner.SetModel(model) }
func (g *gatedClient) GetModel() string      { return g.inner.GetModel() }
func (g *gatedClient) Close() error          { return g.inner.Close() }
