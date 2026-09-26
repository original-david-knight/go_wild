package gowild_knowledge_graph

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/original-david-knight/go_wild/data"
)

// fakeGemini serves the Gemini batchEmbedContents endpoint on loopback. A
// text's vector counts the topic words it contains, so texts about the same
// topic are close.
type fakeGemini struct {
	mu     sync.Mutex
	dims   int      // vector width; the "model" in use
	failOn string   // a text containing this fails; "*" fails every call
	paths  []string // request paths seen
	texts  []string // texts embedded, in order
}

var topics = []string{"chess", "tennis", "cook"}

func (f *fakeGemini) vector(text string) []float32 {
	v := make([]float32, f.dims)
	lower := strings.ToLower(text)
	for i := range v {
		v[i] = 0.01
		if i < len(topics) {
			v[i] += float32(strings.Count(lower, topics[i]))
		}
	}
	return v
}

func (f *fakeGemini) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Requests []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"requests"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.URL.Path)
	type embedding struct {
		Values []float32 `json:"values"`
	}
	var out []embedding
	for _, rq := range req.Requests {
		var text string
		for _, p := range rq.Content.Parts {
			text += p.Text
		}
		f.texts = append(f.texts, text)
		if f.failOn == "*" || (f.failOn != "" && strings.Contains(text, f.failOn)) {
			http.Error(w, `{"error":{"code":500,"message":"embedder down","status":"INTERNAL"}}`, http.StatusInternalServerError)
			return
		}
		out = append(out, embedding{f.vector(text)})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"embeddings": out})
}

func (f *fakeGemini) set(dims int, failOn string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dims, f.failOn = dims, failOn
}

func (f *fakeGemini) seen() (paths, texts []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.paths), slices.Clone(f.texts)
}

// newFakeEmbedder points a real EmbeddingService at a fakeGemini.
func newFakeEmbedder(t *testing.T) (*EmbeddingService, *fakeGemini) {
	t.Helper()
	fake := &fakeGemini{dims: 3}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	t.Setenv("GOOGLE_GEMINI_BASE_URL", srv.URL)
	t.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "false")
	t.Setenv("KG_EMBEDDING_MODEL", "test-embed")
	es, err := NewEmbeddingService(context.Background(), "test-key")
	if err != nil {
		t.Fatal(err)
	}
	return es, fake
}

func scoredNames(t *testing.T, content any) []string {
	t.Helper()
	var out []string
	for _, s := range content.([]ScoredNodeDTO) {
		out = append(out, s.Node.Name)
	}
	return out
}

func addNode(t *testing.T, tools *Tools, name, typ, notes string) NodeDTO {
	t.Helper()
	res, _ := tools.KgAddTool(context.Background(), KgAddInput{Name: name, Type: typ, Notes: notes})
	if !res.Success {
		t.Fatalf("kg_add %s: %s", name, res.Error)
	}
	return res.Content.(map[string]any)["node"].(NodeDTO)
}

func TestSemanticSearchTools(t *testing.T) {
	es, fake := newFakeEmbedder(t)
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()
	tools := NewTools(db, "u1")
	tools.SetEmbeddingService(es)

	chess := addNode(t, tools, "Chess club", NodeTypeOrganization, "Plays chess on Tuesdays")
	addNode(t, tools, "Tennis court", NodeTypeLocation, "")
	addNode(t, tools, "Cooking class", NodeTypeEvent, "")

	// Each node is embedded from its name, type and notes at creation.
	paths, texts := fake.seen()
	if texts[0] != "Chess club (organization): Plays chess on Tuesdays" {
		t.Fatalf("embedded text = %q", texts[0])
	}
	if paths[0] != "/v1beta/models/test-embed:batchEmbedContents" {
		t.Fatalf("request path = %q", paths[0])
	}
	stored, _ := tools.service.GetNode(ctx, chess.ID)
	if !slices.Equal(stored.Embedding, fake.vector("chess chess")) {
		t.Fatalf("stored embedding = %v", stored.Embedding)
	}

	addNode(t, tools, "Chess cafe", NodeTypeLocation, "")

	res, _ := tools.KgSearchTool(ctx, KgSearchInput{Mode: "semantic", Query: "tennis", Limit: 1})
	if !res.Success {
		t.Fatal(res.Error)
	}
	if got := scoredNames(t, res.Content); !slices.Equal(got, []string{"Tennis court"}) {
		t.Fatalf("semantic ranking = %v", got)
	}
	if top := res.Content.([]ScoredNodeDTO)[0].Score; math.Abs(float64(top)-1) > 0.001 {
		t.Fatalf("top score = %v", top)
	}

	// Similar excludes the node itself.
	res, _ = tools.KgSearchTool(ctx, KgSearchInput{Mode: "similar", NodeID: chess.ID, Limit: 1})
	if !res.Success {
		t.Fatal(res.Error)
	}
	if got := scoredNames(t, res.Content); !slices.Equal(got, []string{"Chess cafe"}) {
		t.Fatalf("similar = %v", got)
	}

	// Editing a node re-embeds it.
	notes := "Now a cooking school"
	if res, _ = tools.KgUpdateTool(ctx, KgUpdateInput{ID: chess.ID, Notes: &notes}); !res.Success {
		t.Fatal(res.Error)
	}
	stored, _ = tools.service.GetNode(ctx, chess.ID)
	if !slices.Equal(stored.Embedding, fake.vector("chess cook")) {
		t.Fatalf("embedding after edit = %v", stored.Embedding)
	}

	for name, in := range map[string]KgSearchInput{
		"semantic without query": {Mode: "semantic"},
		"similar without node":   {Mode: "similar"},
		"similar to a missing":   {Mode: "similar", NodeID: "nope"},
	} {
		if res, _ := tools.KgSearchTool(ctx, in); res.Success {
			t.Errorf("%s succeeded: %+v", name, res.Content)
		}
	}
}

