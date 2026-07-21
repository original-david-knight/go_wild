package gowild_knowledge_graph

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/data"
	"github.com/google/uuid"
)

// Service provides operations for managing a knowledge graph.
type Service struct {
	db               gowild_data.Database
	userID           string
	embeddingService *EmbeddingService
}

// NewService creates a new knowledge graph service for the given user.
func NewService(db gowild_data.Database, userID string) *Service {
	return &Service{
		db:     db,
		userID: userID,
	}
}

// userDB returns a user-scoped database.
func (s *Service) userDB() gowild_data.UserDatabase {
	return s.db.ForUser(s.userID)
}

// newID generates a new unique ID.
func newID() string {
	return uuid.New().String()
}

// CreateNode creates a new node in the knowledge graph.
// If an embedding service is configured, automatically generates an embedding.
func (s *Service) CreateNode(ctx context.Context, name, nodeType, notes string, properties map[string]any) (*Node, error) {
	now := time.Now()
	node := &Node{
		ID:         newID(),
		UserID:     s.userID,
		Name:       name,
		Type:       nodeType,
		Notes:      notes,
		Properties: properties,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Generate embedding if service is available
	if s.embeddingService != nil {
		if embedding, err := embedNode(s.embeddingService, ctx, node); err == nil {
			node.Embedding = embedding
		}
		// Silently continue if embedding fails - node creation shouldn't fail due to embedding
	}

	if err := s.userDB().Table(Node{}).Insert(ctx, node); err != nil {
		return nil, fmt.Errorf("failed to create node: %w", err)
	}
	return node, nil
}

// GetNode retrieves a node by ID.
func (s *Service) GetNode(ctx context.Context, nodeID string) (*Node, error) {
	var node Node
	if err := s.userDB().Table(Node{}).Get(ctx, nodeID, &node); err != nil {
		return nil, fmt.Errorf("failed to get node %s: %w", nodeID, err)
	}
	return &node, nil
}

// UpdateNode updates an existing node.
// If an embedding service is configured, regenerates the embedding.
func (s *Service) UpdateNode(ctx context.Context, node *Node) error {
	node.UpdatedAt = time.Now()

	// Regenerate embedding if service is available
	if s.embeddingService != nil {
		if embedding, err := embedNode(s.embeddingService, ctx, node); err == nil {
			node.Embedding = embedding
		}
		// Silently continue if embedding fails - node update shouldn't fail due to embedding
	}

	if err := s.userDB().Table(Node{}).Update(ctx, node); err != nil {
		return fmt.Errorf("failed to update node %s: %w", node.ID, err)
	}
	return nil
}

// DeleteNode removes a node and all its connected edges.
func (s *Service) DeleteNode(ctx context.Context, nodeID string) error {
	return s.db.RunInTransaction(ctx, func(txDB gowild_data.Database) error {
		userTxDB := txDB.ForUser(s.userID)

		// Delete all edges connected to this node
		edges, err := s.getEdgesForNodeTx(ctx, userTxDB, nodeID)
		if err != nil {
			return err
		}
		for _, edge := range edges {
			if err := userTxDB.Table(Edge{}).Delete(ctx, edge.ID); err != nil {
				return fmt.Errorf("failed to delete edge %s: %w", edge.ID, err)
			}
		}

		// Delete the node
		if err := userTxDB.Table(Node{}).Delete(ctx, nodeID); err != nil {
			return fmt.Errorf("failed to delete node %s: %w", nodeID, err)
		}
		return nil
	})
}

// ListNodes returns all active nodes, optionally filtered by type.
func (s *Service) ListNodes(ctx context.Context, nodeType string) ([]Node, error) {
	return s.listNodes(ctx, nodeType, false)
}

// listNodes returns nodes with optional expired/invalid inclusion.
func (s *Service) listNodes(ctx context.Context, nodeType string, includeExpired bool) ([]Node, error) {
	opts := gowild_data.QueryOpts{}
	if nodeType != "" {
		opts.Where = map[string]any{"type": nodeType}
	}
	results, err := s.userDB().Table(Node{}).Query(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}
	nodes := convertNodes(results)
	if includeExpired {
		return nodes, nil
	}
	active := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if isActiveNode(&n, false) {
			active = append(active, n)
		}
	}
	return active, nil
}

