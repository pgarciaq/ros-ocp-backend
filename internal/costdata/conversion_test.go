package costdata

import "testing"

func TestConvertCents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		cents int64
		rate  float64
		want  int64
	}{
		{"identity", 10000, 1.0, 10000},
		{"double", 10000, 2.0, 20000},
		{"USD to EUR", 10000, 0.92, 9200},
		{"round half up", 100, 0.005, 1},
		{"zero cents", 0, 1.5, 0},
		{"negative cents", -10000, 0.92, -9200},
		{"fractional result rounds", 333, 1.0 / 3.0, 111},
		{"large value", 999999999, 1.1, 1099999999},
		{"zero rate returns identity", 5000, 0.0, 5000},
		{"negative rate returns identity", 5000, -1.0, 5000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ConvertCents(tc.cents, tc.rate)
			if got != tc.want {
				t.Errorf("ConvertCents(%d, %f) = %d, want %d", tc.cents, tc.rate, got, tc.want)
			}
		})
	}
}
