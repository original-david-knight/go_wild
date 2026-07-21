package gowild_knowledge_graph

import (
	"context"
	"fmt"
	"time"

	"github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/data"
)

// Tools provides knowledge graph operations as agent tools.
// Methods ending with "Tool" are automatically discovered by gowild_agentic_loop.WrapTools().
type Tools struct {
	service *Service
}

// NewTools creates a new Tools instance.
func NewTools(db gowild_data.Database, userID string) *Tools {
	return &Tools{
		service: NewService(db, userID),
	}
}

// SetEmbeddingService enables semantic search by setting the embedding service.
func (t *Tools) SetEmbeddingService(es *EmbeddingService) {
	t.service.SetEmbeddingService(es)
}

// --- DTOs ---

// NodeDTO is the agent-facing representation of a Node.
type NodeDTO struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Notes      string         `json:"notes,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Status     string         `json:"status,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// EdgeDTO is the agent-facing representation of an Edge.
type EdgeDTO struct {
	ID              string         `json:"id"`
	SourceNodeID    string         `json:"source_node_id"`
	TargetNodeID    string         `json:"target_node_id"`
	RelationType    string         `json:"relation_type"`
	Properties      map[string]any `json:"properties,omitempty"`
	Weight          float64        `json:"weight"`
	ValidFrom       *time.Time     `json:"valid_from,omitempty"`
	ValidUntil      *time.Time     `json:"valid_until,omitempty"`
	ConfidenceScore *float64       `json:"confidence_score,omitempty"`
	Source          string         `json:"source,omitempty"`
	ExtractedBy     string         `json:"extracted_by,omitempty"`
	Status          string         `json:"status,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// QueryResultDTO is the agent-facing representation of a QueryResult.
type QueryResultDTO struct {
	Nodes []NodeDTO `json:"nodes"`
	Edges []EdgeDTO `json:"edges"`
}

// ScoredNodeDTO is the agent-facing representation of a scored node.
type ScoredNodeDTO struct {
	Node  NodeDTO `json:"node"`
	Score float32 `json:"score"`
}

// --- DTO converters ---

func toNodeDTO(n *Node) NodeDTO {
	return NodeDTO{
		ID:         n.ID,
		Name:       n.Name,
		Type:       n.Type,
		Notes:      n.Notes,
		Properties: n.Properties,
		Status:     n.Status,
		CreatedAt:  n.CreatedAt,
		UpdatedAt:  n.UpdatedAt,
	}
}

func toNodeDTOs(nodes []Node) []NodeDTO {
	dtos := make([]NodeDTO, len(nodes))
	for i, n := range nodes {
		dtos[i] = toNodeDTO(&n)
	}
	return dtos
}

func toEdgeDTO(e *Edge) EdgeDTO {
	return EdgeDTO{
		ID:              e.ID,
		SourceNodeID:    e.SourceNodeID,
		TargetNodeID:    e.TargetNodeID,
		RelationType:    e.RelationType,
		Properties:      e.Properties,
		Weight:          e.Weight,
		ValidFrom:       e.ValidFrom,
		ValidUntil:      e.ValidUntil,
		ConfidenceScore: e.ConfidenceScore,
		Source:          e.Source,
		ExtractedBy:     e.ExtractedBy,
		Status:          e.Status,
		CreatedAt:       e.CreatedAt,
		UpdatedAt:       e.UpdatedAt,
	}
}

func toEdgeDTOs(edges []Edge) []EdgeDTO {
	dtos := make([]EdgeDTO, len(edges))
	for i, e := range edges {
		dtos[i] = toEdgeDTO(&e)
	}
	return dtos
}

func toQueryResultDTO(r *QueryResult) QueryResultDTO {
	return QueryResultDTO{
		Nodes: toNodeDTOs(r.Nodes),
		Edges: toEdgeDTOs(r.Edges),
	}
}

func toScoredNodeDTOs(nodes []scoredNode) []ScoredNodeDTO {
	dtos := make([]ScoredNodeDTO, len(nodes))
	for i, n := range nodes {
		dtos[i] = ScoredNodeDTO{
			Node:  toNodeDTO(&n.Node),
			Score: n.Score,
		}
	}
	return dtos
}

// nodesToScoredDTOs wraps plain nodes as ScoredNodeDTOs with score 1.0.
func nodesToScoredDTOs(nodes []Node) []ScoredNodeDTO {
	dtos := make([]ScoredNodeDTO, len(nodes))
	for i, n := range nodes {
		dtos[i] = ScoredNodeDTO{
			Node:  toNodeDTO(&n),
			Score: 1.0,
		}
	}
	return dtos
}

// --- Tool descriptions ---

// DescribeTool returns the description for a tool by name.
func (t *Tools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"kg_search": "Search the knowledge graph. Modes: 'text' (default) searches by name, 'semantic' searches by meaning, 'similar' finds nodes like a given node_id, 'list' returns all nodes. Returns scored results.",
		"kg_add":    "Add knowledge to the graph. To create a node: provide name and type. Use the notes field to store observations, context, summaries, or any information worth remembering about the entity. To create an edge: provide source_node_id, target_node_id, and type (relation). Edges support provenance (source, extracted_by), temporal (valid_from, valid_until), and confidence (confidence_score) fields. Duplicate nodes and contradictory edges are automatically detected.",
		"kg_get":    "Retrieve a node or edge by its ID. Automatically detects whether the ID refers to a node or edge.",
		"kg_update": "Update a node or edge by its ID. For nodes: set name, type, notes, or properties. Use notes to record new information learned about an entity. For edges: set type (relation), properties, weight, valid_from, valid_until, confidence_score. Automatically detects node vs edge.",
		"kg_delete": "Delete a node or edge by its ID. Node deletion cascades to all connected edges. Automatically detects node vs edge.",
		"kg_explore": "Explore graph relationships from a starting node. With just start_node_id: returns immediate neighbors. With max_depth > 1: BFS traversal. With end_node_id: finds shortest path. Supports filtering by relation_types and node_types.",
	}
	return descriptions[name]
}

// --- 1. kg_search ---

// KgSearchInput defines input for searching the knowledge graph.
type KgSearchInput struct {
	Query    string `json:"query,omitempty" description:"Text or natural language query"`
	Mode     string `json:"mode,omitempty" description:"Search mode: text (default), semantic, similar, list" enum:"text,semantic,similar,list"`
	NodeID   string `json:"node_id,omitempty" description:"Node ID (required for 'similar' mode)"`
	NodeType string `json:"node_type,omitempty" description:"Filter results by node type"`
	Limit    int    `json:"limit,omitempty" description:"Max results (default 10)"`
}

// KgSearchTool searches the knowledge graph.
func (t *Tools) KgSearchTool(ctx context.Context, input KgSearchInput) (*gowild_agentic_loop.ToolResult, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}

	mode := input.Mode
	if mode == "" {
		mode = "text"
	}

	switch mode {
	case "list":
		nodes, err := t.service.ListNodes(ctx, input.NodeType)
		if err != nil {
			return gowild_agentic_loop.NewErrorResult(err.Error()), nil
		}
		return gowild_agentic_loop.NewSuccessResult(nodesToScoredDTOs(nodes)), nil

	case "semantic":
		if input.Query == "" {
			return gowild_agentic_loop.NewErrorResult("query is required for semantic search"), nil
		}
		results, err := t.service.semanticSearch(ctx, input.Query, limit)
		if err != nil {
			return gowild_agentic_loop.NewErrorResult(err.Error()), nil
		}
		return gowild_agentic_loop.NewSuccessResult(toScoredNodeDTOs(results)), nil

	case "similar":
		if input.NodeID == "" {
			return gowild_agentic_loop.NewErrorResult("node_id is required for similar mode"), nil
		}
		results, err := t.service.findSimilarNodes(ctx, input.NodeID, limit)
		if err != nil {
			return gowild_agentic_loop.NewErrorResult(err.Error()), nil
		}
		return gowild_agentic_loop.NewSuccessResult(toScoredNodeDTOs(results)), nil

	default: // "text"
		if input.Query == "" {
			return gowild_agentic_loop.NewErrorResult("query is required for text search"), nil
		}
		nodes, err := t.service.SearchNodes(ctx, input.Query)
		if err != nil {
			return gowild_agentic_loop.NewErrorResult(err.Error()), nil
		}
		// Filter by node type if specified
		if input.NodeType != "" {
			filtered := make([]Node, 0, len(nodes))
			for _, n := range nodes {
				if n.Type == input.NodeType {
					filtered = append(filtered, n)
				}
			}
			nodes = filtered
		}
		return gowild_agentic_loop.NewSuccessResult(nodesToScoredDTOs(nodes)), nil
	}
}

// --- 2. kg_add ---

// KgAddInput defines input for adding knowledge (node or edge).
type KgAddInput struct {
	// Node fields
	Name       string         `json:"name,omitempty" description:"Node name (creates a node when no source/target IDs given)"`
	Type       string         `json:"type,omitempty" description:"Node type (concept, entity, person, organization, event, location, document) or edge relation type (related_to, part_of, has_a, is_a, depends_on, created_by, owned_by, references, precedes, follows, similar_to, contradicts)" required:"true"`
	Notes      string         `json:"notes,omitempty" description:"Free-form notes about this node. Use this to store observations, context, summaries, learned details, or any information worth remembering about the entity."`
	Properties map[string]any `json:"properties,omitempty" description:"Additional properties"`

	// Edge fields — presence of both triggers edge creation
	SourceNodeID    string   `json:"source_node_id,omitempty" description:"Source node ID (creates an edge when both source and target given)"`
	TargetNodeID    string   `json:"target_node_id,omitempty" description:"Target node ID (creates an edge when both source and target given)"`
	Weight          float64  `json:"weight,omitempty" description:"Edge weight (default 1.0)"`
	ValidFrom       string   `json:"valid_from,omitempty" description:"RFC3339 timestamp for when this fact became valid"`
	ValidUntil      string   `json:"valid_until,omitempty" description:"RFC3339 timestamp for when this fact expires"`
	ConfidenceScore *float64 `json:"confidence_score,omitempty" description:"Certainty 0.0-1.0 for predictions or estimates"`

	// Provenance
	Source      string `json:"source,omitempty" description:"URL or document the fact was extracted from"`
	ExtractedBy string `json:"extracted_by,omitempty" description:"Tool or agent that generated this fact"`
}

// KgAddTool adds a node or edge to the knowledge graph.
func (t *Tools) KgAddTool(ctx context.Context, input KgAddInput) (*gowild_agentic_loop.ToolResult, error) {
	// Edge creation: both source and target IDs present
	if input.SourceNodeID != "" && input.TargetNodeID != "" {
		return t.addEdge(ctx, input)
	}

	// Node creation
	if input.Name == "" {
		return gowild_agentic_loop.NewErrorResult("name is required to create a node, or provide source_node_id + target_node_id to create an edge"), nil
	}
	if input.Type == "" {
		return gowild_agentic_loop.NewErrorResult("type is required"), nil
	}

	// Check for duplicate by name before creating
	existing, err := t.service.SearchNodes(ctx, input.Name)
	if err == nil {
		for _, n := range existing {
			if n.Name == input.Name && n.Type == input.Type {
				return gowild_agentic_loop.NewSuccessResult(map[string]any{
					"action":   "existing",
					"message":  fmt.Sprintf("Node '%s' (type: %s) already exists", input.Name, input.Type),
					"node":     toNodeDTO(&n),
				}), nil
			}
		}
	}

	node, err := t.service.CreateNode(ctx, input.Name, input.Type, input.Notes, input.Properties)
	if err != nil {
		return gowild_agentic_loop.NewErrorResult(err.Error()), nil
	}
	return gowild_agentic_loop.NewSuccessResult(map[string]any{
		"action": "created",
		"node":   toNodeDTO(node),
	}), nil
}

func (t *Tools) addEdge(ctx context.Context, input KgAddInput) (*gowild_agentic_loop.ToolResult, error) {
	if input.Type == "" {
		return gowild_agentic_loop.NewErrorResult("type (relation type) is required for edges"), nil
	}

	weight := input.Weight
	if weight == 0 {
		weight = 1.0
	}

	params := EdgeParams{
		SourceID:        input.SourceNodeID,
		TargetID:        input.TargetNodeID,
		RelationType:    input.Type,
		Properties:      input.Properties,
		Weight:          weight,
		ConfidenceScore: input.ConfidenceScore,
		Source:          input.Source,
		ExtractedBy:     input.ExtractedBy,
	}

	if input.ValidFrom != "" {
		validFrom, err := time.Parse(time.RFC3339, input.ValidFrom)
		if err != nil {
			return gowild_agentic_loop.NewErrorResult(fmt.Sprintf("invalid valid_from format (use RFC3339): %v", err)), nil
		}
		params.ValidFrom = &validFrom
	}
	if input.ValidUntil != "" {
		validUntil, err := time.Parse(time.RFC3339, input.ValidUntil)
		if err != nil {
			return gowild_agentic_loop.NewErrorResult(fmt.Sprintf("invalid valid_until format (use RFC3339): %v", err)), nil
		}
		params.ValidUntil = &validUntil
	}
	if input.ExtractedBy != "" {
		now := time.Now()
		params.ExtractedAt = &now
	}

	edge, err := t.service.CreateEdgeWithParams(ctx, params)
	if err != nil {
		return gowild_agentic_loop.NewErrorResult(err.Error()), nil
	}
	return gowild_agentic_loop.NewSuccessResult(toEdgeDTO(edge)), nil
}

// --- 3. kg_get ---

// KgGetInput defines input for retrieving a node or edge by ID.
type KgGetInput struct {
	ID string `json:"id" description:"Node or edge ID to retrieve" required:"true"`
}

// KgGetTool retrieves a node or edge by ID.
func (t *Tools) KgGetTool(ctx context.Context, input KgGetInput) (*gowild_agentic_loop.ToolResult, error) {
	// Try node first
	node, err := t.service.GetNode(ctx, input.ID)
	if err == nil {
		return gowild_agentic_loop.NewSuccessResult(map[string]any{
			"kind": "node",
			"node": toNodeDTO(node),
		}), nil
	}

	// Try edge
	edge, err := t.service.GetEdge(ctx, input.ID)
	if err == nil {
		return gowild_agentic_loop.NewSuccessResult(map[string]any{
			"kind": "edge",
			"edge": toEdgeDTO(edge),
		}), nil
	}

	return gowild_agentic_loop.NewErrorResult(fmt.Sprintf("no node or edge found with ID %s", input.ID)), nil
}

// --- 4. kg_update ---

// KgUpdateInput defines input for updating a node or edge.
type KgUpdateInput struct {
	ID              string         `json:"id" description:"Node or edge ID to update" required:"true"`
	Name            string         `json:"name,omitempty" description:"New name (nodes only)"`
	Type            string         `json:"type,omitempty" description:"New type (nodes) or relation type (edges)"`
	Notes           *string        `json:"notes,omitempty" description:"Updated notes (nodes only). Use this to append new observations or replace existing notes."`
	Properties      map[string]any `json:"properties,omitempty" description:"New properties (replaces existing)"`
	Weight          *float64       `json:"weight,omitempty" description:"New weight (edges only)"`
	ValidFrom       string         `json:"valid_from,omitempty" description:"RFC3339 timestamp (edges only)"`
	ValidUntil      string         `json:"valid_until,omitempty" description:"RFC3339 expiration timestamp (edges only)"`
	ConfidenceScore *float64       `json:"confidence_score,omitempty" description:"Certainty 0.0-1.0 (edges only)"`
	Status          string         `json:"status,omitempty" description:"Set status: active, expired, or invalid"`
}

// KgUpdateTool updates a node or edge by ID.
func (t *Tools) KgUpdateTool(ctx context.Context, input KgUpdateInput) (*gowild_agentic_loop.ToolResult, error) {
	// Try node first
	node, err := t.service.GetNode(ctx, input.ID)
	if err == nil {
		if input.Name != "" {
			node.Name = input.Name
		}
		if input.Type != "" {
			node.Type = input.Type
		}
		if input.Notes != nil {
			node.Notes = *input.Notes
		}
		if input.Properties != nil {
			node.Properties = input.Properties
		}
		if input.Status != "" {
			node.Status = input.Status
		}
		if err := t.service.UpdateNode(ctx, node); err != nil {
			return gowild_agentic_loop.NewErrorResult(err.Error()), nil
		}
		return gowild_agentic_loop.NewSuccessResult(map[string]any{
			"kind": "node",
			"node": toNodeDTO(node),
		}), nil
	}

	// Try edge
	edge, edgeErr := t.service.GetEdge(ctx, input.ID)
	if edgeErr != nil {
		return gowild_agentic_loop.NewErrorResult(fmt.Sprintf("no node or edge found with ID %s", input.ID)), nil
	}

	if input.Type != "" {
		edge.RelationType = input.Type
	}
	if input.Properties != nil {
		edge.Properties = input.Properties
	}
	if input.Weight != nil {
		edge.Weight = *input.Weight
	}
	if input.ValidFrom != "" {
		validFrom, err := time.Parse(time.RFC3339, input.ValidFrom)
		if err != nil {
			return gowild_agentic_loop.NewErrorResult(fmt.Sprintf("invalid valid_from format (use RFC3339): %v", err)), nil
		}
		edge.ValidFrom = &validFrom
	}
	if input.ValidUntil != "" {
		validUntil, err := time.Parse(time.RFC3339, input.ValidUntil)
		if err != nil {
			return gowild_agentic_loop.NewErrorResult(fmt.Sprintf("invalid valid_until format (use RFC3339): %v", err)), nil
		}
		edge.ValidUntil = &validUntil
	}
	if input.ConfidenceScore != nil {
		edge.ConfidenceScore = input.ConfidenceScore
	}
	if input.Status != "" {
		edge.Status = input.Status
	}

	if err := t.service.UpdateEdge(ctx, edge); err != nil {
		return gowild_agentic_loop.NewErrorResult(err.Error()), nil
	}
	return gowild_agentic_loop.NewSuccessResult(map[string]any{
		"kind": "edge",
		"edge": toEdgeDTO(edge),
	}), nil
}

// --- 5. kg_delete ---

// KgDeleteInput defines input for deleting a node or edge.
type KgDeleteInput struct {
	ID string `json:"id" description:"Node or edge ID to delete" required:"true"`
}

// KgDeleteTool deletes a node or edge by ID.
func (t *Tools) KgDeleteTool(ctx context.Context, input KgDeleteInput) (*gowild_agentic_loop.ToolResult, error) {
	// Try node first
	if _, err := t.service.GetNode(ctx, input.ID); err == nil {
		if err := t.service.DeleteNode(ctx, input.ID); err != nil {
			return gowild_agentic_loop.NewErrorResult(err.Error()), nil
		}
		return gowild_agentic_loop.NewSuccessResult(fmt.Sprintf("Node %s and its edges deleted", input.ID)), nil
	}

	// Try edge
	if _, err := t.service.GetEdge(ctx, input.ID); err == nil {
		if err := t.service.DeleteEdge(ctx, input.ID); err != nil {
			return gowild_agentic_loop.NewErrorResult(err.Error()), nil
		}
		return gowild_agentic_loop.NewSuccessResult(fmt.Sprintf("Edge %s deleted", input.ID)), nil
	}

	return gowild_agentic_loop.NewErrorResult(fmt.Sprintf("no node or edge found with ID %s", input.ID)), nil
}

// --- 6. kg_explore ---

// KgExploreInput defines input for exploring graph relationships.
type KgExploreInput struct {
	StartNodeID    string   `json:"start_node_id" description:"Starting node ID" required:"true"`
	EndNodeID      string   `json:"end_node_id,omitempty" description:"Target node ID (triggers shortest path search)"`
	MaxDepth       int      `json:"max_depth,omitempty" description:"Max traversal depth (default: 1 for neighbors, 3 for traverse, 5 for path)"`
	RelationTypes  []string `json:"relation_types,omitempty" description:"Filter by relation types"`
	NodeTypes      []string `json:"node_types,omitempty" description:"Filter by node types"`
	IncludeReverse *bool    `json:"include_reverse,omitempty" description:"Include incoming edges (default true)"`
	IncludeExpired bool     `json:"include_expired,omitempty" description:"Include expired/invalid items (default false)"`
}

// KgExploreTool explores graph relationships from a starting node.
func (t *Tools) KgExploreTool(ctx context.Context, input KgExploreInput) (*gowild_agentic_loop.ToolResult, error) {
	includeReverse := true
	if input.IncludeReverse != nil {
		includeReverse = *input.IncludeReverse
	}

	opts := TraversalOptions{
		RelationTypes:  input.RelationTypes,
		NodeTypes:      input.NodeTypes,
		IncludeReverse: includeReverse,
		IncludeExpired: input.IncludeExpired,
	}

	// Path mode: end_node_id present
	if input.EndNodeID != "" {
		opts.MaxDepth = input.MaxDepth
		if opts.MaxDepth <= 0 {
			opts.MaxDepth = 5
		}
		result, err := t.service.FindPath(ctx, input.StartNodeID, input.EndNodeID, opts)
		if err != nil {
			return gowild_agentic_loop.NewErrorResult(err.Error()), nil
		}
		return gowild_agentic_loop.NewSuccessResult(toQueryResultDTO(result)), nil
	}

	// Traverse mode: max_depth > 1
	if input.MaxDepth > 1 {
		opts.MaxDepth = input.MaxDepth
		result, err := t.service.Traverse(ctx, input.StartNodeID, opts)
		if err != nil {
			return gowild_agentic_loop.NewErrorResult(err.Error()), nil
		}
		return gowild_agentic_loop.NewSuccessResult(toQueryResultDTO(result)), nil
	}

	// Default: neighbors (single hop)
	result, err := t.service.GetNeighbors(ctx, input.StartNodeID, opts)
	if err != nil {
		return gowild_agentic_loop.NewErrorResult(err.Error()), nil
	}
	return gowild_agentic_loop.NewSuccessResult(toQueryResultDTO(result)), nil
}
