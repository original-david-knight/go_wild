package gowild_agentic_loop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"google.golang.org/genai"
)

// geminiRequest is one request the fake Gemini API received.
type geminiRequest struct {
	Path string
	Body map[string]any
}

// fakeGemini serves the Gemini REST API from canned JSON keyed by the
// method suffix of the path (":generateContent", ":streamGenerateContent",
// ":batchEmbedContents"), and records every request it saw.
type fakeGemini struct {
	mu       sync.Mutex
	requests []geminiRequest
	replies  map[string]string
	status   int
}

func (f *fakeGemini) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("request body is not JSON: %v", err)
			}
		}
		f.mu.Lock()
		f.requests = append(f.requests, geminiRequest{Path: r.URL.Path, Body: body})
		f.mu.Unlock()
		if f.status != 0 {
			w.WriteHeader(f.status)
			_, _ = io.WriteString(w, `{"error":{"code":400,"message":"model not found","status":"INVALID_ARGUMENT"}}`)
			return
		}
		method := r.URL.Path[strings.LastIndex(r.URL.Path, ":"):]
		reply, ok := f.replies[method]
		if !ok {
			t.Errorf("unexpected Gemini call %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if method == ":streamGenerateContent" {
			w.Header().Set("Content-Type", "text/event-stream")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		_, _ = io.WriteString(w, reply)
	}
}

func (f *fakeGemini) last(t *testing.T) geminiRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("fake Gemini received no request")
	}
	return f.requests[len(f.requests)-1]
}

// startFakeGemini points the genai SDK at a loopback server for the test.
func startFakeGemini(t *testing.T, replies map[string]string) *fakeGemini {
	t.Helper()
	fake := &fakeGemini{replies: replies}
	srv := httptest.NewServer(fake.handler(t))
	t.Cleanup(srv.Close)
	t.Setenv("GOOGLE_GEMINI_BASE_URL", srv.URL)
	t.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "false")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "env-key")
	return fake
}

func TestGeminiClientGenerateContentSendsConfigAndParsesReply(t *testing.T) {
	fake := startFakeGemini(t, map[string]string{
		":generateContent": `{
			"candidates":[{"content":{"role":"model","parts":[
				{"text":"thinking","thought":true},
				{"text":"It is sunny."},
				{"functionCall":{"name":"get_weather","args":{"city":"SF"}}}
			]},"finishReason":"STOP"}],
			"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":4,"totalTokenCount":16}
		}`,
	})

	client, err := NewGeminiClientWithHTTPClient(context.Background(), "k", "gemini-base", http.DefaultClient)
	if err != nil {
		t.Fatalf("NewGeminiClientWithHTTPClient: %v", err)
	}
	defer client.Close()

	temp := float32(0.25)
	resp, err := client.GenerateContent(context.Background(),
		[]*genai.Content{genai.NewContentFromText("weather?", genai.RoleUser)},
		&GenerateContentConfig{
			SystemInstruction: "be brief",
			Temperature:       &temp,
			MaxOutputTokens:   64,
			ThinkingBudget:    128,
			ResponseMIMEType:  "application/json",
			ResponseSchema:    &genai.Schema{Type: genai.TypeObject},
			Model:             "gemini-override",
			Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name: "get_weather", Description: "weather lookup",
			}}}},
		})
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}

	req := fake.last(t)
	if req.Path != "/v1beta/models/gemini-override:generateContent" {
		t.Errorf("path = %q, want the per-request model override", req.Path)
	}
	gen, _ := req.Body["generationConfig"].(map[string]any)
	if gen["temperature"] != 0.25 || gen["maxOutputTokens"] != float64(64) || gen["responseMimeType"] != "application/json" {
		t.Errorf("generationConfig = %#v", gen)
	}
	thinking, _ := gen["thinkingConfig"].(map[string]any)
	if thinking["thinkingBudget"] != float64(128) {
		t.Errorf("thinkingConfig = %#v, want budget 128", gen["thinkingConfig"])
	}
	sys, _ := req.Body["systemInstruction"].(map[string]any)
	sysParts, _ := sys["parts"].([]any)
	if len(sysParts) != 1 || sysParts[0].(map[string]any)["text"] != "be brief" {
		t.Errorf("systemInstruction = %#v", req.Body["systemInstruction"])
	}
	tools, _ := req.Body["tools"].([]any)
	if len(tools) != 1 {
		t.Errorf("tools = %#v, want one tool", req.Body["tools"])
	}

	if got := ExtractText(resp.Content); got != "It is sunny." {
		t.Errorf("text = %q, want the non-thought text", got)
	}
	if resp.FinishReason != "STOP" {
		t.Errorf("finish reason = %q", resp.FinishReason)
	}
	if len(resp.FunctionCalls) != 1 || resp.FunctionCalls[0].Name != "get_weather" || resp.FunctionCalls[0].Args["city"] != "SF" {
		t.Errorf("function calls = %#v", resp.FunctionCalls)
	}
	if resp.Usage == nil || *resp.Usage != (ModelUsage{PromptTokens: 12, CompletionTokens: 4, TotalTokens: 16}) {
		t.Errorf("usage = %#v", resp.Usage)
	}
}

