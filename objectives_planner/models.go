package objectives_planner

import "time"

// KnowledgeEntry stores a fact discovered during execution.
type KnowledgeEntry struct {
	ID           string    `json:"id"`
	ObjectiveID  string    `json:"objective_id"`
	Fact         string    `json:"fact"`
	Source       string    `json:"source"`
	Tags         []string  `json:"tags"`
	Confidence   float64   `json:"confidence"`
	DiscoveredAt time.Time `json:"discovered_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// DecisionEntry records a decision made during execution.
type DecisionEntry struct {
	ID          string    `json:"id"`
	ObjectiveID string    `json:"objective_id"`
	Decision    string    `json:"decision"`
	Reasoning   string    `json:"reasoning"`
	ActionTaken string    `json:"action_taken"`
	Outcome     string    `json:"outcome"`
	CreatedAt   time.Time `json:"created_at"`
}

// LearningEntry stores a pattern synthesized from multiple observations.
type LearningEntry struct {
	ID           string    `json:"id"`
	Learning     string    `json:"learning"`
	Evidence     []string  `json:"evidence"`
	Confidence   float64   `json:"confidence"`
	ApplicableTo []string  `json:"applicable_to"`
	CreatedAt    time.Time `json:"created_at"`
}

// ClarifyingQuestion is a question the planner needs answered before it can proceed.
type ClarifyingQuestion struct {
	Question string `json:"question"`
	Context  string `json:"context"`
}

// PlanOutput is the structured result from the strategic planner.
type PlanOutput struct {
	Reasoning           string               `json:"reasoning"`
	Mutations           []TreeMutation       `json:"mutations"`
	ClarifyingQuestions []ClarifyingQuestion `json:"clarifying_questions"`
}