// SearchNodes finds nodes by name pattern.
// Note: This performs a case-insensitive substring match by fetching all nodes
// and filtering in memory, since the data layer doesn't support LIKE queries.
func (s *Service) SearchNodes(ctx context.Context, namePattern string) ([]Node, error) {
	allNodes, err := s.ListNodes(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to search nodes: %w", err)
	}

	pattern := strings.ToLower(namePattern)
	var matches []Node
	for _, node := range allNodes {
		if strings.Contains(strings.ToLower(node.Name), pattern) {
			matches = append(matches, node)
		}
	}
	return matches, nil
}

// EdgeParams holds parameters for creating an edge.
type EdgeParams struct {
	SourceID        string
	TargetID        string
	RelationType    string
	Properties      map[string]any
	Weight          float64
	ValidFrom       *time.Time
	ConfidenceScore *float64
	Source          string     // Provenance: URL or document identifier
	ExtractedBy     string     // Provenance: tool or agent that created this fact
	ExtractedAt     *time.Time // Provenance: when the fact was extracted
	ValidUntil      *time.Time // TTL: when this fact expires
}

// CreateEdge creates a relationship between two nodes.
func (s *Service) CreateEdge(ctx context.Context, sourceID, targetID, relationType string, properties map[string]any, weight float64) (*Edge, error) {
	return s.CreateEdgeWithParams(ctx, EdgeParams{
		SourceID:     sourceID,
		TargetID:     targetID,
		RelationType: relationType,
		Properties:   properties,
		Weight:       weight,
	})
}

