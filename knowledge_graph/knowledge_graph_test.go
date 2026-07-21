package gowild_knowledge_graph

import (
	"context"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/data"
)

func setupTestDB(t *testing.T) gowild_data.Database {
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	if err := db.AddTable(Node{}); err != nil {
		t.Fatalf("failed to add Node table: %v", err)
	}
	if err := db.AddTable(Edge{}); err != nil {
		t.Fatalf("failed to add Edge table: %v", err)
	}
	return db
}

func TestCreateAndGetNode(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	service := NewService(db, "test-user")

	// Create a node
	node, err := service.CreateNode(ctx, "Test Node", NodeTypeConcept, "", map[string]any{
		"description": "A test concept",
		"importance":  5,
	})
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}
	if node.ID == "" {
		t.Error("expected node to have an ID")
	}
	if node.Name != "Test Node" {
		t.Errorf("expected name 'Test Node', got '%s'", node.Name)
	}
	if node.Type != NodeTypeConcept {
		t.Errorf("expected type '%s', got '%s'", NodeTypeConcept, node.Type)
	}

	// Get the node
	retrieved, err := service.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("failed to get node: %v", err)
	}
	if retrieved.Name != node.Name {
		t.Errorf("expected name '%s', got '%s'", node.Name, retrieved.Name)
	}
	if retrieved.Properties["importance"] != float64(5) {
		t.Errorf("expected importance 5, got %v", retrieved.Properties["importance"])
	}
}

func TestUpdateNode(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	service := NewService(db, "test-user")

	node, _ := service.CreateNode(ctx, "Original Name", NodeTypeEntity, "", nil)

	node.Name = "Updated Name"
	node.Properties = map[string]any{"updated": true}
	if err := service.UpdateNode(ctx, node); err != nil {
		t.Fatalf("failed to update node: %v", err)
	}

	retrieved, _ := service.GetNode(ctx, node.ID)
	if retrieved.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got '%s'", retrieved.Name)
	}
	if retrieved.Properties["updated"] != true {
		t.Error("expected properties to be updated")
	}
}

func TestDeleteNode(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	service := NewService(db, "test-user")

	node1, _ := service.CreateNode(ctx, "Node 1", NodeTypeEntity, "", nil)
	node2, _ := service.CreateNode(ctx, "Node 2", NodeTypeEntity, "", nil)
	_, _ = service.CreateEdge(ctx, node1.ID, node2.ID, RelationTypeRelatedTo, nil, 1.0)

	if err := service.DeleteNode(ctx, node1.ID); err != nil {
		t.Fatalf("failed to delete node: %v", err)
	}

	// Node should be gone
	_, err := service.GetNode(ctx, node1.ID)
	if err == nil {
		t.Error("expected error getting deleted node")
	}

	// Edge should also be gone
	edges, _ := service.GetIncomingEdges(ctx, node2.ID, "")
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
}

func TestListAndSearchNodes(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	service := NewService(db, "test-user")

	service.CreateNode(ctx, "Alice", NodeTypePerson, "", nil)
	service.CreateNode(ctx, "Bob", NodeTypePerson, "", nil)
	service.CreateNode(ctx, "Acme Corp", NodeTypeOrganization, "", nil)

	// List all
	all, _ := service.ListNodes(ctx, "")
	if len(all) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(all))
	}

	// List by type
	people, _ := service.ListNodes(ctx, NodeTypePerson)
	if len(people) != 2 {
		t.Errorf("expected 2 people, got %d", len(people))
	}

	// Search by name
	results, _ := service.SearchNodes(ctx, "A")
	if len(results) != 2 { // Alice and Acme
		t.Errorf("expected 2 results for 'A', got %d", len(results))
	}
}

