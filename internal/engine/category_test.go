package engine

import "testing"

func TestClassifyResource(t *testing.T) {
	tests := []struct {
		name     string
		pct      int32
		expected string
	}{
		{"strongly undersized", 50, CategoryUndersized},
		{"at undersized boundary", 11, CategoryUndersized},
		{"exactly at +10 threshold", 10, CategoryOptimized},
		{"slightly positive", 5, CategoryOptimized},
		{"zero variation", 0, CategoryOptimized},
		{"slightly negative", -5, CategoryOptimized},
		{"exactly at -10 threshold", -10, CategoryOptimized},
		{"at oversized boundary", -11, CategoryOversized},
		{"strongly oversized", -50, CategoryOversized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyResource(tt.pct)
			if got != tt.expected {
				t.Errorf("ClassifyResource(%d) = %q, want %q", tt.pct, got, tt.expected)
			}
		})
	}
}

func TestClassifyOverall(t *testing.T) {
	tests := []struct {
		name        string
		cpu         string
		memory      string
		expected    string
	}{
		{"both optimized", CategoryOptimized, CategoryOptimized, CategoryOptimized},
		{"both undersized", CategoryUndersized, CategoryUndersized, CategoryUndersized},
		{"both oversized", CategoryOversized, CategoryOversized, CategoryOversized},
		{"cpu undersized memory optimized", CategoryUndersized, CategoryOptimized, CategoryUndersized},
		{"cpu optimized memory undersized", CategoryOptimized, CategoryUndersized, CategoryUndersized},
		{"cpu undersized memory oversized - undersized wins", CategoryUndersized, CategoryOversized, CategoryUndersized},
		{"cpu oversized memory undersized - undersized wins", CategoryOversized, CategoryUndersized, CategoryUndersized},
		{"cpu oversized memory optimized", CategoryOversized, CategoryOptimized, CategoryOversized},
		{"cpu optimized memory oversized", CategoryOptimized, CategoryOversized, CategoryOversized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyOverall(tt.cpu, tt.memory)
			if got != tt.expected {
				t.Errorf("ClassifyOverall(%q, %q) = %q, want %q", tt.cpu, tt.memory, got, tt.expected)
			}
		})
	}
}

func TestNullIfEmpty(t *testing.T) {
	if got := nullIfEmpty(""); got != nil {
		t.Errorf("nullIfEmpty(\"\") = %v, want nil", got)
	}
	if got := nullIfEmpty("undersized"); got == nil || *got != "undersized" {
		t.Errorf("nullIfEmpty(\"undersized\") = %v, want pointer to \"undersized\"", got)
	}
}
