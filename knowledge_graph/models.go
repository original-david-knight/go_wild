// Package gowild_knowledge_graph provides a knowledge graph implementation
// with persistent storage support via the gowild_data layer.
package gowild_knowledge_graph

import (
	"time"
)

// Node represents an entity in the knowledge graph.
// Nodes can have arbitrary properties stored as JSON.
type Node struct {
	ID         string         `json:"id"`
	UserID     string         `json:"user_id"`
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Notes      string         `json:"notes,omitempty"`    // Free-form text about this node — store observations, context, summaries, or any relevant information
	Properties map[string]any `json:"properties"`
	Embedding  []float32      `json:"embedding,omitempty"` // Vector embedding for semantic search
	Status     string         `json:"status,omitempty"`      // "active" (default), "expired", "invalid"
	LastUsedAt *time.Time     `json:"last_used_at,omitempty"` // Updated on traversal/query hits
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// Edge represents a directed relationship between two nodes.
// Edges have a type and can carry additional properties.
// Temporal properties (ValidFrom, ConfidenceScore) allow tracking time-bound truth.
type Edge struct {
	ID           string         `json:"id"`
	UserID       string         `json:"user_id"`
	SourceNodeID string         `json:"source_node_id"`
	TargetNodeID string         `json:"target_node_id"`
	RelationType string         `json:"relation_type"`
	Properties   map[string]any `json:"properties"`
	Weight       float64        `json:"weight"`
	// ValidFrom indicates when this relationship became valid/known.
	// Use this to track time-bound facts (e.g., "Shipyard predicts Strike, Date: Feb 2").
	ValidFrom *time.Time `json:"valid_from,omitempty"`
	// ConfidenceScore represents the certainty of this relationship (0.0 to 1.0).
	// Use this for predictions, estimates, or uncertain relationships.
	ConfidenceScore *float64   `json:"confidence_score,omitempty"`
	Source          string     `json:"source,omitempty"`        // URL or document the fact was extracted from
	ExtractedBy     string     `json:"extracted_by,omitempty"`  // Tool or agent that created this fact
	ExtractedAt     *time.Time `json:"extracted_at,omitempty"`  // When the fact was extracted from source
	ValidUntil      *time.Time `json:"valid_until,omitempty"`   // When this fact expires
	Status          string     `json:"status,omitempty"`        // "active" (default), "expired", "invalid"
	VerifiedAt      *time.Time `json:"verified_at,omitempty"`   // Last re-verification timestamp
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// NodeType constants for common entity types.
const (
	NodeTypeConcept      = "concept"
	NodeTypeEntity       = "entity"
	NodeTypePerson       = "person"
	NodeTypeOrganization = "organization"
	NodeTypeEvent        = "event"
	NodeTypeLocation     = "location"
	NodeTypeDocument     = "document"
)

// Status constants for nodes and edges.
const (
	StatusActive  = "active"
	StatusExpired = "expired"
	StatusInvalid = "invalid"
)

// Contradictions maps relation types to their logical inverses.
var Contradictions = map[string]string{
	"is_a":        "is_not_a",
	"is_not_a":    "is_a",
	"supports":    "contradicts",
	"contradicts": "supports",
	"causes":      "prevents",
	"prevents":    "causes",
	"enables":     "disables",
	"disables":    "enables",
	"agrees_with": "disagrees_with",
	"disagrees_with": "agrees_with",
}

// ConsistencyResult reports issues found during edge creation.
type ConsistencyResult struct {
	OK         bool   `json:"ok"`
	Issue      string `json:"issue,omitempty"`       // "duplicate", "contradiction", "self_loop"
	ConflictID string `json:"conflict_id,omitempty"` // ID of the conflicting edge
	Suggestion string `json:"suggestion,omitempty"`  // What the agent should do
}

// RelationType constants for common relationship types.
const (
	RelationTypeRelatedTo   = "related_to"
	RelationTypePartOf      = "part_of"
	RelationTypeHasA        = "has_a"
	RelationTypeIsA         = "is_a"
	RelationTypeDependsOn   = "depends_on"
	RelationTypeCreatedBy   = "created_by"
	RelationTypeOwnedBy     = "owned_by"
	RelationTypeReferences  = "references"
	RelationTypePrecedes    = "precedes"
	RelationTypeFollows     = "follows"
	RelationTypeSimilarTo   = "similar_to"
	RelationTypeContradicts = "contradicts"
)

// QueryResult holds the results of a graph query.
type QueryResult struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// TraversalOptions configures graph traversal behavior.
type TraversalOptions struct {
	MaxDepth       int      `json:"max_depth"`
	RelationTypes  []string `json:"relation_types"`
	NodeTypes      []string `json:"node_types"`
	IncludeReverse bool     `json:"include_reverse"`
	IncludeExpired bool     `json:"include_expired"` // If false (default), filter out expired/invalid items
}
