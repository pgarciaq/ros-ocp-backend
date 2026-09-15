package gpu

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression test for #579: GPU classification writes must lock rows in a
// deterministic order. StoreGPUClassifications used to queue writes in Go map
// order, so concurrent stores locked recommendation_sets rows in different
// sequences and deadlocked (SQLSTATE 40P01, observed warn-only under a
// parallel ingest race).
func TestSortGPUClassificationWrites_DeterministicAndSorted(t *testing.T) {
	writes := make([]gpuClassificationWrite, 0, 200)
	for i := 0; i < 200; i++ {
		writes = append(writes, gpuClassificationWrite{
			namespace:     fmt.Sprintf("ns-%03d", i%11),
			workload:      fmt.Sprintf("wl-%03d", i%7),
			containerName: fmt.Sprintf("c-%03d", i),
			term:          []string{"short_term", "medium_term", "long_term"}[i%3],
		})
	}

	less := func(a, b gpuClassificationWrite) int {
		if a.namespace != b.namespace {
			return compareStr(a.namespace, b.namespace)
		}
		if a.workload != b.workload {
			return compareStr(a.workload, b.workload)
		}
		if a.containerName != b.containerName {
			return compareStr(a.containerName, b.containerName)
		}
		return compareStr(a.term, b.term)
	}

	first := append([]gpuClassificationWrite(nil), writes...)
	sortGPUClassificationWrites(first)
	require.Len(t, first, 200)
	assert.True(t, slices.IsSortedFunc(first, less), "writes must come out in sorted order")

	// Repeat: identical output every time proves map order cannot leak through
	// (Go randomizes iteration, so 100 identical runs over 200 rows cannot
	// pass by luck).
	for i := 0; i < 100; i++ {
		got := append([]gpuClassificationWrite(nil), writes...)
		sortGPUClassificationWrites(got)
		assert.Equal(t, first, got, "identical output on every call (iteration %d)", i)
	}
}

func compareStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
