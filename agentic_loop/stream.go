package gowild_agentic_loop

import (
	"context"
	"iter"
	"strings"

	"google.golang.org/genai"
)

// This file is the token-streaming half of the LLMClient seam (lifedash M17):
// the Gemini chunk assembly behind GenerateContentStreaming, and the
// single-delta fallback that keeps the interface uniform for providers with
// no native streaming path wired yet.

// SingleDeltaFallback satisfies GenerateContentStreaming for a client with no
// native streaming: one blocking GenerateContent call, then the whole text as
// one delta. Spec-compliant by construction — at least one delta when there
// is text, and the concatenation of deltas equals the final text — so a
// provider swap through NewProviderClient never breaks a streaming consumer,
// it only streams coarsely.
func SingleDeltaFallback(ctx context.Context, c LLMClient, contents []*genai.Content, config *GenerateContentConfig, sink func(string)) (*GenerateResponse, error) {
	resp, err := c.GenerateContent(ctx, contents, config)
	if err != nil {
		return nil, err
	}
	if sink != nil {
		if text := responseText(resp); text != "" {
			sink(text)
		}
	}
	return resp, nil
}

// responseText concatenates a response's text parts.
func responseText(resp *GenerateResponse) string {
	if resp == nil || resp.Content == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range resp.Content.Parts {
		if part != nil {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

// AssembleStream drains a genai streaming iterator into one GenerateResponse,
// handing each text delta to sink as it arrives, in order and without loss.
// Function-call parts, usage and the finish reason ride the chunks and are
// carried onto the assembled whole, so a caller downstream of the stream sees
// exactly the response the blocking path would have returned.
func AssembleStream(seq iter.Seq2[*genai.GenerateContentResponse, error], sink func(string)) (*GenerateResponse, error) {
	out := &GenerateResponse{}
	var text strings.Builder
	var calls []*genai.Part

	for chunk, err := range seq {
		if err != nil {
			return nil, err
		}
		if chunk == nil {
			continue
		}
		if chunk.UsageMetadata != nil {
			out.Usage = &ModelUsage{
				PromptTokens:     int(chunk.UsageMetadata.PromptTokenCount),
				CompletionTokens: int(chunk.UsageMetadata.CandidatesTokenCount),
				TotalTokens:      int(chunk.UsageMetadata.TotalTokenCount),
			}
		}
		for _, candidate := range chunk.Candidates {
			if candidate == nil {
				continue
			}
			if candidate.FinishReason != "" {
				out.FinishReason = string(candidate.FinishReason)
			}
			if candidate.GroundingMetadata != nil {
				out.GroundingMetadata = candidate.GroundingMetadata
			}
			if candidate.Content == nil {
				continue
			}
			for _, part := range candidate.Content.Parts {
				if part == nil {
					continue
				}
				if part.Text != "" {
					text.WriteString(part.Text)
					if sink != nil {
						sink(part.Text)
					}
				}
				if part.FunctionCall != nil {
					calls = append(calls, part)
					out.FunctionCalls = append(out.FunctionCalls, part.FunctionCall)
				}
			}
		}
	}

	parts := make([]*genai.Part, 0, 1+len(calls))
	if text.Len() > 0 {
		parts = append(parts, &genai.Part{Text: text.String()})
	}
	parts = append(parts, calls...)
	if len(parts) > 0 {
		out.Content = &genai.Content{Role: string(genai.RoleModel), Parts: parts}
	}
	return out, nil
}