func TestCreateAndGetEdge(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	service := NewService(db, "test-user")

	node1, _ := service.CreateNode(ctx, "Node 1", NodeTypeEntity, "", nil)
	node2, _ := service.CreateNode(ctx, "Node 2", NodeTypeEntity, "", nil)

	edge, err := service.CreateEdge(ctx, node1.ID, node2.ID, RelationTypePartOf, map[string]any{
		"since": "2024",
	}, 0.8)
	if err != nil {
		t.Fatalf("failed to create edge: %v", err)
	}
	if edge.Weight != 0.8 {
		t.Errorf("expected weight 0.8, got %f", edge.Weight)
	}

	retrieved, _ := service.GetEdge(ctx, edge.ID)
	if retrieved.RelationType != RelationTypePartOf {
		t.Errorf("expected relation '%s', got '%s'", RelationTypePartOf, retrieved.RelationType)
	}
}

func TestEdgeTemporalProperties(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	service := NewService(db, "test-user")

	node1, _ := service.CreateNode(ctx, "Shipyard", NodeTypeOrganization, "", nil)
	node2, _ := service.CreateNode(ctx, "Strike", NodeTypeEvent, "", nil)

	// Create edge with temporal properties
	validFrom := time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC)
	confidence := 0.8
	edge, err := service.CreateEdgeWithParams(ctx, EdgeParams{
		SourceID:        node1.ID,
		TargetID:        node2.ID,
		RelationType:    "predicts",
		Properties:      map[string]any{"prediction_type": "labor"},
		Weight:          1.0,
		ValidFrom:       &validFrom,
		ConfidenceScore: &confidence,
	})
	if err != nil {
		t.Fatalf("failed to create edge with temporal properties: %v", err)
	}

	// Verify temporal properties
	if edge.ValidFrom == nil {
		t.Fatal("expected ValidFrom to be set")
	}
	if !edge.ValidFrom.Equal(validFrom) {
		t.Errorf("expected ValidFrom %v, got %v", validFrom, edge.ValidFrom)
	}
	if edge.ConfidenceScore == nil {
		t.Fatal("expected ConfidenceScore to be set")
	}
	if *edge.ConfidenceScore != 0.8 {
		t.Errorf("expected ConfidenceScore 0.8, got %f", *edge.ConfidenceScore)
	}

	// Retrieve and verify persistence
	retrieved, _ := service.GetEdge(ctx, edge.ID)
	if retrieved.ValidFrom == nil || !retrieved.ValidFrom.Equal(validFrom) {
		t.Errorf("ValidFrom not persisted correctly")
	}
	if retrieved.ConfidenceScore == nil || *retrieved.ConfidenceScore != 0.8 {
		t.Errorf("ConfidenceScore not persisted correctly")
	}

	// Update temporal properties
	newConfidence := 0.95
	retrieved.ConfidenceScore = &newConfidence
	if err := service.UpdateEdge(ctx, retrieved); err != nil {
		t.Fatalf("failed to update edge: %v", err)
	}

	updated, _ := service.GetEdge(ctx, edge.ID)
	if *updated.ConfidenceScore != 0.95 {
		t.Errorf("expected updated ConfidenceScore 0.95, got %f", *updated.ConfidenceScore)
	}
}

func TestGetEdges(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	service := NewService(db, "test-user")

	node1, _ := service.CreateNode(ctx, "Node 1", NodeTypeEntity, "", nil)
	node2, _ := service.CreateNode(ctx, "Node 2", NodeTypeEntity, "", nil)
	node3, _ := service.CreateNode(ctx, "Node 3", NodeTypeEntity, "", nil)

	service.CreateEdge(ctx, node1.ID, node2.ID, RelationTypePartOf, nil, 1.0)
	service.CreateEdge(ctx, node1.ID, node3.ID, RelationTypeRelatedTo, nil, 1.0)
	service.CreateEdge(ctx, node3.ID, node1.ID, RelationTypeDependsOn, nil, 1.0)

	// Outgoing from node1
	outgoing, _ := service.GetOutgoingEdges(ctx, node1.ID, "")
	if len(outgoing) != 2 {
		t.Errorf("expected 2 outgoing edges, got %d", len(outgoing))
	}

	// Outgoing from node1 filtered by type
	partOf, _ := service.GetOutgoingEdges(ctx, node1.ID, RelationTypePartOf)
	if len(partOf) != 1 {
		t.Errorf("expected 1 part_of edge, got %d", len(partOf))
	}

	// Incoming to node1
	incoming, _ := service.GetIncomingEdges(ctx, node1.ID, "")
	if len(incoming) != 1 {
		t.Errorf("expected 1 incoming edge, got %d", len(incoming))
	}
}