func TestGeminiClientUsesItsModelUntilChanged(t *testing.T) {
	fake := startFakeGemini(t, map[string]string{
		":generateContent": `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]}}]}`,
	})
	client, err := NewGeminiClient(context.Background(), "", "gemini-first")
	if err != nil {
		t.Fatalf("NewGeminiClient: %v", err)
	}
	contents := []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}

	if _, err := client.GenerateContent(context.Background(), contents, nil); err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if got := fake.last(t).Path; got != "/v1beta/models/gemini-first:generateContent" {
		t.Errorf("path = %q", got)
	}

	client.SetModel("gemini-second")
	if client.GetModel() != "gemini-second" {
		t.Errorf("GetModel = %q", client.GetModel())
	}
	if _, err := client.GenerateContent(context.Background(), contents, &GenerateContentConfig{}); err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if got := fake.last(t).Path; got != "/v1beta/models/gemini-second:generateContent" {
		t.Errorf("path after SetModel = %q", got)
	}
}

func TestGeminiClientReturnsTheAPIError(t *testing.T) {
	fake := startFakeGemini(t, nil)
	fake.status = http.StatusBadRequest
	client, err := NewGeminiClient(context.Background(), "k", "gemini-x")
	if err != nil {
		t.Fatalf("NewGeminiClient: %v", err)
	}
	_, err = client.GenerateContent(context.Background(), []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}, nil)
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("err = %v, want the API's message", err)
	}
}

func TestGeminiClientStreamsEachChunk(t *testing.T) {
	fake := startFakeGemini(t, map[string]string{
		":streamGenerateContent": "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Hel\"}]}}]}\n\n" +
			"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"lo\"}]},\"finishReason\":\"STOP\"}]," +
			"\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":2,\"totalTokenCount\":5}}\n\n",
	})
	client, err := NewGeminiClient(context.Background(), "k", "gemini-stream")
	if err != nil {
		t.Fatalf("NewGeminiClient: %v", err)
	}
	temp := float32(0.5)
	var deltas []string
	resp, err := client.GenerateContentStreaming(context.Background(),
		[]*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)},
		&GenerateContentConfig{
			SystemInstruction: "sys",
			Temperature:       &temp,
			MaxOutputTokens:   10,
			ThinkingBudget:    5,
			ResponseMIMEType:  "text/plain",
			ResponseSchema:    &genai.Schema{Type: genai.TypeString},
			Tools:             []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "noop"}}}},
		},
		func(d string) { deltas = append(deltas, d) })
	if err != nil {
		t.Fatalf("GenerateContentStreaming: %v", err)
	}
	if strings.Join(deltas, "|") != "Hel|lo" {
		t.Errorf("deltas = %q, want [Hel lo]", deltas)
	}
	if got := ExtractText(resp.Content); got != "Hello" {
		t.Errorf("assembled text = %q", got)
	}
	req := fake.last(t)
	if req.Path != "/v1beta/models/gemini-stream:streamGenerateContent" {
		t.Errorf("path = %q", req.Path)
	}
	gen, _ := req.Body["generationConfig"].(map[string]any)
	if gen["maxOutputTokens"] != float64(10) || gen["responseMimeType"] != "text/plain" {
		t.Errorf("generationConfig = %#v", gen)
	}
}

func TestEmbeddingServiceEmbedsEachText(t *testing.T) {
	fake := startFakeGemini(t, map[string]string{
		":batchEmbedContents": `{"embeddings":[{"values":[0.5,-1,2]}]}`,
	})
	t.Setenv("KG_EMBEDDING_MODEL", "embed-custom")

	svc, err := NewEmbeddingService(context.Background(), "")
	if err != nil {
		t.Fatalf("NewEmbeddingService: %v", err)
	}
	defer svc.Close()

	vec, err := svc.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 3 || vec[0] != 0.5 || vec[1] != -1 || vec[2] != 2 {
		t.Errorf("Embed = %v, want [0.5 -1 2]", vec)
	}
	if got := fake.last(t).Path; got != "/v1beta/models/embed-custom:batchEmbedContents" {
		t.Errorf("path = %q, want the KG_EMBEDDING_MODEL override", got)
	}

	batch, err := svc.BatchEmbed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("BatchEmbed: %v", err)
	}
	if len(batch) != 2 || batch[1][2] != 2 {
		t.Errorf("BatchEmbed = %v", batch)
	}
	if n := len(fake.requests); n != 3 {
		t.Errorf("requests = %d, want one per text (3)", n)
	}

	none, err := svc.BatchEmbed(context.Background(), nil)
	if err != nil || none != nil {
		t.Errorf("BatchEmbed(nil) = %v, %v; want nil, nil", none, err)
	}
	if n := len(fake.requests); n != 3 {
		t.Errorf("BatchEmbed(nil) made a request")
	}
}