// CreateEdgeWithParams creates a relationship between two nodes with full control over temporal properties.
func (s *Service) CreateEdgeWithParams(ctx context.Context, params EdgeParams) (*Edge, error) {
	// Verify both nodes exist
	if _, err := s.GetNode(ctx, params.SourceID); err != nil {
		return nil, fmt.Errorf("source node not found: %w", err)
	}
	if _, err := s.GetNode(ctx, params.TargetID); err != nil {
		return nil, fmt.Errorf("target node not found: %w", err)
	}

	now := time.Now()
	edge := &Edge{
		ID:              newID(),
		UserID:          s.userID,
		SourceNodeID:    params.SourceID,
		TargetNodeID:    params.TargetID,
		RelationType:    params.RelationType,
		Properties:      params.Properties,
		Weight:          params.Weight,
		ValidFrom:       params.ValidFrom,
		ConfidenceScore: params.ConfidenceScore,
		Source:          params.Source,
		ExtractedBy:     params.ExtractedBy,
		ExtractedAt:     params.ExtractedAt,
		ValidUntil:      params.ValidUntil,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	// Run consistency checks
	if result, err := s.CheckConsistency(ctx, edge); err != nil {
		return nil, fmt.Errorf("consistency check failed: %w", err)
	} else if !result.OK {
		return nil, fmt.Errorf("consistency check: %s (conflict_id=%s) %s", result.Issue, result.ConflictID, result.Suggestion)
	}

	if err := s.userDB().Table(Edge{}).Insert(ctx, edge); err != nil {
		return nil, fmt.Errorf("failed to create edge: %w", err)
	}
	return edge, nil
}

// GetEdge retrieves an edge by ID.
func (s *Service) GetEdge(ctx context.Context, edgeID string) (*Edge, error) {
	var edge Edge
	if err := s.userDB().Table(Edge{}).Get(ctx, edgeID, &edge); err != nil {
		return nil, fmt.Errorf("failed to get edge %s: %w", edgeID, err)
	}
	return &edge, nil
}

// UpdateEdge updates an existing edge.
func (s *Service) UpdateEdge(ctx context.Context, edge *Edge) error {
	edge.UpdatedAt = time.Now()
	if err := s.userDB().Table(Edge{}).Update(ctx, edge); err != nil {
		return fmt.Errorf("failed to update edge %s: %w", edge.ID, err)
	}
	return nil
}

// DeleteEdge removes an edge.
func (s *Service) DeleteEdge(ctx context.Context, edgeID string) error {
	if err := s.userDB().Table(Edge{}).Delete(ctx, edgeID); err != nil {
		return fmt.Errorf("failed to delete edge %s: %w", edgeID, err)
	}
	return nil
}

// GetOutgoingEdges returns all edges originating from a node.
func (s *Service) GetOutgoingEdges(ctx context.Context, nodeID string, relationType string) ([]Edge, error) {
	where := map[string]any{"source_node_id": nodeID}
	if relationType != "" {
		where["relation_type"] = relationType
	}
	opts := gowild_data.QueryOpts{Where: where}
	results, err := s.userDB().Table(Edge{}).Query(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get outgoing edges: %w", err)
	}
	return convertEdges(results), nil
}

// GetIncomingEdges returns all edges pointing to a node.
func (s *Service) GetIncomingEdges(ctx context.Context, nodeID string, relationType string) ([]Edge, error) {
	where := map[string]any{"target_node_id": nodeID}
	if relationType != "" {
		where["relation_type"] = relationType
	}
	opts := gowild_data.QueryOpts{Where: where}
	results, err := s.userDB().Table(Edge{}).Query(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get incoming edges: %w", err)
	}
	return convertEdges(results), nil
}

// getEdgesForNodeTx returns all edges connected to a node within a transaction.
func (s *Service) getEdgesForNodeTx(ctx context.Context, db gowild_data.UserDatabase, nodeID string) ([]Edge, error) {
	var allEdges []Edge

	outgoingResults, err := db.Table(Edge{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"source_node_id": nodeID},
	})
	if err != nil {
		return nil, err
	}
	allEdges = append(allEdges, convertEdges(outgoingResults)...)

	incomingResults, err := db.Table(Edge{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"target_node_id": nodeID},
	})
	if err != nil {
		return nil, err
	}
	allEdges = append(allEdges, convertEdges(incomingResults)...)

	return allEdges, nil
}

// isActiveEdge returns true if the edge should be included given the filter options.
func isActiveEdge(edge *Edge, includeExpired bool) bool {
	if includeExpired {
		return true
	}
	return edge.Status == "" || edge.Status == StatusActive
}

// isActiveNode returns true if the node should be included given the filter options.
func isActiveNode(node *Node, includeExpired bool) bool {
	if includeExpired {
		return true
	}
	return node.Status == "" || node.Status == StatusActive
}

// GetNeighbors returns all nodes directly connected to the given node.
func (s *Service) GetNeighbors(ctx context.Context, nodeID string, opts TraversalOptions) (*QueryResult, error) {
	result := &QueryResult{
		Nodes: []Node{},
		Edges: []Edge{},
	}

	// Get outgoing edges
	outgoing, err := s.GetOutgoingEdges(ctx, nodeID, "")
	if err != nil {
		return nil, err
	}

	// Filter by relation types if specified
	for _, edge := range outgoing {
		if !isActiveEdge(&edge, opts.IncludeExpired) {
			continue
		}
		if len(opts.RelationTypes) > 0 && !contains(opts.RelationTypes, edge.RelationType) {
			continue
		}
		result.Edges = append(result.Edges, edge)
		node, err := s.GetNode(ctx, edge.TargetNodeID)
		if err != nil {
			continue
		}
		if !isActiveNode(node, opts.IncludeExpired) {
			continue
		}
		if len(opts.NodeTypes) > 0 && !contains(opts.NodeTypes, node.Type) {
			continue
		}
		result.Nodes = append(result.Nodes, *node)
	}

	// Include reverse edges if requested
	if opts.IncludeReverse {
		incoming, err := s.GetIncomingEdges(ctx, nodeID, "")
		if err != nil {
			return nil, err
		}
		for _, edge := range incoming {
			if !isActiveEdge(&edge, opts.IncludeExpired) {
				continue
			}
			if len(opts.RelationTypes) > 0 && !contains(opts.RelationTypes, edge.RelationType) {
				continue
			}
			result.Edges = append(result.Edges, edge)
			node, err := s.GetNode(ctx, edge.SourceNodeID)
			if err != nil {
				continue
			}
			if !isActiveNode(node, opts.IncludeExpired) {
				continue
			}
			if len(opts.NodeTypes) > 0 && !contains(opts.NodeTypes, node.Type) {
				continue
			}
			result.Nodes = append(result.Nodes, *node)
		}
	}

	return result, nil
}

