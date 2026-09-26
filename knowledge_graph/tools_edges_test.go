package gowild_knowledge_graph

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func edgeIDs(es []EdgeDTO) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.ID)
	}
	slices.Sort(out)
	return out
}

func nodeNames(ns []NodeDTO) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.Name)
	}
	slices.Sort(out)
	return out
}

func addEdgeTool(t *testing.T, tools *Tools, from, to, rel string) EdgeDTO {
	t.Helper()
	res, _ := tools.KgAddTool(context.Background(), KgAddInput{SourceNodeID: from, TargetNodeID: to, Type: rel})
	if !res.Success {
		t.Fatalf("kg_add edge %s: %s", rel, res.Error)
	}
	return res.Content.(EdgeDTO)
}

func TestConsistencyChecks(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()
	s := NewService(db, "u1")
	a, _ := s.CreateNode(ctx, "Rain", NodeTypeConcept, "", nil)
	b, _ := s.CreateNode(ctx, "Picnic", NodeTypeEvent, "", nil)
	first, err := s.CreateEdge(ctx, a.ID, b.ID, "prevents", nil, 1)
	if err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		edge Edge
		want ConsistencyResult
	}{
		"self loop": {Edge{SourceNodeID: a.ID, TargetNodeID: a.ID, RelationType: "related_to"},
			ConsistencyResult{Issue: "self_loop", Suggestion: "Edges cannot connect a node to itself. Use properties on the node instead."}},
		"duplicate": {Edge{SourceNodeID: a.ID, TargetNodeID: b.ID, RelationType: "prevents"},
			ConsistencyResult{Issue: "duplicate", ConflictID: first.ID, Suggestion: "An identical edge already exists. Use kg_update to modify it instead."}},
		"contradiction": {Edge{SourceNodeID: b.ID, TargetNodeID: a.ID, RelationType: "causes"},
			ConsistencyResult{Issue: "contradiction", ConflictID: first.ID, Suggestion: "Contradicts existing edge (relation: prevents). Delete the old edge first if the new fact supersedes it."}},
		"inverse relation, other direction": {Edge{SourceNodeID: a.ID, TargetNodeID: b.ID, RelationType: "causes"}, ConsistencyResult{OK: true}},
		"no inverse":                        {Edge{SourceNodeID: b.ID, TargetNodeID: a.ID, RelationType: "related_to"}, ConsistencyResult{OK: true}},
	} {
		got, err := s.CheckConsistency(ctx, &tc.edge)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if *got != tc.want {
			t.Errorf("%s = %+v, want %+v", name, *got, tc.want)
		}
	}

	if _, err := s.CreateEdge(ctx, b.ID, a.ID, "causes", nil, 1); err == nil || !strings.HasPrefix(err.Error(), "consistency check: contradiction (conflict_id="+first.ID+")") {
		t.Fatalf("contradicting edge = %v", err)
	}
	if _, err := s.CreateEdge(ctx, "missing", a.ID, "causes", nil, 1); err == nil || !strings.HasPrefix(err.Error(), "source node not found") {
		t.Fatalf("edge from a missing node = %v", err)
	}
	if _, err := s.CreateEdge(ctx, a.ID, "missing", "causes", nil, 1); err == nil || !strings.HasPrefix(err.Error(), "target node not found") {
		t.Fatalf("edge to a missing node = %v", err)
	}
	in, err := s.GetIncomingEdges(ctx, b.ID, "prevents")
	if err != nil || len(in) != 1 || in[0].ID != first.ID {
		t.Fatalf("incoming prevents = %+v, %v", in, err)
	}
	if in, _ = s.GetIncomingEdges(ctx, b.ID, "causes"); len(in) != 0 {
		t.Fatalf("incoming causes = %+v", in)
	}
}