func TestGetNeighbors(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	service := NewService(db, "test-user")

	center, _ := service.CreateNode(ctx, "Center", NodeTypeConcept, "", nil)
	neighbor1, _ := service.CreateNode(ctx, "Neighbor 1", NodeTypeEntity, "", nil)
	neighbor2, _ := service.CreateNode(ctx, "Neighbor 2", NodeTypePerson, "", nil)
	incoming, _ := service.CreateNode(ctx, "Incoming", NodeTypeEntity, "", nil)

	service.CreateEdge(ctx, center.ID, neighbor1.ID, RelationTypeRelatedTo, nil, 1.0)
	service.CreateEdge(ctx, center.ID, neighbor2.ID, RelationTypeCreatedBy, nil, 1.0)
	service.CreateEdge(ctx, incoming.ID, center.ID, RelationTypeDependsOn, nil, 1.0)

	// Get outgoing neighbors only
	result, _ := service.GetNeighbors(ctx, center.ID, TraversalOptions{})
	if len(result.Nodes) != 2 {
		t.Errorf("expected 2 outgoing neighbors, got %d", len(result.Nodes))
	}

	// Include reverse
	result, _ = service.GetNeighbors(ctx, center.ID, TraversalOptions{IncludeReverse: true})
	if len(result.Nodes) != 3 {
		t.Errorf("expected 3 total neighbors, got %d", len(result.Nodes))
	}

	// Filter by node type
	result, _ = service.GetNeighbors(ctx, center.ID, TraversalOptions{NodeTypes: []string{NodeTypePerson}})
	if len(result.Nodes) != 1 {
		t.Errorf("expected 1 person neighbor, got %d", len(result.Nodes))
	}
}

func TestTraverse(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	service := NewService(db, "test-user")

	// Create a chain: A -> B -> C -> D
	a, _ := service.CreateNode(ctx, "A", NodeTypeEntity, "", nil)
	b, _ := service.CreateNode(ctx, "B", NodeTypeEntity, "", nil)
	c, _ := service.CreateNode(ctx, "C", NodeTypeEntity, "", nil)
	d, _ := service.CreateNode(ctx, "D", NodeTypeEntity, "", nil)

	service.CreateEdge(ctx, a.ID, b.ID, RelationTypeRelatedTo, nil, 1.0)
	service.CreateEdge(ctx, b.ID, c.ID, RelationTypeRelatedTo, nil, 1.0)
	service.CreateEdge(ctx, c.ID, d.ID, RelationTypeRelatedTo, nil, 1.0)

	// Traverse with depth 2 from A
	result, _ := service.Traverse(ctx, a.ID, TraversalOptions{MaxDepth: 2})
	if len(result.Nodes) != 3 { // A, B, C
		t.Errorf("expected 3 nodes at depth 2, got %d", len(result.Nodes))
	}

	// Traverse full depth
	result, _ = service.Traverse(ctx, a.ID, TraversalOptions{MaxDepth: 10})
	if len(result.Nodes) != 4 { // A, B, C, D
		t.Errorf("expected 4 nodes at full depth, got %d", len(result.Nodes))
	}
}

