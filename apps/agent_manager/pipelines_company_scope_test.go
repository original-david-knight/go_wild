package main

import (
	"context"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

func TestPipelineCompanyScopeDispatchesToInCompanyAgent(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)

	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethod(ctx, "run_test", "test", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}

	inAgent, err := svc.CreateAgent(ctx, "in-company")
	if err != nil {
		t.Fatalf("create in-company agent failed: %v", err)
	}
	inDataSvc := data.NewAgentService(db, inAgent.ID)
	if err := inDataSvc.RegisterCapability(ctx, "tester", "run_test"); err != nil {
		t.Fatalf("register in-company capability failed: %v", err)
	}

	outAgent, err := svc.CreateAgent(ctx, "out-company")
	if err != nil {
		t.Fatalf("create out-company agent failed: %v", err)
	}
	outDataSvc := data.NewAgentService(db, outAgent.ID)
	if err := outDataSvc.RegisterCapability(ctx, "tester", "run_test"); err != nil {
		t.Fatalf("register out-company capability failed: %v", err)
	}

	company, err := svc.CreateCompany(ctx, "acme", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := svc.AddAgentToCompany(ctx, company.ID, inAgent.ID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}

	engine := NewPipelineEngine(db, svc)
	engine.SetHeartbeatSender(&stubHeartbeatSender{})
	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:             "company_scope_pipeline",
		Name:           "Company Scope Pipeline",
		ScopeMode:      "company",
		ScopeCompanyID: company.ID,
		Steps: []PipelineStep{
			{
				OnMethod:   "seed_method",
				ToRole:     "tester",
				NextMethod: "run_test",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	_, runID, err := engine.TriggerPipeline(ctx, "company_scope_pipeline", map[string]any{"ok": true})
	if err != nil {
		t.Fatalf("TriggerPipeline failed: %v", err)
	}
	if runID == "" {
		t.Fatalf("expected run id")
	}
	engine.WaitInFlight()

	stepRuns, err := db.Table(data.PipelineStepRun{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"run_id": runID},
	})
	if err != nil {
		t.Fatalf("query step runs failed: %v", err)
	}
	if len(stepRuns) != 1 {
		t.Fatalf("expected 1 step run, got %d", len(stepRuns))
	}

	stepRun := stepRuns[0].(*data.PipelineStepRun)
	job, err := newLocalA2AQueue(db).GetJob(ctx, stepRun.A2AJobID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	gotTo, _ := job["to_public_key"].(string)
	if gotTo == "" {
		t.Fatalf("expected to_public_key to be set")
	}
	if gotTo != inAgent.ID {
		t.Fatalf("expected in-company target %q, got %q", inAgent.ID, gotTo)
	}
	if gotTo == outAgent.ID {
		t.Fatalf("expected out-company agent %q not to be selected", outAgent.ID)
	}
}

func TestPipelineCompanyScopeFailsWhenNoEligibleInCompanyAgent(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)

	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethod(ctx, "run_test", "test", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}

	outAgent, err := svc.CreateAgent(ctx, "only-out-company")
	if err != nil {
		t.Fatalf("create out-company agent failed: %v", err)
	}
	outDataSvc := data.NewAgentService(db, outAgent.ID)
	if err := outDataSvc.RegisterCapability(ctx, "tester", "run_test"); err != nil {
		t.Fatalf("register out-company capability failed: %v", err)
	}

	company, err := svc.CreateCompany(ctx, "empty-co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	engine := NewPipelineEngine(db, svc)
	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:             "company_scope_no_target",
		Name:           "Company Scope No Target",
		ScopeMode:      "company",
		ScopeCompanyID: company.ID,
		Steps: []PipelineStep{
			{
				OnMethod:   "seed_method",
				ToRole:     "tester",
				NextMethod: "run_test",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	_, runID, err := engine.TriggerPipeline(ctx, "company_scope_no_target", map[string]any{"ok": true})
	if err != nil {
		t.Fatalf("TriggerPipeline failed: %v", err)
	}
	if runID == "" {
		t.Fatalf("expected run id")
	}
	engine.WaitInFlight()

	systemDataSvc := data.NewAgentService(db, "system")
	run, err := systemDataSvc.GetPipelineRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetPipelineRun failed: %v", err)
	}
	if run.Status != "failed" {
		t.Fatalf("expected failed run status, got %q", run.Status)
	}
	if run.ScopeMode != "company" {
		t.Fatalf("expected run scope_mode company, got %q", run.ScopeMode)
	}
	if run.ScopeCompanyID != company.ID {
		t.Fatalf("expected run scope_company_id %q, got %q", company.ID, run.ScopeCompanyID)
	}
	if run.FailureReason != companyScopeNoEligibleAgentsFailureReason {
		t.Fatalf("expected failure reason %q, got %q", companyScopeNoEligibleAgentsFailureReason, run.FailureReason)
	}

	stepRuns, err := db.Table(data.PipelineStepRun{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"run_id": runID},
	})
	if err != nil {
		t.Fatalf("query step runs failed: %v", err)
	}
	if len(stepRuns) != 0 {
		t.Fatalf("expected no step runs when no eligible in-company agent, got %d", len(stepRuns))
	}
}

func TestPipelineCompanyScopeRejectsOutOfCompanyClaudeCodeTarget(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)

	outAgent, err := svc.CreateAgent(ctx, "claude-out-company")
	if err != nil {
		t.Fatalf("create out-company agent failed: %v", err)
	}

	company, err := svc.CreateCompany(ctx, "claude-scope", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	engine := NewPipelineEngine(db, svc)
	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:             "company_scope_claude_target",
		Name:           "Company Scope Claude Target",
		ScopeMode:      "company",
		ScopeCompanyID: company.ID,
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed_method",
				ToAgentID:  outAgent.ID,
				NextMethod: "analyze_markets",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	_, runID, err := engine.TriggerPipeline(ctx, "company_scope_claude_target", map[string]any{"ok": true})
	if err != nil {
		t.Fatalf("TriggerPipeline failed: %v", err)
	}
	engine.WaitInFlight()

	systemDataSvc := data.NewAgentService(db, "system")
	run, err := systemDataSvc.GetPipelineRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetPipelineRun failed: %v", err)
	}
	if run.Status != "failed" {
		t.Fatalf("expected failed run status, got %q", run.Status)
	}
	if run.FailureReason != companyScopeNoEligibleAgentsFailureReason {
		t.Fatalf("expected failure reason %q, got %q", companyScopeNoEligibleAgentsFailureReason, run.FailureReason)
	}

	stepRuns, err := db.Table(data.PipelineStepRun{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"run_id": runID},
	})
	if err != nil {
		t.Fatalf("query step runs failed: %v", err)
	}
	if len(stepRuns) != 1 {
		t.Fatalf("expected 1 failed step run, got %d", len(stepRuns))
	}
	if got := stepRuns[0].(*data.PipelineStepRun).Status; got != "failed" {
		t.Fatalf("expected failed step run, got %q", got)
	}
}
