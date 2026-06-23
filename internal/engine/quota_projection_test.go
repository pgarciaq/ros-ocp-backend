package engine

import "testing"

func TestNeedsQuotaReprojection(t *testing.T) {
	if NeedsQuotaReprojection(DefaultQuotaContainerTerm(), DefaultQuotaContainerEngine()) {
		t.Fatal("expected default term/engine to skip reprojection")
	}
	if !NeedsQuotaReprojection("short", DefaultQuotaContainerEngine()) {
		t.Fatal("expected short term to require reprojection")
	}
	if !NeedsQuotaReprojection(DefaultQuotaContainerTerm(), "performance") {
		t.Fatal("expected performance engine to require reprojection")
	}
}
