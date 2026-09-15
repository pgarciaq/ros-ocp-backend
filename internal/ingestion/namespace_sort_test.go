package ingestion

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression test for #578: namespace digest upserts must lock rows in a
// deterministic order. upsertNamespaceDigests used to range over a Go map
// (random order per flush), so concurrent same-cluster manifests locked rows
// in different sequences and deadlocked (SQLSTATE 40P01, observed under a
// 7-way parallel ingest race; solo replay clean).
//
// Go randomizes map iteration, so 100 identical outputs over a 200-key map
// cannot pass by luck — only a sorted order does.
func TestSortedNamespaceDigestKeys_DeterministicAndSorted(t *testing.T) {
	grouped := make(map[NamespaceDigestKey][]NamespaceMetricRow, 200)
	for i := 0; i < 200; i++ {
		sched := ScheduleTypeAllHours
		if i%3 == 0 {
			sched = ScheduleTypeBusinessHours
		}
		grouped[NamespaceDigestKey{
			OrgID:        fmt.Sprintf("org-%03d", i%7),
			ClusterUUID:  fmt.Sprintf("cluster-%03d", i%5),
			Namespace:    fmt.Sprintf("ns-%03d", i),
			BucketDate:   time.Date(2026, 8, 1+(i%28), 0, 0, 0, 0, time.UTC),
			ScheduleType: sched,
		}] = nil
	}

	less := func(a, b NamespaceDigestKey) int {
		if c := cmpStr(a.OrgID, b.OrgID); c != 0 {
			return c
		}
		if c := cmpStr(a.ClusterUUID, b.ClusterUUID); c != 0 {
			return c
		}
		if c := cmpStr(a.Namespace, b.Namespace); c != 0 {
			return c
		}
		if a.BucketDate.Before(b.BucketDate) {
			return -1
		}
		if a.BucketDate.After(b.BucketDate) {
			return 1
		}
		return cmpStr(string(a.ScheduleType), string(b.ScheduleType))
	}

	first := sortedNamespaceDigestKeys(grouped)
	require.Len(t, first, 200)
	assert.True(t, slices.IsSortedFunc(first, less), "keys must come out in sorted order")

	for i := 0; i < 100; i++ {
		assert.Equal(t, first, sortedNamespaceDigestKeys(grouped),
			"identical output on every call (iteration %d) — map order must not leak through", i)
	}
}
