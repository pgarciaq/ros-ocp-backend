package api

import "testing"

func TestEscapeILIKE(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"plain", "plain"},
		{"50%", "50\\%"},
		{"a_b", "a\\_b"},
		{`a\b`, `a\\b`},
		{`100%_done\`, "100\\%\\_done\\\\"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := escapeILIKE(tt.in); got != tt.want {
				t.Fatalf("escapeILIKE(%q)=%q; want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBuildModeClause_EscapesILIKEWildcards(t *testing.T) {
	clause, err := buildModeClause("project", "namespace", FilterModeInclude, []string{"foo_bar"}, 64, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for key, vals := range clause {
		if key != "namespace ILIKE ? ESCAPE '\\'" {
			t.Fatalf("unexpected SQL key %q", key)
		}
		arr, ok := vals.([]string)
		if !ok || len(arr) != 1 || arr[0] != "%foo\\_bar%" {
			t.Fatalf("unexpected param values: %#v", vals)
		}
	}
}
