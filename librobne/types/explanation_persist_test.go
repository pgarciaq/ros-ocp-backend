package types

import (
	"strconv"
	"strings"
	"testing"
)

func TestContainerExplValuePlaceholders_KnownStartValues(t *testing.T) {
	t.Parallel()
	for _, start := range []int{18, 25, 31, 47} {
		t.Run("start="+strconv.Itoa(start), func(t *testing.T) {
			got := ContainerExplValuePlaceholders(start)

			// Verify against a freshly built reference.
			var parts []string
			for i := 0; i < containerExplColCount; i++ {
				parts = append(parts, "$"+strconv.Itoa(start+i))
			}
			want := strings.Join(parts, ",")

			if got != want {
				t.Errorf("ContainerExplValuePlaceholders(%d)\n got: %s\nwant: %s", start, got, want)
			}
		})
	}
}

func TestContainerExplValuePlaceholders_UnknownStart(t *testing.T) {
	t.Parallel()
	got := ContainerExplValuePlaceholders(100)
	if !strings.HasPrefix(got, "$100,") {
		t.Errorf("unexpected prefix: %s", got)
	}
	if count := strings.Count(got, ","); count != containerExplColCount-1 {
		t.Errorf("expected %d commas, got %d", containerExplColCount-1, count)
	}
}

func TestContainerExplValuePlaceholders_CacheHit(t *testing.T) {
	t.Parallel()
	a := ContainerExplValuePlaceholders(18)
	b := ContainerExplValuePlaceholders(18)
	if a != b {
		t.Errorf("cached results differ")
	}
}

func BenchmarkContainerExplValuePlaceholders_Cached(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ContainerExplValuePlaceholders(47)
	}
}

func BenchmarkContainerExplValuePlaceholders_Uncached(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ContainerExplValuePlaceholders(999)
	}
}
