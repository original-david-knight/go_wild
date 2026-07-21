package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
)

func TestCallCompanyAdminToolsUnknownToolIsUnhandled(t *testing.T) {
	h := NewBrokerToolsHandler(setupManagerTestDB(t))
	handled, result, err := h.callCompanyAdminTools(context.Background(), "agent-unknown", "not_a_company_admin_tool", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Fatalf("expected handled=false, got true with result=%#v", result)
	}
	if result != nil {
		t.Fatalf("expected nil result for unhandled tool, got %#v", result)
	}
}

func TestIsCompanyAdminToolRecognition(t *testing.T) {
	if !isCompanyAdminTool("company_admin_get_context") {
		t.Fatalf("expected company_admin_get_context to be recognized")
	}
	if !isCompanyAdminTool("send_company_heartbeat") {
		t.Fatalf("expected send_company_heartbeat alias to be recognized")
	}
	if isCompanyAdminTool("company_admin_not_real") {
		t.Fatalf("expected unknown company admin tool to be rejected")
	}
}

func TestBrokerCompanyAdminToolsRequireMembership(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)

	ctx := context.Background()
	agentID := "agent-no-company"
	agentSvc := data.NewAgentService(db, agentID)
	agent, err := agentSvc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	agent.SetEnabledTools([]string{"company_admin"})
	if err := agentSvc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	_, err = h.callTool(ctx, agentID, agentSvc, "company_admin_get_context", nil)
	if err == nil {
		t.Fatalf("expected membership error")
	}
	if !strings.Contains(err.Error(), "require company membership") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBrokerCompanyAdminMutationsRequireCEO(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()

	ceoID := "company-ceo"
	memberID := "company-member"
	newMemberID := "company-new-member"
	for _, agentID := range []string{ceoID, memberID, newMemberID} {
		agentSvc := data.NewAgentService(db, agentID)
		if _, err := agentSvc.EnsureAgent(ctx); err != nil {
			t.Fatalf("EnsureAgent(%s) failed: %v", agentID, err)
		}
	}

	company, err := data.CreateCompany(ctx, db, "acme", "", ceoID)
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, memberID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(member) failed: %v", err)
	}

	memberSvc := data.NewAgentService(db, memberID)
	memberAgent, err := memberSvc.GetAgent(ctx)
	if err != nil {
		t.Fatalf("GetAgent(member) failed: %v", err)
	}
	memberAgent.SetEnabledTools([]string{"company_admin"})
	if err := memberSvc.UpdateAgent(ctx, memberAgent); err != nil {
		t.Fatalf("UpdateAgent(member) failed: %v", err)
	}
	addInput, _ := json.Marshal(map[string]any{"agent_id": newMemberID, "role": "member"})
	_, err = h.callTool(ctx, memberID, memberSvc, "company_admin_add_member", addInput)
	if err == nil {
		t.Fatalf("expected ceo authorization error")
	}
	if !strings.Contains(err.Error(), "requires ceo role") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBrokerCompanyAdminCEOMutationsAllowed(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()

	ceoID := "company-ceo"
	memberID := "company-member"
	for _, agentID := range []string{ceoID, memberID} {
		agentSvc := data.NewAgentService(db, agentID)
		if _, err := agentSvc.EnsureAgent(ctx); err != nil {
			t.Fatalf("EnsureAgent(%s) failed: %v", agentID, err)
		}
	}

	company, err := data.CreateCompany(ctx, db, "acme", "", ceoID)
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	ceoSvc := data.NewAgentService(db, ceoID)

	updateName := "acme-updated"
	updateDesc := "updated description"
	updateInput, _ := json.Marshal(map[string]any{
		"name":        updateName,
		"description": updateDesc,
	})
	if _, err := h.callTool(ctx, ceoID, ceoSvc, "company_admin_update_company", updateInput); err != nil {
		t.Fatalf("company_admin_update_company failed: %v", err)
	}

	addInput, _ := json.Marshal(map[string]any{"agent_id": memberID, "role": "operator"})
	if _, err := h.callTool(ctx, ceoID, ceoSvc, "company_admin_add_member", addInput); err != nil {
		t.Fatalf("company_admin_add_member failed: %v", err)
	}

	updatedCompany, err := data.GetCompany(ctx, db, company.ID)
	if err != nil {
		t.Fatalf("GetCompany failed: %v", err)
	}
	if updatedCompany.Name != updateName {
		t.Fatalf("expected updated company name %q, got %q", updateName, updatedCompany.Name)
	}
	if updatedCompany.Description != updateDesc {
		t.Fatalf("expected updated company description %q, got %q", updateDesc, updatedCompany.Description)
	}

	member, err := data.GetCompanyMemberForAgent(ctx, db, memberID)
	if err != nil {
		t.Fatalf("GetCompanyMemberForAgent failed: %v", err)
	}
	if member == nil || member.CompanyID != company.ID {
		t.Fatalf("expected member in company %q, got %#v", company.ID, member)
	}
	if member.Role != "operator" {
		t.Fatalf("expected updated member role operator, got %q", member.Role)
	}
}

