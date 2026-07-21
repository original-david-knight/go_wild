package main

import (
	"context"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
)

func TestCloneAgent(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	// Create source agent with config
	source, err := svc.CreateAgent(ctx, "poly-researcher")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	source.ModelProvider = data.LLMProviderOpenAI
	source.OpenAIAuthMode = data.OpenAIAuthModeCodexOAuth
	source.Model = "gemini-2.5-pro"
	source.SmartModel = "gemini-2.5-pro"
	source.SmartDefault = true
	source.MaxTurns = 20
	source.Heartbeat = "10m"
	source.SystemPrompt = "You are a researcher."
	source.MemoryLimit = "512m"
	source.CPULimit = "1.0"
	source.SetEnabledTools([]string{"shell", "python", "browser"})
	source.SetEnvVars(map[string]string{"API_KEY": "test123"})
	if err := svc.UpdateAgent(ctx, source); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	// Register a capability on source
	srcSvc := data.NewAgentService(db, "poly-researcher")
	srcSvc.CreateA2AMethod(ctx, "research_market", "Research a market", "", "")
	if err := srcSvc.RegisterCapability(ctx, "researcher", "research_market"); err != nil {
		t.Fatalf("RegisterCapability failed: %v", err)
	}

	// Create a company and add source to it
	company, err := svc.CreateCompany(ctx, "Test Corp", "Test company", "poly-researcher")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	// Clone
	clone, err := svc.CloneAgent(ctx, "poly-researcher", "poly-researcher-2")
	if err != nil {
		t.Fatalf("CloneAgent failed: %v", err)
	}

	// Verify config was copied
	if clone.ModelProvider != data.LLMProviderOpenAI {
		t.Errorf("expected ModelProvider %s, got %q", data.LLMProviderOpenAI, clone.ModelProvider)
	}
	if clone.OpenAIAuthMode != data.OpenAIAuthModeCodexOAuth {
		t.Errorf("expected OpenAIAuthMode %s, got %q", data.OpenAIAuthModeCodexOAuth, clone.OpenAIAuthMode)
	}
	if clone.Model != "gemini-2.5-pro" {
		t.Errorf("expected Model gemini-2.5-pro, got %q", clone.Model)
	}
	if clone.SmartModel != "gemini-2.5-pro" {
		t.Errorf("expected SmartModel gemini-2.5-pro, got %q", clone.SmartModel)
	}
	if !clone.SmartDefault {
		t.Error("expected SmartDefault true")
	}
	if clone.MaxTurns != 20 {
		t.Errorf("expected MaxTurns 20, got %d", clone.MaxTurns)
	}
	if clone.Heartbeat != "10m" {
		t.Errorf("expected Heartbeat 10m, got %q", clone.Heartbeat)
	}
	if clone.SystemPrompt != "You are a researcher." {
		t.Errorf("expected SystemPrompt copied, got %q", clone.SystemPrompt)
	}
	if clone.MemoryLimit != "512m" {
		t.Errorf("expected MemoryLimit 512m, got %q", clone.MemoryLimit)
	}

	// Verify env vars copied
	env := clone.EnvVars()
	if env["API_KEY"] != "test123" {
		t.Errorf("expected env var API_KEY=test123, got %q", env["API_KEY"])
	}

	// Verify enabled tools copied
	tools := clone.EnabledTools()
	if !tools["shell"] || !tools["python"] || !tools["browser"] {
		t.Errorf("expected enabled tools copied, got %v", tools)
	}

	// Verify wallet seed phrase is different (auto-generated)
	if clone.WalletSeedPhrase == source.WalletSeedPhrase {
		t.Error("expected clone to have different wallet seed phrase")
	}
	if clone.WalletSeedPhrase == "" {
		t.Error("expected clone to have a wallet seed phrase")
	}

	// Verify capabilities were copied
	cloneSvc := data.NewAgentService(db, "poly-researcher-2")
	caps, err := cloneSvc.GetCapabilities(ctx)
	if err != nil {
		t.Fatalf("GetCapabilities failed: %v", err)
	}
	if len(caps) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(caps))
	}
	if caps[0].Role != "researcher" || caps[0].Method != "research_market" {
		t.Errorf("unexpected capability: %s/%s", caps[0].Role, caps[0].Method)
	}

	// Verify company membership was copied
	member, err := data.GetCompanyMemberForAgent(ctx, db, "poly-researcher-2")
	if err != nil {
		t.Fatalf("GetCompanyMemberForAgent failed: %v", err)
	}
	if member == nil {
		t.Fatal("expected clone to be in company")
	}
	if member.CompanyID != company.ID {
		t.Errorf("expected company %s, got %s", company.ID, member.CompanyID)
	}

	// Verify duplicate clone fails
	_, err = svc.CloneAgent(ctx, "poly-researcher", "poly-researcher-2")
	if err == nil {
		t.Error("expected error cloning to existing agent ID")
	}

	// Verify clone from non-existent source fails
	_, err = svc.CloneAgent(ctx, "nonexistent", "new-agent")
	if err == nil {
		t.Error("expected error cloning from non-existent agent")
	}
}

