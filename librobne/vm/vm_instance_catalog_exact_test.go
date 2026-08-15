package vm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentInstanceType_ExactMatch(t *testing.T) {
	match := RecognizeInstanceTypeExact(4, 16, nil)
	require.NotNil(t, match)
	assert.Equal(t, "u1.xlarge", match.Name)
}

func TestCurrentInstanceType_NoMatch(t *testing.T) {
	match := RecognizeInstanceTypeExact(3, 16, nil)
	assert.Nil(t, match)
}

func TestCurrentInstanceType_ClusterTypeMatch(t *testing.T) {
	clusterTypes := []InstanceType{
		{Name: "custom.large", Series: vmSeriesGeneralPurpose, VCPU: 8, MemoryGiB: 32, GPUs: 0, Selectable: true},
	}
	match := RecognizeInstanceTypeExact(8, 32, clusterTypes)
	require.NotNil(t, match)
	assert.Equal(t, "custom.large", match.Name)
}
