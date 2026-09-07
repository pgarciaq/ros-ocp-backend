package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shared savings helper appends vmTerm last; these callers never
// reference it. The pop must verify the tail so a future helper
// reorder/removal fails loud instead of silently rebinding $N (#565).
func TestPopVMTermArg(t *testing.T) {
	tests := []struct {
		name    string
		args    []interface{}
		term    string
		want    []interface{}
		wantErr string
	}{
		// Matching tails pop, preserving order and length.
		{"short maps", []interface{}{"org", "cost", "short", "short_term"}, "short", []interface{}{"org", "cost", "short"}, ""},
		{"medium maps", []interface{}{"org", "cost", "medium", "medium_term"}, "medium", []interface{}{"org", "cost", "medium"}, ""},
		{"long maps", []interface{}{"org", "cost", "long", "long_term"}, "long", []interface{}{"org", "cost", "long"}, ""},
		{"unknown term defaults like the mapper", []interface{}{"org", "cost", "weird", "medium_term"}, "weird", []interface{}{"org", "cost", "weird"}, ""},
		// Anything else errors: blind-trim restoration would eat term here.
		{"term tail errors", []interface{}{"org", "cost", "short"}, "short", nil, "shared helper shape changed"},
		{"non-string tail errors", []interface{}{"org", []string{"uuid"}}, "short", nil, "shared helper shape changed"},
		{"empty args errors", nil, "short", nil, "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := popVMTermArg(tt.args, tt.term)
			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