func TestFindAllAgentsByCapability(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	// Create agents
	svc.CreateAgent(ctx, "agent-1")
	svc.CreateAgent(ctx, "agent-2")
	svc.CreateAgent(ctx, "agent-3")

	// Register a method and capabilities
	dataSvc := data.NewAgentService(db, "agent-1")
	dataSvc.CreateA2AMethod(ctx, "do_research", "Do research", "", "")
	dataSvc.RegisterCapability(ctx, "researcher", "do_research")

	dataSvc2 := data.NewAgentService(db, "agent-2")
	dataSvc2.RegisterCapability(ctx, "researcher", "do_research")

	// agent-3 has a different capability
	dataSvc3 := data.NewAgentService(db, "agent-3")
	dataSvc3.CreateA2AMethod(ctx, "analyze", "Analyze", "", "")
	dataSvc3.RegisterCapability(ctx, "analyst", "analyze")

	// FindAllAgentsByCapability should return both researchers
	agents, err := dataSvc.FindAllAgentsByCapability(ctx, "researcher", "do_research")
	if err != nil {
		t.Fatalf("FindAllAgentsByCapability failed: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}

	// FindAllAgentsByCapability for analysts should return 1
	agents, err = dataSvc.FindAllAgentsByCapability(ctx, "analyst", "analyze")
	if err != nil {
		t.Fatalf("FindAllAgentsByCapability failed: %v", err)
	}
	if len(agents) != 1 || agents[0] != "agent-3" {
		t.Errorf("expected [agent-3], got %v", agents)
	}

	// Non-existent capability should error
	_, err = dataSvc.FindAllAgentsByCapability(ctx, "unknown", "method")
	if err == nil {
		t.Error("expected error for non-existent capability")
	}
}

func TestFanOutPoolJobsWithHeartbeatSender(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	sender := &stubHeartbeatSender{}
	engine := &PipelineEngine{db: db, service: service, heartbeatSender: sender}

	// Create two agents with the same capability
	service.CreateAgent(ctx, "primary-agent")
	service.CreateAgent(ctx, "clone-agent")

	dataSvc := data.NewAgentService(db, "primary-agent")
	dataSvc.CreateA2AMethod(ctx, "do_work", "Do work", "", "")
	dataSvc.RegisterCapability(ctx, "worker", "do_work")

	dataSvc2 := data.NewAgentService(db, "clone-agent")
	dataSvc2.RegisterCapability(ctx, "worker", "do_work")

	// Create a running pipeline run
	systemSvc := data.NewAgentService(db, "system")
	run := &data.PipelineRun{
		ID:          "test-run-explicit",
		PipelineID:  "test-pipe",
		CurrentStep: 0,
		Status:      "running",
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	result := map[string]any{
		"items": []any{
			map[string]any{"id": "1"},
			map[string]any{"id": "2"},
			map[string]any{"id": "3"},
			map[string]any{"id": "4"},
		},
	}

	engine.executeFanOutStep(ctx, systemSvc, run, PipelineStep{
		OnMethod:   "seed",
		ToAgentID:  "primary-agent",
		ToRole:     "worker",
		NextMethod: "do_work",
		FanOut:     true,
		FanOutKey:  "items",
		ParamMap:   map[string]string{"$": "item"},
	}, 0, result, nil)

	// Verify 4 step runs created
	stepRuns, err := systemSvc.ListStepRunsForRun(ctx, "test-run-explicit")
	if err != nil {
		t.Fatalf("ListStepRunsForRun failed: %v", err)
	}
	if len(stepRuns) != 4 {
		t.Fatalf("expected 4 step runs, got %d", len(stepRuns))
	}

	// With a heartbeat sender, initial delivery claims 1 job per agent.
	// 2 agents = 2 claimed jobs, 2 still in pool (queued + unassigned).
	queue := engine.localQueueOrDefault()
	claimed := 0
	pooled := 0
	for _, sr := range stepRuns {
		job, err := queue.GetJob(ctx, sr.A2AJobID)
		if err != nil {
			t.Fatalf("GetJob failed: %v", err)
		}
		status, _ := job["status"].(string)
		toAgent, _ := job["to_public_key"].(string)
		if status == "claimed" && toAgent != "" {
			claimed++
		} else if status == "queued" && toAgent == "" {
			pooled++
		}
	}
	if claimed != 2 {
		t.Errorf("expected 2 claimed jobs (one per agent), got %d", claimed)
	}
	if pooled != 2 {
		t.Errorf("expected 2 pool jobs still queued, got %d", pooled)
	}

	// Verify heartbeats were sent to both agents
	if len(sender.calls) != 2 {
		t.Fatalf("expected 2 heartbeat calls (one per agent), got %d", len(sender.calls))
	}
	agents := map[string]bool{}
	for _, c := range sender.calls {
		agents[c.agentID] = true
	}
	if !agents["primary-agent"] || !agents["clone-agent"] {
		t.Errorf("expected heartbeats to both agents, got %v", agents)
	}
}

func TestFanOutPoolJobsUnassigned(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	// No heartbeat sender — jobs won't be delivered, staying as pool jobs.
	engine := &PipelineEngine{db: db, service: service}

	service.CreateAgent(ctx, "researcher-1")
	service.CreateAgent(ctx, "researcher-2")

	dataSvc := data.NewAgentService(db, "researcher-1")
	dataSvc.CreateA2AMethod(ctx, "do_research", "Do research", "", "")
	dataSvc.RegisterCapability(ctx, "researcher", "do_research")

	dataSvc2 := data.NewAgentService(db, "researcher-2")
	dataSvc2.RegisterCapability(ctx, "researcher", "do_research")

	systemSvc := data.NewAgentService(db, "system")
	run := &data.PipelineRun{
		ID:          "test-run",
		PipelineID:  "research_pipeline",
		CurrentStep: 0,
		Status:      "running",
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	result := map[string]any{
		"topics": []any{
			map[string]any{"name": "topic1"},
			map[string]any{"name": "topic2"},
			map[string]any{"name": "topic3"},
			map[string]any{"name": "topic4"},
		},
	}

	engine.executeFanOutStep(ctx, systemSvc, run, PipelineStep{
		OnMethod:   "find_topics",
		ToRole:     "researcher",
		NextMethod: "do_research",
		FanOut:     true,
		FanOutKey:  "topics",
		ParamMap:   map[string]string{"$": "topic"},
	}, 0, result, nil)

	// Verify 4 step runs created
	stepRuns, err := systemSvc.ListStepRunsForRun(ctx, "test-run")
	if err != nil {
		t.Fatalf("ListStepRunsForRun failed: %v", err)
	}
	if len(stepRuns) != 4 {
		t.Fatalf("expected 4 step runs, got %d", len(stepRuns))
	}

	// All jobs should be pool jobs (unassigned, queued) since no heartbeat sender.
	queue := engine.localQueueOrDefault()
	for _, sr := range stepRuns {
		job, err := queue.GetJob(ctx, sr.A2AJobID)
		if err != nil {
			t.Fatalf("GetJob failed: %v", err)
		}
		status, _ := job["status"].(string)
		toAgent, _ := job["to_public_key"].(string)
		if status != "queued" {
			t.Errorf("expected queued, got %q", status)
		}
		if toAgent != "" {
			t.Errorf("expected unassigned pool job, got to_agent=%q", toAgent)
		}
	}

	// Any agent can claim any pool job.
	job1, err := queue.ClaimJob(ctx, "researcher-1", stepRuns[0].A2AJobID, 300)
	if err != nil {
		t.Fatalf("ClaimJob by researcher-1 failed: %v", err)
	}
	if agent, _ := job1["to_public_key"].(string); agent != "researcher-1" {
		t.Errorf("expected pool job assigned to researcher-1 after claim, got %q", agent)
	}

	job2, err := queue.ClaimJob(ctx, "researcher-2", stepRuns[1].A2AJobID, 300)
	if err != nil {
		t.Fatalf("ClaimJob by researcher-2 failed: %v", err)
	}
	if agent, _ := job2["to_public_key"].(string); agent != "researcher-2" {
		t.Errorf("expected pool job assigned to researcher-2 after claim, got %q", agent)
	}
}

func TestPoolJobRequeueClearsAgent(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	queue := newLocalA2AQueue(db)

	// Submit a pool job (empty to_agent_id)
	jobResult, _, err := queue.Submit(ctx, "pipeline:test", "", "", localA2ARequest{
		Method: "test_method",
		Params: map[string]any{"key": "val"},
	})
	if err != nil {
		t.Fatalf("Submit pool job failed: %v", err)
	}
	jobID, _ := jobResult["job_id"].(string)

	// Claim it as agent-1
	_, err = queue.ClaimJob(ctx, "agent-1", jobID, 300)
	if err != nil {
		t.Fatalf("ClaimJob failed: %v", err)
	}

	// Requeue — pool job should have to_agent_id cleared
	if err := queue.RequeueClaimedJob(ctx, "agent-1", jobID); err != nil {
		t.Fatalf("RequeueClaimedJob failed: %v", err)
	}

	job, err := queue.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	if status, _ := job["status"].(string); status != "queued" {
		t.Errorf("expected queued after requeue, got %q", status)
	}
	if agent, _ := job["to_public_key"].(string); agent != "" {
		t.Errorf("expected empty to_agent after pool requeue, got %q", agent)
	}

	// Now agent-2 can claim the same job
	_, err = queue.ClaimJob(ctx, "agent-2", jobID, 300)
	if err != nil {
		t.Fatalf("ClaimJob by agent-2 after requeue failed: %v", err)
	}
	job, _ = queue.GetJob(ctx, jobID)
	if agent, _ := job["to_public_key"].(string); agent != "agent-2" {
		t.Errorf("expected agent-2 after re-claim, got %q", agent)
	}
}
