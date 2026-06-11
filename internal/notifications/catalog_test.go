package notifications

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCatalog_AllCodesSorted(t *testing.T) {
	resp := BuildCatalog("")
	require.Equal(t, len(Definitions), resp.Meta.Count)
	for i := 1; i < len(resp.Data); i++ {
		assert.Less(t, resp.Data[i-1].Code, resp.Data[i].Code)
	}
	for _, entry := range resp.Data {
		assert.NotEmpty(t, entry.Name)
		assert.Equal(t, CodeNames[entry.Code], entry.Name)
	}
}

func TestBuildCatalog_PluginFilterContainer_IncludesStale(t *testing.T) {
	resp := BuildCatalog("container")
	codes := make([]int16, len(resp.Data))
	for i, e := range resp.Data {
		codes[i] = e.Code
	}
	assert.Contains(t, codes, int16(2))
}

func TestBuildCatalog_PluginFilterNode_EmittedAndReserved(t *testing.T) {
	resp := BuildCatalog("node")
	codes := make([]int16, len(resp.Data))
	for i, e := range resp.Data {
		codes[i] = e.Code
	}
	for _, want := range []int16{11, 12, 13, 15, 25, 74, 76, 77} {
		assert.Contains(t, codes, want, "node plugin catalog missing code %d", want)
	}
	assert.NotContains(t, codes, int16(36), "code 36 is emitted by gpu/timeslicing (filter[plugin]=gpu), not CPU/memory node utilization")
	assert.Contains(t, codes, int16(75), "reserved AUTOSCALER_MIN_REPLICAS code 75 should appear under node filter")
}
