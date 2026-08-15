package node

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecommendNodes_PopulatesInstanceTypeFromDigests(t *testing.T) {
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)
	digests := []DigestRow{
		makeDigestRowWithType("node-a", "m5.xlarge", 1, 3000, 6000, 6000, 12000, 4000, 16000, allocCPU, allocMem),
		makeDigestRowWithType("node-a", "m5.xlarge", 2, 3000, 6000, 6000, 12000, 4000, 16000, allocCPU, allocMem),
		makeDigestRowWithType("node-a", "m5.xlarge", 3, 3000, 6000, 6000, 12000, 4000, 16000, allocCPU, allocMem),
		makeDigestRowWithType("node-b", "", 1, 3000, 6000, 6000, 12000, 4000, 16000, allocCPU, allocMem),
		makeDigestRowWithType("node-b", "", 2, 3000, 6000, 6000, 12000, 4000, 16000, allocCPU, allocMem),
		makeDigestRowWithType("node-b", "", 3, 3000, 6000, 6000, 12000, 4000, 16000, allocCPU, allocMem),
	}

	recs := RecommendNodes(digests, relaxedUnderutilConfig(), defaultThresholdSettings, singleMediumTerm())
	byNode := recsByNode(recs, "medium", "cost")
	require.Contains(t, byNode, "node-a")
	require.Contains(t, byNode, "node-b")
	assert.Equal(t, "m5.xlarge", byNode["node-a"].InstanceType)
	assert.Empty(t, byNode["node-b"].InstanceType)
}
