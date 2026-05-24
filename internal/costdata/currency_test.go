package costdata

import "testing"

func TestResolveCurrency(t *testing.T) {
	t.Parallel()
	if got := ResolveCurrency(nil); got != DefaultCurrency {
		t.Fatalf("ResolveCurrency(nil) = %q, want %q", got, DefaultCurrency)
	}
	if got := ResolveCurrency(&ClusterCostData{}); got != DefaultCurrency {
		t.Fatalf("ResolveCurrency(empty) = %q, want %q", got, DefaultCurrency)
	}
	if got := ResolveCurrency(&ClusterCostData{Currency: "EUR"}); got != "EUR" {
		t.Fatalf("ResolveCurrency(EUR) = %q, want EUR", got)
	}
}
