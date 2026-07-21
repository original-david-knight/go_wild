package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlePeerGroupsMethodGuard(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodPut, "/api/peer-groups", nil)
	rec := httptest.NewRecorder()

	h.handlePeerGroups(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "method not allowed") {
		t.Fatalf("expected method not allowed error, got %s", rec.Body.String())
	}
}

func TestHandlePeerGroupUnknownAction(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodGet, "/api/peer-groups/group-1/not-real", nil)
	rec := httptest.NewRecorder()

	h.handlePeerGroup(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d body=%s", http.StatusNotFound, rec.Code, rec.Body.String())
	}
}

func TestHandlePeerGroupMissingID(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodDelete, "/api/peer-groups/", nil)
	rec := httptest.NewRecorder()

	h.handlePeerGroup(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d body=%s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestHandlePeerGroupMethodGuards(t *testing.T) {
	h := &Handlers{}

	groupReq := httptest.NewRequest(http.MethodGet, "/api/peer-groups/group-1", nil)
	groupRec := httptest.NewRecorder()
	h.handlePeerGroup(groupRec, groupReq)
	if groupRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected group route wrong-method status %d, got %d body=%s", http.StatusMethodNotAllowed, groupRec.Code, groupRec.Body.String())
	}

	membersReq := httptest.NewRequest(http.MethodGet, "/api/peer-groups/group-1/members", nil)
	membersRec := httptest.NewRecorder()
	h.handlePeerGroup(membersRec, membersReq)
	if membersRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected members route wrong-method status %d, got %d body=%s", http.StatusMethodNotAllowed, membersRec.Code, membersRec.Body.String())
	}

	memberReq := httptest.NewRequest(http.MethodPost, "/api/peer-groups/group-1/members/agent-1", nil)
	memberRec := httptest.NewRecorder()
	h.handlePeerGroup(memberRec, memberReq)
	if memberRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected member route wrong-method status %d, got %d body=%s", http.StatusMethodNotAllowed, memberRec.Code, memberRec.Body.String())
	}
}

func TestPeerGroupRouteRecognitionHelpers(t *testing.T) {
	if !isPeerGroupCollectionMethod(http.MethodGet) || !isPeerGroupCollectionMethod(http.MethodPost) {
		t.Fatalf("expected GET/POST to be recognized collection methods")
	}
	if isPeerGroupCollectionMethod(http.MethodPatch) {
		t.Fatalf("expected PATCH to be rejected collection method")
	}

	if !isPeerGroupMethod(http.MethodDelete) {
		t.Fatalf("expected DELETE to be recognized group method")
	}
	if isPeerGroupMethod(http.MethodGet) {
		t.Fatalf("expected GET to be rejected group method")
	}

	if !isPeerGroupMembersMethod(http.MethodPost) {
		t.Fatalf("expected POST to be recognized members method")
	}
	if isPeerGroupMembersMethod(http.MethodDelete) {
		t.Fatalf("expected DELETE to be rejected members method")
	}

	if !isPeerGroupMemberMethod(http.MethodDelete) {
		t.Fatalf("expected DELETE to be recognized member method")
	}
	if isPeerGroupMemberMethod(http.MethodPost) {
		t.Fatalf("expected POST to be rejected member method")
	}
}

func TestParsePeerGroupRoute(t *testing.T) {
	route, err := parsePeerGroupRoute("/api/peer-groups/group-1")
	if err != nil {
		t.Fatalf("expected route parse success, got %v", err)
	}
	if route.groupID != "group-1" || route.action != "" || route.agentID != "" {
		t.Fatalf("unexpected base route parse: %#v", route)
	}

	route, err = parsePeerGroupRoute("/api/peer-groups/group-1/members")
	if err != nil {
		t.Fatalf("expected members route parse success, got %v", err)
	}
	if route.groupID != "group-1" || route.action != "members" || route.agentID != "" {
		t.Fatalf("unexpected members route parse: %#v", route)
	}

	route, err = parsePeerGroupRoute("/api/peer-groups/group-1/members/agent-1/extra")
	if err != nil {
		t.Fatalf("expected member route parse success, got %v", err)
	}
	if route.groupID != "group-1" || route.action != "members" || route.agentID != "agent-1/extra" {
		t.Fatalf("unexpected member route parse: %#v", route)
	}

	if _, err := parsePeerGroupRoute("/api/peer-groups/"); err == nil {
		t.Fatalf("expected missing group id to fail")
	}
}