// Traverse performs a breadth-first traversal from a starting node.
func (s *Service) Traverse(ctx context.Context, startNodeID string, opts TraversalOptions) (*QueryResult, error) {
	result := &QueryResult{
		Nodes: []Node{},
		Edges: []Edge{},
	}

	visited := make(map[string]bool)
	edgesSeen := make(map[string]bool)
	queue := []struct {
		nodeID string
		depth  int
	}{{startNodeID, 0}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if visited[current.nodeID] {
			continue
		}
		visited[current.nodeID] = true

		node, err := s.GetNode(ctx, current.nodeID)
		if err != nil {
			continue
		}

		// Skip if node type doesn't match filter (except for start node)
		if current.depth > 0 && len(opts.NodeTypes) > 0 && !contains(opts.NodeTypes, node.Type) {
			continue
		}

		result.Nodes = append(result.Nodes, *node)

		if current.depth >= opts.MaxDepth {
			continue
		}

		// Get neighbors
		neighbors, err := s.GetNeighbors(ctx, current.nodeID, opts)
		if err != nil {
			continue
		}

		for _, edge := range neighbors.Edges {
			if !edgesSeen[edge.ID] {
				edgesSeen[edge.ID] = true
				result.Edges = append(result.Edges, edge)
			}
		}

		for _, neighbor := range neighbors.Nodes {
			if !visited[neighbor.ID] {
				queue = append(queue, struct {
					nodeID string
					depth  int
				}{neighbor.ID, current.depth + 1})
			}
		}
	}

	return result, nil
}

// FindPath finds the shortest path between two nodes using BFS.
func (s *Service) FindPath(ctx context.Context, startID, endID string, opts TraversalOptions) (*QueryResult, error) {
	if startID == endID {
		node, err := s.GetNode(ctx, startID)
		if err != nil {
			return nil, err
		}
		return &QueryResult{Nodes: []Node{*node}, Edges: []Edge{}}, nil
	}

	type pathState struct {
		nodeID string
		path   []string
		edges  []Edge
		depth  int
	}

	visited := make(map[string]bool)
	queue := []pathState{{startID, []string{startID}, []Edge{}, 0}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if visited[current.nodeID] {
			continue
		}
		visited[current.nodeID] = true

		if current.depth >= opts.MaxDepth {
			continue
		}

		neighbors, err := s.GetNeighbors(ctx, current.nodeID, opts)
		if err != nil {
			continue
		}

		for i, neighbor := range neighbors.Nodes {
			if neighbor.ID == endID {
				// Found the target
				newPath := append([]string{}, current.path...)
				newPath = append(newPath, neighbor.ID)
				newEdges := append([]Edge{}, current.edges...)
				newEdges = append(newEdges, neighbors.Edges[i])

				// Build result with all nodes in path
				result := &QueryResult{
					Nodes: make([]Node, 0, len(newPath)),
					Edges: newEdges,
				}
				for _, nodeID := range newPath {
					node, err := s.GetNode(ctx, nodeID)
					if err != nil {
						continue
					}
					result.Nodes = append(result.Nodes, *node)
				}
				return result, nil
			}

			if !visited[neighbor.ID] {
				newPath := append([]string{}, current.path...)
				newPath = append(newPath, neighbor.ID)
				newEdges := append([]Edge{}, current.edges...)
				if i < len(neighbors.Edges) {
					newEdges = append(newEdges, neighbors.Edges[i])
				}
				queue = append(queue, pathState{neighbor.ID, newPath, newEdges, current.depth + 1})
			}
		}
	}

	return nil, fmt.Errorf("no path found between %s and %s", startID, endID)
}

