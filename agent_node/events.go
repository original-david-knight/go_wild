package agentnode

// GraphEvent is the marker interface for all graph execution events.
type GraphEvent interface {
	graphEventMarker()
}

// NodeStartEvent is emitted when a node begins execution.
type NodeStartEvent struct {
	NodeID NodeID
}

func (NodeStartEvent) graphEventMarker() {}

// NodeDoneEvent is emitted when a node completes successfully.
type NodeDoneEvent struct {
	NodeID NodeID
	Result *NodeResult
}

func (NodeDoneEvent) graphEventMarker() {}

// NodeFailedEvent is emitted when a node fails during execution.
type NodeFailedEvent struct {
	NodeID     NodeID
	Error      string
	FullPrompt string
}

func (NodeFailedEvent) graphEventMarker() {}

// NodeSkippedEvent is emitted when a node is skipped due to a failed dependency.
type NodeSkippedEvent struct {
	NodeID NodeID
	Reason string
}

func (NodeSkippedEvent) graphEventMarker() {}

// PlanEvent is emitted when the planner produces a new graph.
type PlanEvent struct {
	Round int
	Graph *NodeGraph
}

func (PlanEvent) graphEventMarker() {}

// SufficiencyCheckEvent is emitted when the sufficiency checker runs.
type SufficiencyCheckEvent struct {
	Round      int
	Sufficient bool
	Reasoning  string
}

func (SufficiencyCheckEvent) graphEventMarker() {}

// DeepResearchProgressEvent bridges progress from the deep research engine.
type DeepResearchProgressEvent struct {
	NodeID       NodeID
	Stage        string // run_start, round_start, planned_query, search_start, source, round_complete, run_complete, warning
	Round        int
	Query        string
	URL          string
	ObjectiveKey string
	Warning      string
}

func (DeepResearchProgressEvent) graphEventMarker() {}
