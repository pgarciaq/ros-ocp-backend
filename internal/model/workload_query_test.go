package model

import "testing"

func TestGetWorkloadsByOrgID_ReturnsCorrectType(t *testing.T) {
	orgID := "1234567"
	query, err := BuildGetWorkloadsByOrgIDQuery(orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.OrgID != "1234567" {
		t.Errorf("expected org_id 1234567, got %s", query.OrgID)
	}
}

func TestGetWorkloadsByOrgID_EmptyOrgID(t *testing.T) {
	_, err := BuildGetWorkloadsByOrgIDQuery("")
	if err == nil {
		t.Error("empty org_id should return error")
	}
}
