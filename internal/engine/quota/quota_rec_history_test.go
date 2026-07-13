package quota

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaHistoryEntries_PerResource(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	cpuBP := 5000
	memBP := 6000
	storageBP := 7000
	podsBP := 8000
	rec := QuotaRec{
		RecommendationType: QuotaRecTypeTighten,
		RiskLevel:          QuotaRiskMedium,
		Snapshot: NamespaceQuotaSnapshot{
			CPURequestHardMC:        100000,
			CPURequestUsedMC:        50000,
			MemoryRequestHardBytes:  2 * gib,
			MemoryRequestUsedBytes:  gib,
			StorageRequestHardBytes: 10 * gib,
			StorageRequestUsedBytes: 3 * gib,
			PodsHard:                100,
			PodsUsed:                80,
		},
		Recommended: QuotaResourceBundle{
			CPURequestMillicores: 60000,
			MemoryRequestBytes:   1200000000,
			StorageRequestBytes:  3600000000,
			Pods:                 88,
		},
		Utilization: QuotaUtilizationBP{
			CPURequestBP:     &cpuBP,
			MemoryRequestBP:  &memBP,
			StorageRequestBP: &storageBP,
			PodsBP:           &podsBP,
		},
	}

	entries := quotaHistoryEntries(rec)
	require.Len(t, entries, 4)
	resources := []string{entries[0].resource, entries[1].resource, entries[2].resource, entries[3].resource}
	assert.ElementsMatch(t, []string{"cpu_request", "memory_request", "storage_request", "pods"}, resources)

	cpu := findQuotaHistoryEntry(entries, "cpu_request")
	require.NotNil(t, cpu)
	assert.Equal(t, int64(100000), cpu.currentHard)
	assert.Equal(t, int64(50000), cpu.currentUsed)
	assert.Equal(t, int64(60000), cpu.recommendedHard)
	require.NotNil(t, cpu.utilizationPercent)
	assert.Equal(t, 50, *cpu.utilizationPercent)
}

func findQuotaHistoryEntry(entries []quotaHistoryEntry, resource string) *quotaHistoryEntry {
	for i := range entries {
		if entries[i].resource == resource {
			return &entries[i]
		}
	}
	return nil
}
