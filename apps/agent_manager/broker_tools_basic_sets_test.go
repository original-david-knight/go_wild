package main

import (
	"context"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func TestCallReportToolsUnknownToolIsUnhandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	if _, err := NewAgentService(db).CreateAgent(context.Background(), "report-agent"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	svc := data.NewAgentService(db, "report-agent")

	handled, result, err := h.callReportTools(context.Background(), svc, "not_a_report_tool", nil)
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

func TestCallReportToolsGetReportHandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	if _, err := NewAgentService(db).CreateAgent(context.Background(), "report-agent"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	svc := data.NewAgentService(db, "report-agent")

	setHandled, _, setErr := h.callReportTools(context.Background(), svc, "set_report_html", []byte(`{"html":"<p>hello</p>"}`))
	if setErr != nil {
		t.Fatalf("unexpected set_report_html error: %v", setErr)
	}
	if !setHandled {
		t.Fatalf("expected set_report_html to be handled")
	}

	handled, result, err := h.callReportTools(context.Background(), svc, "get_report_html", []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatalf("expected get_report_html to be handled")
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if resultMap["exists"] != true {
		t.Fatalf("expected exists=true, got %#v", resultMap["exists"])
	}
	if resultMap["html"] != "<p>hello</p>" {
		t.Fatalf("expected saved html, got %#v", resultMap["html"])
	}
}

func TestIsReportToolRecognition(t *testing.T) {
	if !isReportTool("set_report_html") {
		t.Fatalf("expected set_report_html to be recognized")
	}
	if isReportTool("report_not_real") {
		t.Fatalf("expected unknown report tool to be rejected")
	}
}

func TestCallSoulToolsUnknownToolIsUnhandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	if _, err := NewAgentService(db).CreateAgent(context.Background(), "soul-agent"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	svc := data.NewAgentService(db, "soul-agent")

	handled, result, err := h.callSoulTools(context.Background(), svc, "not_a_soul_tool", nil)
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

func TestCallSoulToolsReadSoulHandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	if _, err := NewAgentService(db).CreateAgent(context.Background(), "soul-agent"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	svc := data.NewAgentService(db, "soul-agent")

	handled, result, err := h.callSoulTools(context.Background(), svc, "read_soul", []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatalf("expected read_soul to be handled")
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if resultMap["exists"] != false {
		t.Fatalf("expected exists=false, got %#v", resultMap["exists"])
	}
}

func TestIsSoulToolRecognition(t *testing.T) {
	if !isSoulTool("read_soul") {
		t.Fatalf("expected read_soul to be recognized")
	}
	if isSoulTool("soul_not_real") {
		t.Fatalf("expected unknown soul tool to be rejected")
	}
}

func TestCallSkillToolsUnknownToolIsUnhandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	if _, err := NewAgentService(db).CreateAgent(context.Background(), "skill-agent"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	svc := data.NewAgentService(db, "skill-agent")

	handled, result, err := h.callSkillTools(context.Background(), svc, "not_a_skill_tool", nil)
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

func TestCallSkillToolsListSkillsHandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	if _, err := NewAgentService(db).CreateAgent(context.Background(), "skill-agent"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	svc := data.NewAgentService(db, "skill-agent")

	handled, result, err := h.callSkillTools(context.Background(), svc, "list_skills", []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatalf("expected list_skills to be handled")
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if resultMap["count"] != 0 {
		t.Fatalf("expected count=0, got %#v", resultMap["count"])
	}
}

func TestIsSkillToolRecognition(t *testing.T) {
	if !isSkillTool("save_skill") {
		t.Fatalf("expected save_skill to be recognized")
	}
	if isSkillTool("skill_not_real") {
		t.Fatalf("expected unknown skill tool to be rejected")
	}
}
