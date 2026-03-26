package model

import "testing"

func TestTermsToCleanup_ThreeToTwo(t *testing.T) {
	oldTermCount := 3
	newTermCount := 2
	removed := TermsToCleanup(oldTermCount, newTermCount)
	if len(removed) != 1 {
		t.Fatalf("expected 1 removed term, got %d", len(removed))
	}
	if removed[0] != "long_term" {
		t.Errorf("expected 'long_term' removed, got %s", removed[0])
	}
}

func TestTermsToCleanup_ThreeToOne(t *testing.T) {
	removed := TermsToCleanup(3, 1)
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed terms, got %d", len(removed))
	}
	expected := map[string]bool{"medium_term": true, "long_term": true}
	for _, r := range removed {
		if !expected[r] {
			t.Errorf("unexpected removed term: %s", r)
		}
	}
}

func TestTermsToCleanup_AddingTerms(t *testing.T) {
	removed := TermsToCleanup(2, 3)
	if len(removed) != 0 {
		t.Errorf("adding terms should not remove anything, got %v", removed)
	}
}

func TestTermsToCleanup_SameCount(t *testing.T) {
	removed := TermsToCleanup(3, 3)
	if len(removed) != 0 {
		t.Errorf("same count should not remove anything, got %v", removed)
	}
}