func TestBrokerCompanyAdminSendHeartbeatFanOutFiltersMembers(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()

	ceoID := "hb-ceo"
	analystID := "hb-analyst"
	engineerID := "hb-engineer"
	for _, agentID := range []string{ceoID, analystID, engineerID} {
		agentSvc := data.NewAgentService(db, agentID)
		if _, err := agentSvc.EnsureAgent(ctx); err != nil {
			t.Fatalf("EnsureAgent(%s) failed: %v", agentID, err)
		}
	}

	company, err := data.CreateCompany(ctx, db, "hb-co", "", ceoID)
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, analystID, "analyst"); err != nil {
		t.Fatalf("AddAgentToCompany(analyst) failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, engineerID, "engineer"); err != nil {
		t.Fatalf("AddAgentToCompany(engineer) failed: %v", err)
	}

	sent := make([]string, 0)
	h.sendHeartbeatFn = func(agentID, message string) error {
		if strings.TrimSpace(message) == "" {
			return fmt.Errorf("empty message")
		}
		sent = append(sent, agentID)
		return nil
	}

	ceoSvc := data.NewAgentService(db, ceoID)
	input, _ := json.Marshal(map[string]any{
		"message":       "sync now",
		"include_ceo":   false,
		"member_filter": "analyst",
	})
	result, err := h.callTool(ctx, ceoID, ceoSvc, "company_admin_send_heartbeat", input)
	if err != nil {
		t.Fatalf("company_admin_send_heartbeat failed: %v", err)
	}

	if len(sent) != 1 || sent[0] != analystID {
		t.Fatalf("expected heartbeat only to analyst %q, got %#v", analystID, sent)
	}

	resMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if got, _ := resMap["sent_count"].(int); got != 1 {
		if gotF, _ := resMap["sent_count"].(float64); int(gotF) != 1 {
			t.Fatalf("expected sent_count=1, got %#v", resMap["sent_count"])
		}
	}
}