func TestToolsEdgeLifecycle(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()
	tools := NewTools(db, "u1")
	alice := addNode(t, tools, "Alice", NodeTypePerson, "")
	acme := addNode(t, tools, "Acme", NodeTypeOrganization, "")

	for name, tc := range map[string]struct {
		in   KgAddInput
		want string
	}{
		"no name":         {KgAddInput{Type: NodeTypePerson}, "name is required to create a node"},
		"no type":         {KgAddInput{Name: "Bob"}, "type is required"},
		"edge no type":    {KgAddInput{SourceNodeID: alice.ID, TargetNodeID: acme.ID}, "type (relation type) is required for edges"},
		"bad valid_from":  {KgAddInput{SourceNodeID: alice.ID, TargetNodeID: acme.ID, Type: "works_at", ValidFrom: "yesterday"}, "invalid valid_from format (use RFC3339)"},
		"bad valid_until": {KgAddInput{SourceNodeID: alice.ID, TargetNodeID: acme.ID, Type: "works_at", ValidUntil: "soon"}, "invalid valid_until format (use RFC3339)"},
		"missing target":  {KgAddInput{SourceNodeID: alice.ID, TargetNodeID: "nope", Type: "works_at"}, "target node not found"},
	} {
		if res, _ := tools.KgAddTool(ctx, tc.in); res.Success || !strings.HasPrefix(res.Error, tc.want) {
			t.Errorf("%s = %+v, want error %q", name, res, tc.want)
		}
	}

	conf := 0.7
	res, _ := tools.KgAddTool(ctx, KgAddInput{
		SourceNodeID: alice.ID, TargetNodeID: acme.ID, Type: "works_at", Weight: 2,
		ValidFrom: "2026-01-01T00:00:00Z", ValidUntil: "2027-01-01T00:00:00Z", ConfidenceScore: &conf,
	})
	if !res.Success {
		t.Fatal(res.Error)
	}
	edge := res.Content.(EdgeDTO)
	if edge.Weight != 2 || edge.ValidFrom.Format(time.RFC3339) != "2026-01-01T00:00:00Z" || edge.ValidUntil.Format(time.RFC3339) != "2027-01-01T00:00:00Z" || *edge.ConfidenceScore != 0.7 || edge.ExtractedBy != "" {
		t.Fatalf("created edge = %+v", edge)
	}

	// kg_get finds edges as well as nodes.
	res, _ = tools.KgGetTool(ctx, KgGetInput{ID: edge.ID})
	if got := res.Content.(map[string]any); !res.Success || got["kind"] != "edge" || got["edge"].(EdgeDTO).RelationType != "works_at" {
		t.Fatalf("kg_get edge = %+v", res)
	}
	if res, _ = tools.KgGetTool(ctx, KgGetInput{ID: "nope"}); res.Success || res.Error != "no node or edge found with ID nope" {
		t.Fatalf("kg_get unknown = %+v", res)
	}

	// kg_update on an edge.
	weight, conf2 := 0.5, 0.9
	res, _ = tools.KgUpdateTool(ctx, KgUpdateInput{
		ID: edge.ID, Type: "employed_by", Properties: map[string]any{"role": "cto"}, Weight: &weight,
		ValidFrom: "2026-02-01T00:00:00Z", ValidUntil: "2026-12-31T00:00:00Z", ConfidenceScore: &conf2, Status: StatusExpired,
	})
	if !res.Success {
		t.Fatal(res.Error)
	}
	stored, err := tools.service.GetEdge(ctx, edge.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RelationType != "employed_by" || stored.Properties["role"] != "cto" || stored.Weight != 0.5 || *stored.ConfidenceScore != 0.9 ||
		stored.Status != StatusExpired || !stored.ValidFrom.Equal(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)) || !stored.ValidUntil.Equal(time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("updated edge = %+v", stored)
	}
	for name, tc := range map[string]struct {
		in   KgUpdateInput
		want string
	}{
		"bad valid_from":  {KgUpdateInput{ID: edge.ID, ValidFrom: "x"}, "invalid valid_from format (use RFC3339)"},
		"bad valid_until": {KgUpdateInput{ID: edge.ID, ValidUntil: "x"}, "invalid valid_until format (use RFC3339)"},
		"unknown":         {KgUpdateInput{ID: "nope", Name: "x"}, "no node or edge found with ID nope"},
	} {
		if res, _ := tools.KgUpdateTool(ctx, tc.in); res.Success || !strings.HasPrefix(res.Error, tc.want) {
			t.Errorf("kg_update %s = %+v, want error %q", name, res, tc.want)
		}
	}

	// kg_update on a node sets type, properties and status.
	res, _ = tools.KgUpdateTool(ctx, KgUpdateInput{ID: acme.ID, Type: NodeTypeEntity, Properties: map[string]any{"size": "big"}, Status: StatusInvalid})
	if !res.Success {
		t.Fatal(res.Error)
	}
	node, _ := tools.service.GetNode(ctx, acme.ID)
	if node.Type != NodeTypeEntity || node.Properties["size"] != "big" || node.Status != StatusInvalid || node.Name != "Acme" {
		t.Fatalf("updated node = %+v", node)
	}

	// kg_delete an edge, then an unknown ID.
	res, _ = tools.KgDeleteTool(ctx, KgDeleteInput{ID: edge.ID})
	if !res.Success || res.Content != "Edge "+edge.ID+" deleted" {
		t.Fatalf("kg_delete edge = %+v", res)
	}
	if _, err := tools.service.GetEdge(ctx, edge.ID); err == nil {
		t.Fatal("deleted edge still readable")
	}
	if res, _ = tools.KgDeleteTool(ctx, KgDeleteInput{ID: edge.ID}); res.Success || res.Error != "no node or edge found with ID "+edge.ID {
		t.Fatalf("kg_delete unknown = %+v", res)
	}
}