func TestEmbeddingServiceDefaultModelAndErrors(t *testing.T) {
	fake := startFakeGemini(t, nil)
	t.Setenv("KG_EMBEDDING_MODEL", "")
	fake.status = http.StatusBadRequest

	svc, err := NewEmbeddingService(context.Background(), "explicit-key")
	if err != nil {
		t.Fatalf("NewEmbeddingService: %v", err)
	}
	if _, err := svc.Embed(context.Background(), "x"); err == nil || !strings.HasPrefix(err.Error(), "failed to embed content:") {
		t.Errorf("Embed err = %v", err)
	}
	if got := fake.last(t).Path; got != "/v1beta/models/"+DefaultEmbeddingModel+":batchEmbedContents" {
		t.Errorf("path = %q, want the default model", got)
	}
	if _, err := svc.BatchEmbed(context.Background(), []string{"a", "b"}); err == nil || !strings.HasPrefix(err.Error(), "failed to embed text 0:") {
		t.Errorf("BatchEmbed err = %v", err)
	}
}

func TestRetrievalEmbedderSendsTaskTypeAndWidth(t *testing.T) {
	fake := startFakeGemini(t, map[string]string{
		":batchEmbedContents": `{"embeddings":[{"values":[1,2]},{"values":[3,4]}]}`,
	})
	emb, err := NewRetrievalEmbedder(context.Background(), "k", "", 2)
	if err != nil {
		t.Fatalf("NewRetrievalEmbedder: %v", err)
	}

	docs, err := emb.EmbedDocuments(context.Background(), []string{"one", "two"})
	if err != nil {
		t.Fatalf("EmbedDocuments: %v", err)
	}
	if len(docs) != 2 || docs[0][1] != 2 || docs[1][0] != 3 {
		t.Errorf("EmbedDocuments = %v", docs)
	}
	req := fake.last(t)
	if req.Path != "/v1beta/models/"+DefaultEmbeddingModel+":batchEmbedContents" {
		t.Errorf("path = %q", req.Path)
	}
	reqs, _ := req.Body["requests"].([]any)
	if len(reqs) != 2 {
		t.Fatalf("requests = %#v, want two", req.Body["requests"])
	}
	first := reqs[0].(map[string]any)
	if first["taskType"] != "RETRIEVAL_DOCUMENT" || first["outputDimensionality"] != float64(2) {
		t.Errorf("first request = %#v", first)
	}

	// A query is one text, but the fake returns two vectors: a count mismatch.
	if _, err := emb.EmbedQuery(context.Background(), "q"); err == nil || err.Error() != "embedding returned 2 vectors for 1 texts" {
		t.Errorf("EmbedQuery err = %v", err)
	}
	reqs, _ = fake.last(t).Body["requests"].([]any)
	if reqs[0].(map[string]any)["taskType"] != "RETRIEVAL_QUERY" {
		t.Errorf("query request = %#v", reqs[0])
	}
}

func TestRetrievalEmbedderQueryAndErrors(t *testing.T) {
	fake := startFakeGemini(t, map[string]string{
		":batchEmbedContents": `{"embeddings":[{"values":[7,8]}]}`,
	})
	if _, err := NewRetrievalEmbedder(context.Background(), "", "m", 2); err == nil || err.Error() != "retrieval embedder: no API key" {
		t.Errorf("no-key err = %v", err)
	}
	emb, err := NewRetrievalEmbedder(context.Background(), "k", "embed-x", 2)
	if err != nil {
		t.Fatalf("NewRetrievalEmbedder: %v", err)
	}
	vec, err := emb.EmbedQuery(context.Background(), "q")
	if err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}
	if len(vec) != 2 || vec[0] != 7 || vec[1] != 8 {
		t.Errorf("EmbedQuery = %v", vec)
	}
	if got := fake.last(t).Path; got != "/v1beta/models/embed-x:batchEmbedContents" {
		t.Errorf("path = %q", got)
	}

	fake.status = http.StatusBadRequest
	if _, err := emb.EmbedDocuments(context.Background(), []string{"a"}); err == nil || !strings.HasPrefix(err.Error(), "failed to embed content:") {
		t.Errorf("EmbedDocuments err = %v", err)
	}
}
