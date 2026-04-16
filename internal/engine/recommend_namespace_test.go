package engine

import (
	"testing"
)

func TestEvaluateNamespaceNotifications_NewWorkload(t *testing.T) {
	rec := NamespaceRec{
		DataDays:        0,
		ConfidenceLevel: 0.0,
	}
	codes := EvaluateNamespaceNotifications(rec)

	found := false
	for _, c := range codes {
		if c == NotifNewWorkload {
			found = true
		}
	}
	if !found {
		t.Errorf("expected NotifNewWorkload (%d) in codes %v", NotifNewWorkload, codes)
	}
}

func TestEvaluateNamespaceNotifications_LowConfidence(t *testing.T) {
	rec := NamespaceRec{
		DataDays:        3,
		ConfidenceLevel: 0.3,
	}
	codes := EvaluateNamespaceNotifications(rec)

	found := false
	for _, c := range codes {
		if c == NotifLowConfidence {
			found = true
		}
	}
	if !found {
		t.Errorf("expected NotifLowConfidence (%d) in codes %v", NotifLowConfidence, codes)
	}
}

func TestEvaluateNamespaceNotifications_NoNotifications(t *testing.T) {
	rec := NamespaceRec{
		DataDays:        10,
		ConfidenceLevel: 0.8,
	}
	codes := EvaluateNamespaceNotifications(rec)

	if len(codes) != 0 {
		t.Errorf("expected no notification codes, got %v", codes)
	}
}

func TestEvaluateNamespaceNotifications_NoOOMOrIdle(t *testing.T) {
	// Even with zero days and low confidence, only NewWorkload should appear
	// (not OOM or idle, which are container-only).
	rec := NamespaceRec{
		DataDays:        0,
		ConfidenceLevel: 0.0,
	}
	codes := EvaluateNamespaceNotifications(rec)

	for _, c := range codes {
		if c == NotifOOMDetected {
			t.Error("namespace notifications should never include OOM")
		}
		if c == NotifIdleWorkload {
			t.Error("namespace notifications should never include idle workload")
		}
	}
}

func TestEvaluateNamespaceNotifications_BothNewAndLowConfidence(t *testing.T) {
	// DataDays=0 triggers NewWorkload, but LowConfidence requires DataDays>0.
	rec := NamespaceRec{
		DataDays:        0,
		ConfidenceLevel: 0.2,
	}
	codes := EvaluateNamespaceNotifications(rec)

	hasNew := false
	hasLow := false
	for _, c := range codes {
		if c == NotifNewWorkload {
			hasNew = true
		}
		if c == NotifLowConfidence {
			hasLow = true
		}
	}
	if !hasNew {
		t.Error("expected NotifNewWorkload for DataDays=0")
	}
	if hasLow {
		t.Error("LowConfidence should not fire when DataDays=0")
	}
}

func TestNamespaceRec_VariationFields(t *testing.T) {
	rec := NamespaceRec{
		OrgID:                  "org1",
		ClusterUUID:            "cluster-1",
		Namespace:              "default",
		Term:                   "short",
		Engine:                 "cost",
		RecCPURequestMC:        200,
		RecCPULimitMC:          400,
		RecMemRequestKiB:       4096,
		RecMemLimitKiB:         8192,
		CurrentCPURequestMC:    100,
		CurrentCPULimitMC:      200,
		CurrentMemRequestKiB:   2048,
		CurrentMemLimitKiB:     4096,
		VariationCPURequestPct: 100.0,
		VariationMemRequestPct: 100.0,
		ConfidenceLevel:        0.9,
		DataDays:               14,
		Stale:                  false,
	}

	// Verify struct fields are set correctly.
	if rec.Namespace != "default" {
		t.Errorf("expected namespace default, got %s", rec.Namespace)
	}
	if rec.VariationCPURequestPct != 100.0 {
		t.Errorf("expected variation 100.0, got %f", rec.VariationCPURequestPct)
	}
}