func TestFindPath(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	service := NewService(db, "test-user")

	// Create a graph: A -> B -> C, A -> D -> C
	a, _ := service.CreateNode(ctx, "A", NodeTypeEntity, "", nil)
	b, _ := service.CreateNode(ctx, "B", NodeTypeEntity, "", nil)
	c, _ := service.CreateNode(ctx, "C", NodeTypeEntity, "", nil)
	d, _ := service.CreateNode(ctx, "D", NodeTypeEntity, "", nil)

	service.CreateEdge(ctx, a.ID, b.ID, RelationTypeRelatedTo, nil, 1.0)
	service.CreateEdge(ctx, b.ID, c.ID, RelationTypeRelatedTo, nil, 1.0)
	service.CreateEdge(ctx, a.ID, d.ID, RelationTypeRelatedTo, nil, 1.0)
	service.CreateEdge(ctx, d.ID, c.ID, RelationTypeRelatedTo, nil, 1.0)

	// Find path from A to C (should find one of the two paths)
	result, err := service.FindPath(ctx, a.ID, c.ID, TraversalOptions{MaxDepth: 5})
	if err != nil {
		t.Fatalf("failed to find path: %v", err)
	}
	if len(result.Nodes) != 3 { // A, (B or D), C
		t.Errorf("expected path of length 3, got %d", len(result.Nodes))
	}
	if result.Nodes[0].ID != a.ID {
		t.Error("path should start with A")
	}
	if result.Nodes[len(result.Nodes)-1].ID != c.ID {
		t.Error("path should end with C")
	}

	// Path to self
	result, _ = service.FindPath(ctx, a.ID, a.ID, TraversalOptions{MaxDepth: 5})
	if len(result.Nodes) != 1 {
		t.Errorf("expected single node for self-path, got %d", len(result.Nodes))
	}

	// No path
	isolated, _ := service.CreateNode(ctx, "Isolated", NodeTypeEntity, "", nil)
	_, err = service.FindPath(ctx, a.ID, isolated.ID, TraversalOptions{MaxDepth: 5})
	if err == nil {
		t.Error("expected error for no path")
	}
}

func TestUserIsolation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	service1 := NewService(db, "user-1")
	service2 := NewService(db, "user-2")

	// User 1 creates a node
	node1, _ := service1.CreateNode(ctx, "User 1 Node", NodeTypeEntity, "", nil)

	// User 2 creates a node
	node2, _ := service2.CreateNode(ctx, "User 2 Node", NodeTypeEntity, "", nil)

	// User 1 should only see their node
	nodes1, _ := service1.ListNodes(ctx, "")
	if len(nodes1) != 1 {
		t.Errorf("user 1 expected 1 node, got %d", len(nodes1))
	}
	if nodes1[0].ID != node1.ID {
		t.Error("user 1 got wrong node")
	}

	// User 2 should only see their node
	nodes2, _ := service2.ListNodes(ctx, "")
	if len(nodes2) != 1 {
		t.Errorf("user 2 expected 1 node, got %d", len(nodes2))
	}
	if nodes2[0].ID != node2.ID {
		t.Error("user 2 got wrong node")
	}
}