func TestBrokerCompanyAdminSendHeartbeatCapturesFailures(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()

	ceoID := "hb2-ceo"
	memberOK := "hb2-ok"
	memberFail := "hb2-fail"
	for _, agentID := range []string{ceoID, memberOK, memberFail} {
		agentSvc := data.NewAgentService(db, agentID)
		if _, err := agentSvc.EnsureAgent(ctx); err != nil {
			t.Fatalf("EnsureAgent(%s) failed: %v", agentID, err)
		}
	}

	company, err := data.CreateCompany(ctx, db, "hb2-co", "", ceoID)
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, memberOK, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(ok) failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, memberFail, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(fail) failed: %v", err)
	}

	sent := make([]string, 0)
	h.sendHeartbeatFn = func(agentID, message string) error {
		if agentID == memberFail {
			return fmt.Errorf("session unavailable")
		}
		sent = append(sent, agentID)
		return nil
	}

	ceoSvc := data.NewAgentService(db, ceoID)
	input, _ := json.Marshal(map[string]any{
		"message": "sync all",
	})
	result, err := h.callTool(ctx, ceoID, ceoSvc, "company_admin_send_heartbeat", input)
	if err != nil {
		t.Fatalf("company_admin_send_heartbeat failed: %v", err)
	}

	sort.Strings(sent)
	expectedSent := []string{ceoID, memberOK}
	sort.Strings(expectedSent)
	if len(sent) != len(expectedSent) || sent[0] != expectedSent[0] || sent[1] != expectedSent[1] {
		t.Fatalf("expected sent agents %v, got %v", expectedSent, sent)
	}

	resMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	failedRaw, ok := resMap["failed"].(map[string]string)
	if !ok {
		failedAny, ok := resMap["failed"].(map[string]any)
		if !ok {
			t.Fatalf("expected failed map, got %T", resMap["failed"])
		}
		failedRaw = make(map[string]string, len(failedAny))
		for k, v := range failedAny {
			failedRaw[k] = fmt.Sprintf("%v", v)
		}
	}
	if _, ok := failedRaw[memberFail]; !ok {
		t.Fatalf("expected failed entry for %q, got %#v", memberFail, failedRaw)
	}
}

func TestBrokerSendCompanyHeartbeatAlias(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()

	ceoID := "hb3-ceo"
	memberID := "hb3-member"
	for _, agentID := range []string{ceoID, memberID} {
		agentSvc := data.NewAgentService(db, agentID)
		if _, err := agentSvc.EnsureAgent(ctx); err != nil {
			t.Fatalf("EnsureAgent(%s) failed: %v", agentID, err)
		}
	}

	company, err := data.CreateCompany(ctx, db, "hb3-co", "", ceoID)
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, memberID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(member) failed: %v", err)
	}

	sent := make([]string, 0, 1)
	h.sendHeartbeatFn = func(agentID, message string) error {
		sent = append(sent, agentID)
		return nil
	}

	ceoSvc := data.NewAgentService(db, ceoID)
	input, _ := json.Marshal(map[string]any{
		"company_id":    company.ID,
		"message":       "sync alias",
		"include_ceo":   false,
		"member_filter": "member",
	})
	result, err := h.callTool(ctx, ceoID, ceoSvc, "send_company_heartbeat", input)
	if err != nil {
		t.Fatalf("send_company_heartbeat failed: %v", err)
	}

	if len(sent) != 1 || sent[0] != memberID {
		t.Fatalf("expected alias heartbeat only to member %q, got %#v", memberID, sent)
	}

	resMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if got, _ := resMap["company_id"].(string); got != company.ID {
		t.Fatalf("expected company_id %q, got %q", company.ID, got)
	}
}

func TestBrokerSendCompanyHeartbeatAliasRejectsMismatchedCompany(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()

	ceoID := "hb4-ceo"
	agentSvc := data.NewAgentService(db, ceoID)
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	companyA, err := data.CreateCompany(ctx, db, "hb4-a", "", ceoID)
	if err != nil {
		t.Fatalf("CreateCompany(A) failed: %v", err)
	}
	companyB, err := data.CreateCompany(ctx, db, "hb4-b", "", "")
	if err != nil {
		t.Fatalf("CreateCompany(B) failed: %v", err)
	}

	input, _ := json.Marshal(map[string]any{
		"company_id": companyB.ID,
		"message":    "bad scope",
	})
	_, err = h.callTool(ctx, ceoID, agentSvc, "send_company_heartbeat", input)
	if err == nil {
		t.Fatalf("expected company mismatch error")
	}
	if !strings.Contains(err.Error(), "must match caller company") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Sanity check: matching company_id should succeed when heartbeat sender exists.
	h.sendHeartbeatFn = func(agentID, message string) error { return nil }
	okInput, _ := json.Marshal(map[string]any{
		"company_id": companyA.ID,
		"message":    "ok scope",
	})
	if _, err := h.callTool(ctx, ceoID, agentSvc, "send_company_heartbeat", okInput); err != nil {
		t.Fatalf("expected matching company_id to succeed, got %v", err)
	}
}