func TestToolsExploreAndFilters(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()
	tools := NewTools(db, "u1")
	// alice -knows-> bob -works_at-> acme -located_in-> paris; carol -knows-> alice
	alice := addNode(t, tools, "Alice", NodeTypePerson, "")
	bob := addNode(t, tools, "Bob", NodeTypePerson, "")
	acme := addNode(t, tools, "Acme", NodeTypeOrganization, "")
	paris := addNode(t, tools, "Paris", NodeTypeLocation, "")
	carol := addNode(t, tools, "Carol", NodeTypePerson, "")
	ab := addEdgeTool(t, tools, alice.ID, bob.ID, "knows")
	bw := addEdgeTool(t, tools, bob.ID, acme.ID, "works_at")
	ap := addEdgeTool(t, tools, acme.ID, paris.ID, "located_in")
	ca := addEdgeTool(t, tools, carol.ID, alice.ID, "knows")

	explore := func(in KgExploreInput) QueryResultDTO {
		t.Helper()
		res, _ := tools.KgExploreTool(ctx, in)
		if !res.Success {
			t.Fatalf("kg_explore %+v: %s", in, res.Error)
		}
		return res.Content.(QueryResultDTO)
	}
	no := false

	r := explore(KgExploreInput{StartNodeID: alice.ID})
	if !slices.Equal(nodeNames(r.Nodes), []string{"Bob", "Carol"}) || !slices.Equal(edgeIDs(r.Edges), edgeIDs([]EdgeDTO{ab, ca})) {
		t.Fatalf("neighbours = %v %v", nodeNames(r.Nodes), r.Edges)
	}
	if r = explore(KgExploreInput{StartNodeID: alice.ID, IncludeReverse: &no}); !slices.Equal(nodeNames(r.Nodes), []string{"Bob"}) {
		t.Fatalf("outgoing only = %v", nodeNames(r.Nodes))
	}
	if r = explore(KgExploreInput{StartNodeID: bob.ID, NodeTypes: []string{NodeTypeOrganization}}); !slices.Equal(nodeNames(r.Nodes), []string{"Acme"}) {
		t.Fatalf("node type filter = %v", nodeNames(r.Nodes))
	}
	if r = explore(KgExploreInput{StartNodeID: bob.ID, RelationTypes: []string{"knows"}}); !slices.Equal(nodeNames(r.Nodes), []string{"Alice"}) {
		t.Fatalf("relation filter = %v", nodeNames(r.Nodes))
	}

	// Traverse: depth 2 from alice reaches acme, not paris.
	if r = explore(KgExploreInput{StartNodeID: alice.ID, MaxDepth: 2, IncludeReverse: &no}); !slices.Equal(nodeNames(r.Nodes), []string{"Acme", "Alice", "Bob"}) || !slices.Equal(edgeIDs(r.Edges), edgeIDs([]EdgeDTO{ab, bw})) {
		t.Fatalf("traverse = %v, %d edges", nodeNames(r.Nodes), len(r.Edges))
	}
	if r = explore(KgExploreInput{StartNodeID: alice.ID, MaxDepth: 3, IncludeReverse: &no, NodeTypes: []string{NodeTypePerson}}); !slices.Equal(nodeNames(r.Nodes), []string{"Alice", "Bob"}) {
		t.Fatalf("traverse limited to people = %v", nodeNames(r.Nodes))
	}

	// Path: carol to paris is four hops.
	r = explore(KgExploreInput{StartNodeID: carol.ID, EndNodeID: paris.ID, IncludeReverse: &no})
	if len(r.Nodes) != 5 || r.Nodes[0].Name != "Carol" || r.Nodes[4].Name != "Paris" || len(r.Edges) != 4 || r.Edges[3].ID != ap.ID {
		t.Fatalf("path = %v", r.Nodes)
	}
	if r = explore(KgExploreInput{StartNodeID: bob.ID, EndNodeID: bob.ID}); len(r.Nodes) != 1 || r.Nodes[0].Name != "Bob" || len(r.Edges) != 0 {
		t.Fatalf("path to itself = %+v", r)
	}
	res, _ := tools.KgExploreTool(ctx, KgExploreInput{StartNodeID: paris.ID, EndNodeID: carol.ID, IncludeReverse: &no})
	if res.Success || res.Error != "no path found between "+paris.ID+" and "+carol.ID {
		t.Fatalf("path against the edges = %+v", res)
	}
	if res, _ = tools.KgExploreTool(ctx, KgExploreInput{StartNodeID: "nope", EndNodeID: "nope"}); res.Success || !strings.HasPrefix(res.Error, "failed to get node nope") {
		t.Fatalf("path from a missing node to itself = %+v", res)
	}

	// Expired edges and nodes drop out unless asked for.
	expired := StatusExpired
	if res, _ = tools.KgUpdateTool(ctx, KgUpdateInput{ID: ab.ID, Status: expired}); !res.Success {
		t.Fatal(res.Error)
	}
	if res, _ = tools.KgUpdateTool(ctx, KgUpdateInput{ID: carol.ID, Status: expired}); !res.Success {
		t.Fatal(res.Error)
	}
	if r = explore(KgExploreInput{StartNodeID: alice.ID}); len(r.Nodes) != 0 || len(r.Edges) != 0 {
		t.Fatalf("neighbours with expiries = %v, edges %v", nodeNames(r.Nodes), edgeIDs(r.Edges))
	}
	if r = explore(KgExploreInput{StartNodeID: alice.ID, IncludeExpired: true}); !slices.Equal(nodeNames(r.Nodes), []string{"Bob", "Carol"}) {
		t.Fatalf("neighbours including expired = %v", nodeNames(r.Nodes))
	}
	res, _ = tools.KgSearchTool(ctx, KgSearchInput{Mode: "list", NodeType: NodeTypePerson})
	if got := scoredNames(t, res.Content); !slices.Equal(sorted(got), []string{"Alice", "Bob"}) {
		t.Fatalf("listed people = %v", got)
	}

	// Text search with a type filter, and without a query.
	res, _ = tools.KgSearchTool(ctx, KgSearchInput{Query: "a", NodeType: NodeTypeLocation})
	if got := scoredNames(t, res.Content); !slices.Equal(got, []string{"Paris"}) {
		t.Fatalf("text search for locations = %v", got)
	}
	if res, _ = tools.KgSearchTool(ctx, KgSearchInput{}); res.Success || res.Error != "query is required for text search" {
		t.Fatalf("text search without a query = %+v", res)
	}
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}
