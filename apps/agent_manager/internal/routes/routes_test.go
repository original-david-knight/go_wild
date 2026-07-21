package routes

import "testing"

func TestParseAgent(t *testing.T) {
	route, err := ParseAgent("/api/agents/agent-1/mcp-servers/server-a/test")
	if err != nil {
		t.Fatalf("ParseAgent() unexpected error: %v", err)
	}
	if route.AgentID != "agent-1" || route.Action != "mcp-servers" || route.ServerID != "server-a" || route.ServerAction != "test" {
		t.Fatalf("unexpected route: %+v", route)
	}

	if _, err := ParseAgent("/api/agents/"); err == nil {
		t.Fatalf("expected missing agent id to fail")
	}
}

func TestParseCompany(t *testing.T) {
	route, err := ParseCompany("/api/companies/company-1/missions/tasks/123")
	if err != nil {
		t.Fatalf("ParseCompany() unexpected error: %v", err)
	}
	if route.CompanyID != "company-1" || route.Action != "missions" {
		t.Fatalf("unexpected route: %+v", route)
	}
	if len(route.Parts) != 4 || route.Parts[3] != "123" {
		t.Fatalf("unexpected parts: %#v", route.Parts)
	}
}

func TestParsePeerGroup(t *testing.T) {
	route, err := ParsePeerGroup("/api/peer-groups/group-1/members/agent-1/extra")
	if err != nil {
		t.Fatalf("ParsePeerGroup() unexpected error: %v", err)
	}
	if route.GroupID != "group-1" || route.Action != "members" || route.AgentID != "agent-1/extra" {
		t.Fatalf("unexpected route: %+v", route)
	}
}

func TestParsePipeline(t *testing.T) {
	route, err := ParsePipeline("/api/pipelines/pipeline-a/actions/trigger-polymarket")
	if err != nil {
		t.Fatalf("ParsePipeline() unexpected error: %v", err)
	}
	if route.Kind != PipelineRouteAction || route.PipelineID != "pipeline-a" || route.Action != "trigger-polymarket" {
		t.Fatalf("unexpected route: %+v", route)
	}

	if _, err := ParsePipeline("/api/pipelines/   "); err == nil {
		t.Fatalf("expected missing pipeline id to fail")
	}
}
