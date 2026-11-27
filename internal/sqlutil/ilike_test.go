package sqlutil

import "testing"

func TestEscapeILIKE(t *testing.T) {
	cases := map[string]string{
		"plain":      "plain",
		"100%":       `100\%`,
		"a_b":        `a\_b`,
		`back\slash`: `back\\slash`,
		"%_%":        `\%\_\%`,
	}
	for in, want := range cases {
		if got := EscapeILIKE(in); got != want {
			t.Errorf("EscapeILIKE(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContainsPattern(t *testing.T) {
	if got, want := ContainsPattern("a_b"), `%a\_b%`; got != want {
		t.Errorf("ContainsPattern = %q, want %q", got, want)
	}
}

// TestStarPattern pins the Koku-style wildcard contract (#957): `*` is the
// only wildcard the caller may use, and `%`, `_`, `\` in their literal text
// must match themselves.
func TestStarPattern(t *testing.T) {
	cases := map[string]string{
		"*":       "%",
		"*_":      `%\_`,
		"foo*":    "foo%",
		"*foo*":   "%foo%",
		"a%b_c*":  `a\%b\_c%`,
		"no-star": "no-star",
		`a\*b`:    `a\\%b`,
	}
	for in, want := range cases {
		if got := StarPattern(in); got != want {
			t.Errorf("StarPattern(%q) = %q, want %q", in, got, want)
		}
	}
}
