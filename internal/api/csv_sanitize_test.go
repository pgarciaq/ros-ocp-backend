package api

import (
	"testing"
)

func TestSanitizeCSVCell(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"safe value", "my-cluster", "my-cluster"},
		{"numeric", "123.45", "123.45"},
		{"uuid", "550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440000"},
		{"k8s namespace", "openshift-monitoring", "openshift-monitoring"},
		{"formula equals", "=CMD('calc')", "'=CMD('calc')"},
		{"formula plus", "+1+1", "'+1+1"},
		{"formula minus", "-1-1", "'-1-1"},
		{"formula at", "@SUM(A1:A10)", "'@SUM(A1:A10)"},
		{"formula tab", "\t=1+1", "'\t=1+1"},
		{"formula cr", "\r=1+1", "'\r=1+1"},
		{"minus in middle is safe", "my-cluster-alias", "my-cluster-alias"},
		{"plus in middle is safe", "cpu+memory", "cpu+memory"},
		{"equals in middle is safe", "key=value", "key=value"},
		{"DDE attack", "=DDE(\"cmd\",\"/C calc\",\"!A0\")", "'=DDE(\"cmd\",\"/C calc\",\"!A0\")"},
		{"hyperlink formula", "=HYPERLINK(\"http://evil.com\")", "'=HYPERLINK(\"http://evil.com\")"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeCSVCell(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeCSVCell(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeCSVRow(t *testing.T) {
	row := []string{"safe", "=formula", "+cmd", "normal", "-minus", "@at"}
	got := sanitizeCSVRow(row)
	want := []string{"safe", "'=formula", "'+cmd", "normal", "'-minus", "'@at"}
	if len(got) != len(want) {
		t.Fatalf("sanitizeCSVRow returned %d elements, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sanitizeCSVRow[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSanitizeCSVRow_MutatesInPlace(t *testing.T) {
	row := []string{"=dangerous", "safe"}
	result := sanitizeCSVRow(row)
	if result[0] != "'=dangerous" {
		t.Errorf("expected sanitized value, got %q", result[0])
	}
	if row[0] != "'=dangerous" {
		t.Errorf("expected in-place mutation, original slice not modified")
	}
}