func TestTools(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	tools := NewTools(db, "test-user")

	// Create node via kg_add tool
	result, _ := tools.KgAddTool(ctx, KgAddInput{
		Name: "Tool Test Node",
		Type: NodeTypeConcept,
		Properties: map[string]any{
			"test": true,
		},
	})
	if !result.Success {
		t.Fatalf("kg_add (node) failed: %s", result.Error)
	}

	addResult := result.Content.(map[string]any)
	if addResult["action"] != "created" {
		t.Fatalf("expected action=created, got %v", addResult["action"])
	}
	node := addResult["node"].(NodeDTO)

	// Duplicate detection: adding same node should return "existing"
	result, _ = tools.KgAddTool(ctx, KgAddInput{
		Name: "Tool Test Node",
		Type: NodeTypeConcept,
	})
	if !result.Success {
		t.Fatalf("kg_add (duplicate) failed: %s", result.Error)
	}
	dupResult := result.Content.(map[string]any)
	if dupResult["action"] != "existing" {
		t.Fatalf("expected action=existing for duplicate, got %v", dupResult["action"])
	}

	// Get node via kg_get tool
	result, _ = tools.KgGetTool(ctx, KgGetInput{ID: node.ID})
	if !result.Success {
		t.Fatalf("kg_get failed: %s", result.Error)
	}
	getResult := result.Content.(map[string]any)
	if getResult["kind"] != "node" {
		t.Errorf("expected kind=node, got %v", getResult["kind"])
	}

	// Search via kg_search tool (list mode)
	result, _ = tools.KgSearchTool(ctx, KgSearchInput{Mode: "list"})
	if !result.Success {
		t.Fatalf("kg_search (list) failed: %s", result.Error)
	}
	scored := result.Content.([]ScoredNodeDTO)
	if len(scored) != 1 {
		t.Errorf("expected 1 node, got %d", len(scored))
	}

	// Search via kg_search tool (text mode)
	result, _ = tools.KgSearchTool(ctx, KgSearchInput{Query: "Tool"})
	if !result.Success {
		t.Fatalf("kg_search (text) failed: %s", result.Error)
	}

	// Create second node and edge via kg_add
	result, _ = tools.KgAddTool(ctx, KgAddInput{
		Name: "Related Node",
		Type: NodeTypeEntity,
	})
	if !result.Success {
		t.Fatalf("kg_add (node 2) failed: %s", result.Error)
	}
	node2 := result.Content.(map[string]any)["node"].(NodeDTO)

	result, _ = tools.KgAddTool(ctx, KgAddInput{
		SourceNodeID: node.ID,
		TargetNodeID: node2.ID,
		Type:         RelationTypeRelatedTo,
		Source:       "https://example.com",
		ExtractedBy:  "test_agent",
	})
	if !result.Success {
		t.Fatalf("kg_add (edge) failed: %s", result.Error)
	}
	edge := result.Content.(EdgeDTO)
	if edge.Source != "https://example.com" {
		t.Errorf("expected provenance source, got %q", edge.Source)
	}

	// Explore neighbors via kg_explore
	result, _ = tools.KgExploreTool(ctx, KgExploreInput{StartNodeID: node.ID})
	if !result.Success {
		t.Fatalf("kg_explore failed: %s", result.Error)
	}

	// Update node via kg_update
	result, _ = tools.KgUpdateTool(ctx, KgUpdateInput{ID: node.ID, Name: "Updated Name"})
	if !result.Success {
		t.Fatalf("kg_update (node) failed: %s", result.Error)
	}

	// Delete edge via kg_delete
	result, _ = tools.KgDeleteTool(ctx, KgDeleteInput{ID: edge.ID})
	if !result.Success {
		t.Fatalf("kg_delete (edge) failed: %s", result.Error)
	}

	// Delete node via kg_delete
	result, _ = tools.KgDeleteTool(ctx, KgDeleteInput{ID: node.ID})
	if !result.Success {
		t.Fatalf("kg_delete (node) failed: %s", result.Error)
	}
}

func TestToolsWrapWithDescriptions(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	tools := NewTools(db, "test-user")

	// Verify ToolProvider interface is implemented
	wrapped := gowild_agentic_loop.WrapToolsWithDescriptions(tools)

	// Should have 6 consolidated tools
	if len(wrapped) != 6 {
		t.Errorf("expected 6 tools, got %d", len(wrapped))
	}

	// Verify descriptions are populated
	for _, tool := range wrapped {
		if tool.Description() == "" {
			t.Errorf("tool %s has empty description", tool.Name())
		}
	}

	// Verify specific tool names
	toolNames := make(map[string]bool)
	for _, tool := range wrapped {
		toolNames[tool.Name()] = true
	}

	expectedTools := []string{
		"kg_search", "kg_add", "kg_get", "kg_update", "kg_delete", "kg_explore",
	}

	for _, expected := range expectedTools {
		if !toolNames[expected] {
			t.Errorf("missing expected tool: %s", expected)
		}
	}
}