func TestSemanticSearchWithoutEmbedder(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()
	tools := NewTools(db, "u1")
	n := addNode(t, tools, "Chess club", NodeTypeOrganization, "")
	for _, in := range []KgSearchInput{{Mode: "semantic", Query: "chess"}, {Mode: "similar", NodeID: n.ID}} {
		res, _ := tools.KgSearchTool(ctx, in)
		if res.Success || res.Error != "embedding service not configured" {
			t.Errorf("%s = %+v", in.Mode, res)
		}
	}
}

func TestEmbedderFailures(t *testing.T) {
	es, fake := newFakeEmbedder(t)
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()
	s := NewService(db, "u1")
	s.SetEmbeddingService(es)

	withVec, err := s.CreateNode(ctx, "Chess club", NodeTypeOrganization, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	cafe, _ := s.CreateNode(ctx, "Chess cafe", NodeTypeLocation, "", nil)
	fake.set(3, "*")
	// A node is still created when embedding fails, just without a vector.
	bare, err := s.CreateNode(ctx, "Tennis court", NodeTypeLocation, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetNode(ctx, bare.ID); len(got.Embedding) != 0 {
		t.Fatalf("embedding stored after a failure: %v", got.Embedding)
	}
	bare.Notes = "edited"
	if err := s.UpdateNode(ctx, bare); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetNode(ctx, bare.ID); got.Notes != "edited" || len(got.Embedding) != 0 {
		t.Fatalf("update with a failing embedder = %+v", got)
	}

	if _, err := s.semanticSearch(ctx, "chess", 5); err == nil || !strings.HasPrefix(err.Error(), "failed to embed query") {
		t.Fatalf("semantic search with a failing embedder = %v", err)
	}
	// A node with a stored vector falls back to it; one without fails.
	res, err := s.findSimilarNodes(ctx, withVec.ID, 5)
	if err != nil || len(res) != 1 || res[0].Node.ID != cafe.ID {
		t.Fatalf("similar from a stored vector = %+v, %v", res, err)
	}
	if _, err := s.findSimilarNodes(ctx, bare.ID, 5); err == nil || !strings.HasPrefix(err.Error(), "failed to embed node") {
		t.Fatalf("similar without any vector = %v", err)
	}
}

func TestStaleEmbeddingsAreRegenerated(t *testing.T) {
	es, fake := newFakeEmbedder(t)
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()
	s := NewService(db, "u1")
	s.SetEmbeddingService(es)

	// Vectors from an older, narrower model match nothing the new one makes.
	fake.set(2, "")
	chess, _ := s.CreateNode(ctx, "Chess club", NodeTypeOrganization, "", nil)
	tennis, _ := s.CreateNode(ctx, "Tennis court", NodeTypeLocation, "", nil)
	fake.set(3, "")

	res, err := s.semanticSearch(ctx, "chess", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].Node.ID != chess.ID {
		t.Fatalf("results after regeneration = %+v", res)
	}
	for _, id := range []string{chess.ID, tennis.ID} {
		if n, _ := s.GetNode(ctx, id); len(n.Embedding) != 3 {
			t.Fatalf("node %s still has a %d-wide vector", n.Name, len(n.Embedding))
		}
	}

	// The same recovery from a similar-nodes lookup, which also refreshes
	// the source node's own vector.
	fake.set(2, "")
	if _, err := s.regenerateEmbeddings(ctx); err != nil {
		t.Fatal(err)
	}
	fake.set(3, "")
	sim, err := s.findSimilarNodes(ctx, chess.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(sim) != 1 || sim[0].Node.ID != tennis.ID {
		t.Fatalf("similar after regeneration = %+v", sim)
	}
	if n, _ := s.GetNode(ctx, chess.ID); len(n.Embedding) != 3 {
		t.Fatalf("source vector not refreshed: %v", n.Embedding)
	}

	// A regeneration that fails part-way leaves the search empty, not failed.
	fake.set(2, "")
	s.regenerateEmbeddings(ctx)
	fake.set(3, "Tennis")
	if res, err = s.semanticSearch(ctx, "chess", 5); err != nil || len(res) != 0 {
		t.Fatalf("search after a failed regeneration = %+v, %v", res, err)
	}
	if _, err := s.regenerateEmbeddings(ctx); err == nil || !strings.Contains(err.Error(), "failed to embed node "+tennis.ID) {
		t.Fatalf("partial regeneration = %v", err)
	}
}

func TestRegisteredTables(t *testing.T) {
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := gowild_data.AddAllTables(db); err != nil {
		t.Fatal(err)
	}
	s := NewService(db, "u1")
	ctx := context.Background()
	a, err := s.CreateNode(ctx, "A", NodeTypeConcept, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.CreateNode(ctx, "B", NodeTypeConcept, "", nil)
	if _, err := s.CreateEdge(ctx, a.ID, b.ID, RelationTypeRelatedTo, nil, 1); err != nil {
		t.Fatal(err)
	}
	out, err := s.GetOutgoingEdges(ctx, a.ID, "")
	if err != nil || len(out) != 1 || out[0].TargetNodeID != b.ID {
		t.Fatalf("edges on registered tables = %+v, %v", out, err)
	}
}
