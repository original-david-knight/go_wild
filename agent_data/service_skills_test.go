package data

import (
	"context"
	"testing"
)

func TestSkillOperations(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	// No skills initially
	skills, err := svc.GetAllSkills(ctx)
	if err != nil {
		t.Fatalf("GetAllSkills failed: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}

	// Save skill (create)
	skill := &Skill{
		Name:        "greet",
		Description: "Greet a user",
		Code:        "print('hello')",
	}
	isUpdate, err := svc.SaveSkill(ctx, skill)
	if err != nil {
		t.Fatalf("SaveSkill failed: %v", err)
	}
	if isUpdate {
		t.Error("expected create, not update")
	}

	// Get skill by name
	got, err := svc.GetSkill(ctx, "greet")
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}
	if got.Code != "print('hello')" {
		t.Errorf("unexpected code: %q", got.Code)
	}

	// Update skill
	skill.Code = "print('hi!')"
	isUpdate, err = svc.SaveSkill(ctx, skill)
	if err != nil {
		t.Fatalf("SaveSkill update failed: %v", err)
	}
	if !isUpdate {
		t.Error("expected update, not create")
	}

	got, _ = svc.GetSkill(ctx, "greet")
	if got.Code != "print('hi!')" {
		t.Errorf("unexpected updated code: %q", got.Code)
	}

	// List skills
	skills, _ = svc.GetAllSkills(ctx)
	if len(skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(skills))
	}

	// Delete skill
	if err := svc.DeleteSkill(ctx, "greet"); err != nil {
		t.Fatalf("DeleteSkill failed: %v", err)
	}

	got, _ = svc.GetSkill(ctx, "greet")
	if got != nil {
		t.Error("expected skill to be deleted")
	}

	// Delete non-existent skill
	err = svc.DeleteSkill(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error deleting non-existent skill")
	}
}