// convertNodes converts []any to []Node.
func convertNodes(results []any) []Node {
	nodes := make([]Node, 0, len(results))
	for _, r := range results {
		if node, ok := r.(*Node); ok {
			nodes = append(nodes, *node)
		}
	}
	return nodes
}

// convertEdges converts []any to []Edge.
func convertEdges(results []any) []Edge {
	edges := make([]Edge, 0, len(results))
	for _, r := range results {
		if edge, ok := r.(*Edge); ok {
			edges = append(edges, *edge)
		}
	}
	return edges
}

// contains checks if a slice contains a value.
func contains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

// SetEmbeddingService sets the embedding service for semantic search.
func (s *Service) SetEmbeddingService(es *EmbeddingService) {
	s.embeddingService = es
}

// semanticSearch finds nodes similar to a query using vector embeddings.
// Returns nodes ranked by semantic similarity.
func (s *Service) semanticSearch(ctx context.Context, query string, limit int) ([]scoredNode, error) {
	if s.embeddingService == nil {
		return nil, fmt.Errorf("embedding service not configured")
	}

	// Generate embedding for query
	queryEmbedding, err := s.embeddingService.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	// Get all nodes with embeddings
	allNodes, err := s.ListNodes(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	results := rankBySimilarity(queryEmbedding, allNodes, limit)
	if len(results) == 0 && hasAnyEmbeddings(allNodes) {
		// Existing vectors may be from an old/unsupported model; attempt one-time regeneration.
		if _, regenErr := s.regenerateEmbeddings(ctx); regenErr != nil {
			log.Printf("knowledge graph semantic re-embedding failed: %v", regenErr)
			return results, nil
		}

		refreshedNodes, listErr := s.ListNodes(ctx, "")
		if listErr != nil {
			log.Printf("knowledge graph reload after re-embedding failed: %v", listErr)
			return results, nil
		}
		results = rankBySimilarity(queryEmbedding, refreshedNodes, limit)
	}

	return results, nil
}

// findSimilarNodes finds nodes similar to a given node.
// Uses the node's embedding if available, otherwise generates one.
func (s *Service) findSimilarNodes(ctx context.Context, nodeID string, limit int) ([]scoredNode, error) {
	if s.embeddingService == nil {
		return nil, fmt.Errorf("embedding service not configured")
	}

	node, err := s.GetNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	embedding, err := embedNode(s.embeddingService, ctx, node)
	if err != nil {
		if len(node.Embedding) > 0 {
			embedding = node.Embedding
		} else {
			return nil, fmt.Errorf("failed to embed node: %w", err)
		}
	} else if !equalEmbeddings(node.Embedding, embedding) {
		node.Embedding = embedding
		node.UpdatedAt = time.Now()
		if updateErr := s.userDB().Table(Node{}).Update(ctx, node); updateErr != nil {
			log.Printf("knowledge graph source node re-embedding update failed for %s: %v", node.ID, updateErr)
		}
	}

	// Get all nodes with embeddings
	allNodes, err := s.ListNodes(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	// Filter out the source node
	filteredNodes := make([]Node, 0, len(allNodes)-1)
	for _, n := range allNodes {
		if n.ID != nodeID {
			filteredNodes = append(filteredNodes, n)
		}
	}

	results := rankBySimilarity(embedding, filteredNodes, limit)
	if len(results) == 0 && hasAnyEmbeddings(filteredNodes) {
		// Existing vectors may be from an old/unsupported model; attempt one-time regeneration.
		if _, regenErr := s.regenerateEmbeddings(ctx); regenErr != nil {
			log.Printf("knowledge graph similarity re-embedding failed: %v", regenErr)
			return results, nil
		}

		refreshedNodes, listErr := s.ListNodes(ctx, "")
		if listErr != nil {
			log.Printf("knowledge graph reload after re-embedding failed: %v", listErr)
			return results, nil
		}

		updatedFiltered := make([]Node, 0, len(refreshedNodes)-1)
		for _, n := range refreshedNodes {
			if n.ID != nodeID {
				updatedFiltered = append(updatedFiltered, n)
			}
		}
		results = rankBySimilarity(embedding, updatedFiltered, limit)
	}

	return results, nil
}

// regenerateEmbeddings refreshes embeddings for all nodes.
// Used internally to recover from vector-space drift (old/unsupported model).
func (s *Service) regenerateEmbeddings(ctx context.Context) (int, error) {
	if s.embeddingService == nil {
		return 0, fmt.Errorf("embedding service not configured")
	}

	allNodes, err := s.ListNodes(ctx, "")
	if err != nil {
		return 0, fmt.Errorf("failed to list nodes: %w", err)
	}

	count := 0
	for _, node := range allNodes {
		embedding, err := embedNode(s.embeddingService, ctx, &node)
		if err != nil {
			return count, fmt.Errorf("failed to embed node %s: %w", node.ID, err)
		}

		node.Embedding = embedding
		node.UpdatedAt = time.Now()
		if err := s.userDB().Table(Node{}).Update(ctx, &node); err != nil {
			return count, fmt.Errorf("failed to update node %s: %w", node.ID, err)
		}
		count++
	}

	return count, nil
}

func hasAnyEmbeddings(nodes []Node) bool {
	for _, node := range nodes {
		if len(node.Embedding) > 0 {
			return true
		}
	}
	return false
}

func equalEmbeddings(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// CheckConsistency verifies a proposed edge against existing graph state.
// Returns a ConsistencyResult indicating whether the edge can be created.
func (s *Service) CheckConsistency(ctx context.Context, edge *Edge) (*ConsistencyResult, error) {
	// Self-loop detection
	if edge.SourceNodeID == edge.TargetNodeID {
		return &ConsistencyResult{
			OK:         false,
			Issue:      "self_loop",
			Suggestion: "Edges cannot connect a node to itself. Use properties on the node instead.",
		}, nil
	}

	// Duplicate detection — same source, target, and relation type
	outgoing, err := s.GetOutgoingEdges(ctx, edge.SourceNodeID, edge.RelationType)
	if err != nil {
		return nil, fmt.Errorf("failed to check duplicates: %w", err)
	}
	for _, existing := range outgoing {
		if existing.TargetNodeID == edge.TargetNodeID {
			return &ConsistencyResult{
				OK:         false,
				Issue:      "duplicate",
				ConflictID: existing.ID,
				Suggestion: "An identical edge already exists. Use kg_update to modify it instead.",
			}, nil
		}
	}

	// Inverse contradiction detection
	if inverse, ok := Contradictions[edge.RelationType]; ok {
		reverseEdges, err := s.GetOutgoingEdges(ctx, edge.TargetNodeID, inverse)
		if err != nil {
			return nil, fmt.Errorf("failed to check contradictions: %w", err)
		}
		for _, existing := range reverseEdges {
			if existing.TargetNodeID == edge.SourceNodeID {
				return &ConsistencyResult{
					OK:         false,
					Issue:      "contradiction",
					ConflictID: existing.ID,
					Suggestion: fmt.Sprintf("Contradicts existing edge (relation: %s). Delete the old edge first if the new fact supersedes it.", inverse),
				}, nil
			}
		}
	}

	return &ConsistencyResult{OK: true}, nil
}
