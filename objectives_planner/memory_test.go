package objectives_planner

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMemoryStore_KnowledgeCRUD(t *testing.T) {
	db := setupTestDB(t)
	mem := NewMemoryStore(db)
	ctx := context.Background()

	// Add knowledge
	entry := &KnowledgeEntry{
		ObjectiveID: "obj-1",
		Fact:        "The API rate limit is 100 requests per minute",
		Source:      "execution",
		Tags:        []string{"api", "rate-limit"},
		Confidence:  0.95,
	}
	if err := mem.addKnowledge(ctx, entry); err != nil {
		t.Fatalf("add knowledge: %v", err)
	}
	if entry.ID == "" {
		t.Fatal("expected ID to be set")
	}
	if entry.DiscoveredAt.IsZero() {
		t.Fatal("expected DiscoveredAt to be set")
	}

	// Add another
	entry2 := &KnowledgeEntry{
		ObjectiveID: "obj-1",
		Fact:        "Competitor pricing is $9.99/month",
		Source:      "research",
		Tags:        []string{"pricing", "competitor"},
		Confidence:  0.8,
	}
	if err := mem.addKnowledge(ctx, entry2); err != nil {
		t.Fatalf("add knowledge 2: %v", err)
	}

	// Get all (no tag filter)
	all, err := mem.getRelevantKnowledge(ctx, nil, 10)
	if err != nil {
		t.Fatalf("get all knowledge: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}
}

func TestMemoryStore_KnowledgeTagFilter(t *testing.T) {
	db := setupTestDB(t)
	mem := NewMemoryStore(db)
	ctx := context.Background()

	mem.addKnowledge(ctx, &KnowledgeEntry{
		Fact: "API fact", Tags: []string{"api", "technical"}, Confidence: 0.9,
	})
	mem.addKnowledge(ctx, &KnowledgeEntry{
		Fact: "Pricing fact", Tags: []string{"pricing", "business"}, Confidence: 0.8,
	})
	mem.addKnowledge(ctx, &KnowledgeEntry{
		Fact: "Another API fact", Tags: []string{"api", "integration"}, Confidence: 0.7,
	})

	// Filter by "api" tag
	apiKnowledge, err := mem.getRelevantKnowledge(ctx, []string{"api"}, 10)
	if err != nil {
		t.Fatalf("get api knowledge: %v", err)
	}
	if len(apiKnowledge) != 2 {
		t.Fatalf("expected 2 api entries, got %d", len(apiKnowledge))
	}

	// Filter by "pricing" tag
	pricingKnowledge, err := mem.getRelevantKnowledge(ctx, []string{"pricing"}, 10)
	if err != nil {
		t.Fatalf("get pricing knowledge: %v", err)
	}
	if len(pricingKnowledge) != 1 {
		t.Fatalf("expected 1 pricing entry, got %d", len(pricingKnowledge))
	}

	// Filter by multiple tags (union)
	combined, err := mem.getRelevantKnowledge(ctx, []string{"api", "business"}, 10)
	if err != nil {
		t.Fatalf("get combined knowledge: %v", err)
	}
	if len(combined) != 3 {
		t.Fatalf("expected 3 combined entries, got %d", len(combined))
	}

	// Case-insensitive tag matching
	upper, err := mem.getRelevantKnowledge(ctx, []string{"API"}, 10)
	if err != nil {
		t.Fatalf("get uppercase knowledge: %v", err)
	}
	if len(upper) != 2 {
		t.Fatalf("expected 2 entries with case-insensitive match, got %d", len(upper))
	}
}

func TestMemoryStore_KnowledgeExpiration(t *testing.T) {
	db := setupTestDB(t)
	mem := NewMemoryStore(db)
	ctx := context.Background()

	// Add an expired entry
	mem.addKnowledge(ctx, &KnowledgeEntry{
		Fact:       "Old fact",
		Tags:       []string{"old"},
		Confidence: 0.5,
		ExpiresAt:  time.Now().UTC().Add(-24 * time.Hour),
	})

	// Add a valid entry
	mem.addKnowledge(ctx, &KnowledgeEntry{
		Fact:       "Fresh fact",
		Tags:       []string{"new"},
		Confidence: 0.9,
		ExpiresAt:  time.Now().UTC().Add(24 * time.Hour),
	})

	// Add an entry with no expiry
	mem.addKnowledge(ctx, &KnowledgeEntry{
		Fact:       "Permanent fact",
		Tags:       []string{"permanent"},
		Confidence: 1.0,
	})

	// getRelevantKnowledge should exclude expired
	all, err := mem.getRelevantKnowledge(ctx, nil, 10)
	if err != nil {
		t.Fatalf("get knowledge: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 non-expired entries, got %d", len(all))
	}

	// expireStaleKnowledge should remove the old one
	count, err := mem.expireStaleKnowledge(ctx)
	if err != nil {
		t.Fatalf("expire stale: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 expired, got %d", count)
	}

	// Verify it's gone
	remaining, _ := mem.getRelevantKnowledge(ctx, nil, 10)
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining, got %d", len(remaining))
	}
}

func TestMemoryStore_Decisions(t *testing.T) {
	db := setupTestDB(t)
	mem := NewMemoryStore(db)
	ctx := context.Background()

	// Add decisions for different objectives
	mem.addDecision(ctx, &DecisionEntry{
		ObjectiveID: "obj-1",
		Decision:    "Use REST API instead of GraphQL",
		Reasoning:   "Simpler integration",
		Outcome:     "Success",
	})
	mem.addDecision(ctx, &DecisionEntry{
		ObjectiveID: "obj-1",
		Decision:    "Cache responses for 5 minutes",
		Reasoning:   "Reduce API calls",
		Outcome:     "Reduced load by 60%",
	})
	mem.addDecision(ctx, &DecisionEntry{
		ObjectiveID: "obj-2",
		Decision:    "Different objective decision",
		Reasoning:   "Other reason",
	})

	// Get decisions for obj-1
	decisions, err := mem.getRecentDecisions(ctx, "obj-1", 10)
	if err != nil {
		t.Fatalf("get decisions: %v", err)
	}
	if len(decisions) != 2 {
		t.Fatalf("expected 2 decisions for obj-1, got %d", len(decisions))
	}

	// Verify auto-set fields
	for _, d := range decisions {
		if d.ID == "" {
			t.Error("expected ID to be set")
		}
		if d.CreatedAt.IsZero() {
			t.Error("expected CreatedAt to be set")
		}
	}

	// Get decisions for obj-2
	obj2Decisions, err := mem.getRecentDecisions(ctx, "obj-2", 10)
	if err != nil {
		t.Fatalf("get obj-2 decisions: %v", err)
	}
	if len(obj2Decisions) != 1 {
		t.Fatalf("expected 1 decision for obj-2, got %d", len(obj2Decisions))
	}
}

func TestMemoryStore_Learnings(t *testing.T) {
	db := setupTestDB(t)
	mem := NewMemoryStore(db)
	ctx := context.Background()

	mem.addLearning(ctx, &LearningEntry{
		Learning:     "API calls should include retry logic",
		Evidence:     []string{"task-1 failed due to timeout", "task-3 succeeded after retry"},
		Confidence:   0.85,
		ApplicableTo: []string{"api", "integration"},
	})
	mem.addLearning(ctx, &LearningEntry{
		Learning:     "Product descriptions should be under 500 chars",
		Evidence:     []string{"listing-1 rejected for length"},
		Confidence:   0.9,
		ApplicableTo: []string{"content", "product"},
	})

	// Verify auto-set fields
	all, err := mem.getApplicableLearnings(ctx, nil, 10)
	if err != nil {
		t.Fatalf("get all learnings: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 learnings, got %d", len(all))
	}
	for _, l := range all {
		if l.ID == "" {
			t.Error("expected ID to be set")
		}
		if l.CreatedAt.IsZero() {
			t.Error("expected CreatedAt to be set")
		}
	}

	// Filter by tag
	apiLearnings, err := mem.getApplicableLearnings(ctx, []string{"api"}, 10)
	if err != nil {
		t.Fatalf("get api learnings: %v", err)
	}
	if len(apiLearnings) != 1 {
		t.Fatalf("expected 1 api learning, got %d", len(apiLearnings))
	}
	if apiLearnings[0].Learning != "API calls should include retry logic" {
		t.Fatalf("unexpected learning: %s", apiLearnings[0].Learning)
	}
}

func TestMemoryStore_FormatMemoryContext(t *testing.T) {
	db := setupTestDB(t)
	mem := NewMemoryStore(db)
	ctx := context.Background()

	objID := "test-obj-1"

	// Add some memory
	mem.addDecision(ctx, &DecisionEntry{
		ObjectiveID: objID,
		Decision:    "Use REST API",
		Reasoning:   "Simpler",
		Outcome:     "Worked well",
	})
	mem.addKnowledge(ctx, &KnowledgeEntry{
		ObjectiveID: objID,
		Fact:        "Rate limit is 100/min",
		Tags:        []string{"api"},
		Confidence:  0.9,
	})
	mem.addLearning(ctx, &LearningEntry{
		Learning:     "Always add retries",
		Confidence:   0.85,
		ApplicableTo: []string{"api"},
	})

	context_, err := mem.FormatMemoryContext(ctx, objID)
	if err != nil {
		t.Fatalf("format memory context: %v", err)
	}

	if !strings.Contains(context_, "Recent Decisions") {
		t.Error("expected memory context to contain decisions section")
	}
	if !strings.Contains(context_, "Use REST API") {
		t.Error("expected memory context to contain decision text")
	}
	if !strings.Contains(context_, "Known Facts") {
		t.Error("expected memory context to contain knowledge section")
	}
	if !strings.Contains(context_, "Rate limit is 100/min") {
		t.Error("expected memory context to contain fact text")
	}
	if !strings.Contains(context_, "Learned Patterns") {
		t.Error("expected memory context to contain learnings section")
	}
	if !strings.Contains(context_, "Always add retries") {
		t.Error("expected memory context to contain learning text")
	}
}

func TestMemoryStore_FormatMemoryContextEmpty(t *testing.T) {
	db := setupTestDB(t)
	mem := NewMemoryStore(db)
	ctx := context.Background()

	context_, err := mem.FormatMemoryContext(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("format empty memory context: %v", err)
	}

	if context_ != "" {
		t.Fatalf("expected empty context for no memory, got %q", context_)
	}
}

func TestMemoryStore_KnowledgeLimitRespected(t *testing.T) {
	db := setupTestDB(t)
	mem := NewMemoryStore(db)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		mem.addKnowledge(ctx, &KnowledgeEntry{
			Fact: "Fact", Tags: []string{"test"}, Confidence: 0.5,
		})
	}

	limited, err := mem.getRelevantKnowledge(ctx, []string{"test"}, 3)
	if err != nil {
		t.Fatalf("get limited: %v", err)
	}
	if len(limited) != 3 {
		t.Fatalf("expected 3 entries with limit, got %d", len(limited))
	}
}
