package agentnode

import (
	"encoding/json"
	"fmt"
	"sync"
)

// NodeGraph is a directed acyclic graph of node definitions.
type NodeGraph struct {
	Nodes []NodeDef `json:"nodes"`
}

// Validate checks the graph for structural errors:
// duplicate IDs, missing dependency references, and cycles.
func (g *NodeGraph) Validate() error {
	ids := make(map[NodeID]struct{}, len(g.Nodes))

	// Check for duplicate IDs
	for _, n := range g.Nodes {
		if _, exists := ids[n.ID]; exists {
			return fmt.Errorf("duplicate node ID: %s", n.ID)
		}
		ids[n.ID] = struct{}{}
	}

	// Check for missing dependency references
	for _, n := range g.Nodes {
		for _, dep := range n.DependsOn {
			if _, exists := ids[dep]; !exists {
				return fmt.Errorf("node %s depends on unknown node %s", n.ID, dep)
			}
		}
	}

	// Check for cycles using DFS
	const (
		white = 0 // unvisited
		gray  = 1 // in current path
		black = 2 // fully processed
	)

	color := make(map[NodeID]int, len(g.Nodes))
	adj := make(map[NodeID][]NodeID, len(g.Nodes))
	for _, n := range g.Nodes {
		adj[n.ID] = n.DependsOn
	}

	var hasCycle func(id NodeID) bool
	hasCycle = func(id NodeID) bool {
		color[id] = gray
		for _, dep := range adj[id] {
			switch color[dep] {
			case gray:
				return true
			case white:
				if hasCycle(dep) {
					return true
				}
			}
		}
		color[id] = black
		return false
	}

	for _, n := range g.Nodes {
		if color[n.ID] == white {
			if hasCycle(n.ID) {
				return fmt.Errorf("cycle detected in graph")
			}
		}
	}

	return nil
}

// topologicalSort returns nodes in a valid execution order using Kahn's algorithm.
// Returns an error if the graph contains a cycle.
func (g *NodeGraph) topologicalSort() ([]NodeDef, error) {
	nodeMap := make(map[NodeID]*NodeDef, len(g.Nodes))
	inDegree := make(map[NodeID]int, len(g.Nodes))
	dependents := make(map[NodeID][]NodeID, len(g.Nodes))

	for i := range g.Nodes {
		n := &g.Nodes[i]
		nodeMap[n.ID] = n
		inDegree[n.ID] = len(n.DependsOn)
		for _, dep := range n.DependsOn {
			dependents[dep] = append(dependents[dep], n.ID)
		}
	}

	// Start with nodes that have no dependencies
	queue := make([]NodeID, 0)
	for _, n := range g.Nodes {
		if inDegree[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}

	sorted := make([]NodeDef, 0, len(g.Nodes))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		sorted = append(sorted, *nodeMap[id])

		for _, dep := range dependents[id] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(sorted) != len(g.Nodes) {
		return nil, fmt.Errorf("cycle detected in graph")
	}

	return sorted, nil
}

// leafNodeIDs returns the IDs of nodes that no other node depends on.
// These are the terminal/output nodes of the graph.
func (g *NodeGraph) leafNodeIDs() []NodeID {
	hasDependents := make(map[NodeID]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		for _, dep := range n.DependsOn {
			hasDependents[dep] = true
		}
	}
	var leaves []NodeID
	for _, n := range g.Nodes {
		if !hasDependents[n.ID] {
			leaves = append(leaves, n.ID)
		}
	}
	return leaves
}

// SharedState is a concurrency-safe store for node results.
type SharedState struct {
	mu      sync.RWMutex
	results map[NodeID]*NodeResult
}

// NewSharedState creates a new empty SharedState.
func NewSharedState() *SharedState {
	return &SharedState{
		results: make(map[NodeID]*NodeResult),
	}
}

// set stores a node result.
func (s *SharedState) set(id NodeID, result *NodeResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[id] = result
}

// get retrieves a node result, or nil if not found.
func (s *SharedState) get(id NodeID) *NodeResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.results[id]
}

// Snapshot returns a map of node IDs to their JSON output, suitable for
// passing to the planner or sufficiency checker.
func (s *SharedState) Snapshot() map[string]json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked(nil)
}

// snapshotOnly returns a snapshot filtered to only the given node IDs.
func (s *SharedState) snapshotOnly(ids []NodeID) map[string]json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	filter := make(map[NodeID]bool, len(ids))
	for _, id := range ids {
		filter[id] = true
	}
	return s.snapshotLocked(filter)
}

func (s *SharedState) snapshotLocked(filter map[NodeID]bool) map[string]json.RawMessage {
	snap := make(map[string]json.RawMessage, len(s.results))
	for id, r := range s.results {
		if filter != nil && !filter[id] {
			continue
		}
		if r.Status != NodeDone {
			continue
		}
		if len(r.Output) > 0 {
			snap[string(id)] = r.Output
		} else if r.Text != "" {
			b, _ := json.Marshal(r.Text)
			snap[string(id)] = b
		}
	}
	return snap
}
