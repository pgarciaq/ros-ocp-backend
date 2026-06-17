package api

import "testing"

func TestRequestIncludesExplanation(t *testing.T) {
	tests := []struct {
		param string
		want  bool
	}{
		{"", false},
		{"explanation", true},
		{" explanation ", true},
		{"explanation,savings_detail", true},
		{"savings_detail", false},
		{"unknown", false},
	}
	for _, tc := range tests {
		if got := RequestIncludesExplanation(tc.param); got != tc.want {
			t.Errorf("RequestIncludesExplanation(%q) = %v, want %v", tc.param, got, tc.want)
		}
	}
}
