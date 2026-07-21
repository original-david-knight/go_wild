package data

import (
	"context"
	"testing"
)

func TestListAgents(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Create agents
	svc1 := NewAgentService(db, "agent-1")
	svc1.EnsureAgent(ctx)

	svc2 := NewAgentService(db, "agent-2")
	svc2.EnsureAgent(ctx)

	// Give agent-2 a soul
	svc2.SaveSoul(ctx, "I have a soul")

	agents, err := ListAgents(ctx, db)
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}

	// Check soul status
	for _, a := range agents {
		if a.ID == "agent-2" && !a.HasSoul {
			t.Error("agent-2 should have soul")
		}
		if a.ID == "agent-1" && a.HasSoul {
			t.Error("agent-1 should not have soul")
		}
	}
}

func TestDeleteAgent(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "delete-me")
	ctx := context.Background()

	// Create agent with associated data
	svc.EnsureAgent(ctx)
	svc.SaveMemory(ctx, "some memory")
	svc.SaveSoul(ctx, "some soul")
	svc.AddArchiveEntry(ctx, "summary", "tags", "content")
	svc.AddTask(ctx, "a task", "end")
	svc.AddRecurringTask(ctx, "recurring", 60)
	svc.SaveHistorySnapshot(ctx, "[]")
	svc.SaveChatMessage(ctx, "user", "hello")
	svc.AddPendingEmail(ctx, &PendingEmail{Type: "send", Recipients: "a@b.com", Subject: "test"})
	svc.SaveSkill(ctx, &Skill{Name: "test-skill", Code: "pass"})

	// Delete agent
	if err := svc.DeleteAgent(ctx); err != nil {
		t.Fatalf("DeleteAgent failed: %v", err)
	}

	// Verify everything is cleaned up
	_, err := svc.GetAgent(ctx)
	if err == nil {
		t.Error("expected error getting deleted agent")
	}

	mem, _ := svc.GetMemory(ctx)
	if mem != nil {
		t.Error("expected nil memory after delete")
	}

	soul, _ := svc.GetSoul(ctx)
	if soul != nil {
		t.Error("expected nil soul after delete")
	}
}
