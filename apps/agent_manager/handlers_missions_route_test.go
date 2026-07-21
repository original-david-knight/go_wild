package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupHandlersWithCompanyForMissions(t *testing.T) (*Handlers, string) {
	t.Helper()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	company, err := svc.CreateCompany(context.Background(), "Mission Route Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	return NewHandlers(svc, nil, nil, nil, nil), company.ID
}

func TestHandleMissionsCollectionMethodGuard(t *testing.T) {
	h, companyID := setupHandlersWithCompanyForMissions(t)

	req := httptest.NewRequest(http.MethodPut, "/api/companies/"+companyID+"/missions", nil)
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleMissionsUnknownAction(t *testing.T) {
	h, companyID := setupHandlersWithCompanyForMissions(t)

	req := httptest.NewRequest(http.MethodGet, "/api/companies/"+companyID+"/missions/mission-1/not-real", nil)
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleMissionsActionMethodGuard(t *testing.T) {
	h, companyID := setupHandlersWithCompanyForMissions(t)

	req := httptest.NewRequest(http.MethodGet, "/api/companies/"+companyID+"/missions/mission-1/pause", nil)
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleMissionsEscalationResolveWrongMethodNotFound(t *testing.T) {
	h, companyID := setupHandlersWithCompanyForMissions(t)

	req := httptest.NewRequest(http.MethodGet, "/api/companies/"+companyID+"/missions/escalations/esc-1/resolve", nil)
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestIsMissionActionRecognition(t *testing.T) {
	if !isMissionAction("pause") {
		t.Fatalf("expected pause action to be recognized")
	}
	if !isMissionAction("tree") {
		t.Fatalf("expected tree action to be recognized")
	}
	if isMissionAction("not-real") {
		t.Fatalf("expected unknown mission action to be rejected")
	}
}
