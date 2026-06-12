package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeDigestRowWithType(node, instanceType string, day int, cpuP50, cpuP95, memP50, memP95, cpuReqs, memReqs int64, allocCPU, allocMem *int64) NodeDigestRow {
	d := makeDigestRow(node, day, cpuP50, cpuP95, memP50, memP95, cpuReqs, memReqs, allocCPU, allocMem)
	d.InstanceType = instanceType
	return d
}

func underutilizedNodeDigests(node, instanceType string, days int, cpuP95, memP95 int64) []NodeDigestRow {
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)
	digests := make([]NodeDigestRow, days)
	for i := 0; i < days; i++ {
		day := i + 1
		digests[i] = makeDigestRowWithType(
			node, instanceType, day,
			cpuP95/2, cpuP95, memP95/2, memP95,
			4000, 16000, allocCPU, allocMem,
		)
	}
	return digests
}

func relaxedUnderutilConfig() NodeRecConfig {
	cfg := defaultNodeRecConfig()
	cfg.UnderutilThresholdBP = ThresholdToBasisPoints(0.40)
	return cfg
}

func recsByNode(recs []NodeRec, term, engine string) map[string]NodeRec {
	m := make(map[string]NodeRec)
	for _, r := range recs {
		if r.Term == term && r.Engine == engine {
			m[r.Node] = r
		}
	}
	return m
}

func totalReduction(recs []NodeRec, term, engine string) int {
	sum := 0
	for _, r := range recs {
		if r.Term == term && r.Engine == engine {
			sum += r.NodeCountReduction
		}
	}
	return sum
}

func TestRecommendNodes_InstanceTypeGroupConsolidation_FiveNodes(t *testing.T) {
	cfg := relaxedUnderutilConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	var digests []NodeDigestRow
	for i := 1; i <= 5; i++ {
		node := "m5-node-" + string(rune('0'+i))
		digests = append(digests,
			makeDigestRowWithType(node, "m5.xlarge", 1, 3000, 6000, 6000, 12000, 4000, 16000, allocCPU, allocMem),
			makeDigestRowWithType(node, "m5.xlarge", 2, 3000, 6000, 6000, 12000, 4000, 16000, allocCPU, allocMem),
			makeDigestRowWithType(node, "m5.xlarge", 3, 3000, 6000, 6000, 12000, 4000, 16000, allocCPU, allocMem),
		)
	}

	results := RecommendNodes(digests, cfg, defaultNodeThresholdSettings, singleMediumTerm())
	byNode := recsByNode(results, "medium", "cost")

	require.Len(t, byNode, 5)
	assert.Equal(t, 2, totalReduction(results, "medium", "cost"), "5 nodes with fleet workload fitting ~3 should remove 2")

	reduced := 0
	for _, r := range byNode {
		if r.NodeCountReduction == 1 {
			reduced++
		} else {
			assert.Equal(t, 0, r.NodeCountReduction)
		}
	}
	assert.Equal(t, 2, reduced)
}

func TestRecommendNodes_InstanceTypeGroupConsolidation_MixedTypes(t *testing.T) {
	cfg := relaxedUnderutilConfig()
	allocLarge := ptr64(16000)
	alloc2x := ptr64(32000)

	var digests []NodeDigestRow
	for i := 1; i <= 3; i++ {
		node := "xl-" + string(rune('0'+i))
		digests = append(digests, underutilizedNodeDigests(node, "m5.xlarge", 3, 6000, 12000)...)
	}
	for i := 1; i <= 2; i++ {
		node := "2xl-" + string(rune('0'+i))
		digests = append(digests,
			makeDigestRowWithType(node, "m5.2xlarge", 1, 4000, 8000, 8000, 16000, 8000, 32000, alloc2x, ptr64(131072)),
			makeDigestRowWithType(node, "m5.2xlarge", 2, 4000, 8000, 8000, 16000, 8000, 32000, alloc2x, ptr64(131072)),
			makeDigestRowWithType(node, "m5.2xlarge", 3, 4000, 8000, 8000, 16000, 8000, 32000, alloc2x, ptr64(131072)),
		)
	}
	_ = allocLarge // m5.xlarge group reference capacity

	results := RecommendNodes(digests, cfg, defaultNodeThresholdSettings, singleMediumTerm())
	assert.Equal(t, 5, len(recsByNode(results, "medium", "cost")))

	xlReduction := 0
	xl2Reduction := 0
	for _, r := range results {
		if r.Engine != "cost" || r.Term != "medium" {
			continue
		}
		if r.NodeCountReduction > 0 {
			switch {
			case r.Node[:3] == "xl-":
				xlReduction += r.NodeCountReduction
			case r.Node[:4] == "2xl-":
				xl2Reduction += r.NodeCountReduction
			}
		}
	}
	assert.Greater(t, xlReduction, 0, "m5.xlarge group should consolidate")
	assert.GreaterOrEqual(t, xl2Reduction, 0, "m5.2xlarge group computes independently")
}

func TestRecommendNodes_UnknownInstanceType_FallsBackToBinary(t *testing.T) {
	cfg := defaultNodeRecConfig()
	digests := underutilizedNodeDigests("bare-metal-1", "", 3, 2000, 4000)

	results := RecommendNodes(digests, cfg, defaultNodeThresholdSettings, singleMediumTerm())
	rec := recsByNode(results, "medium", "cost")["bare-metal-1"]
	require.NotEmpty(t, rec.Node)
	assert.Equal(t, 1, rec.NodeCountReduction, "single unknown-capacity node uses binary consolidation")
}

func TestRecommendNodes_SimilarCapacityWithoutInstanceType_Consolidates(t *testing.T) {
	cfg := relaxedUnderutilConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	var digests []NodeDigestRow
	for i := 1; i <= 4; i++ {
		node := "bm-" + string(rune('0'+i))
		nodeDays := underutilizedNodeDigests(node, "", 3, 6000, 12000)
		for j := range nodeDays {
			nodeDays[j].MaxCPUAllocMC = allocCPU
			nodeDays[j].MaxMemAllocKiB = allocMem
		}
		digests = append(digests, nodeDays...)
	}

	results := RecommendNodes(digests, cfg, defaultNodeThresholdSettings, singleMediumTerm())
	assert.Equal(t, 2, totalReduction(results, "medium", "cost"),
		"four similar-capacity nodes without instance_type should fleet-consolidate")
}

func TestRecommendNodes_AllNodesWellUtilized_NoReduction(t *testing.T) {
	cfg := defaultNodeRecConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	var digests []NodeDigestRow
	for i := 1; i <= 3; i++ {
		node := "busy-" + string(rune('0'+i))
		digests = append(digests,
			makeDigestRowWithType(node, "m5.xlarge", 1, 12000, 14000, 50000, 55000, 14000, 60000, allocCPU, allocMem),
			makeDigestRowWithType(node, "m5.xlarge", 2, 12500, 14500, 51000, 56000, 14000, 60000, allocCPU, allocMem),
			makeDigestRowWithType(node, "m5.xlarge", 3, 12200, 14200, 50500, 55500, 14000, 60000, allocCPU, allocMem),
		)
	}

	results := RecommendNodes(digests, cfg, defaultNodeThresholdSettings, singleMediumTerm())
	assert.Equal(t, 0, totalReduction(results, "medium", "cost"))
}

func TestRecommendNodes_SingleNodeInGroup_BinaryFallback(t *testing.T) {
	cfg := defaultNodeRecConfig()
	digests := underutilizedNodeDigests("solo-node", "m5.xlarge", 3, 2000, 4000)

	results := RecommendNodes(digests, cfg, defaultNodeThresholdSettings, singleMediumTerm())
	rec := recsByNode(results, "medium", "cost")["solo-node"]
	assert.Equal(t, 1, rec.NodeCountReduction)
}

func TestMinimumNodesForWorkload(t *testing.T) {
	// 5 × 2000 mc = 10000 total; 16000 mc per node @ 80% target → 12800 usable per node → ceil(10000/12800)=1
	min := minimumNodesForWorkload(10000, 0, 16000, 65536, 0.8)
	assert.Equal(t, int64(1), min)

	// 5 × 6000 mc = 30000 total → ceil(30000/12800)=3
	min = minimumNodesForWorkload(30000, 0, 16000, 65536, 0.8)
	assert.Equal(t, int64(3), min)
}

func TestFleetGroupKey_PrefersMachineSetOverInstanceType(t *testing.T) {
	class := nodeClassification{MachineSetName: "worker-us-east-1a"}
	key := fleetGroupKey("node-1", class, map[string]string{"node-1": "m5.xlarge"})
	assert.Equal(t, "ms:worker-us-east-1a", key)
}

func TestFleetGroupKey_FallsBackToInstanceType(t *testing.T) {
	class := nodeClassification{}
	key := fleetGroupKey("node-1", class, map[string]string{"node-1": "m5.xlarge"})
	assert.Equal(t, "it:m5.xlarge", key)
}

func TestRecommendNodes_MachineSetFleetGrouping(t *testing.T) {
	cfg := relaxedUnderutilConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	var digests []NodeDigestRow
	for i := 1; i <= 3; i++ {
		node := fmt.Sprintf("ms-node-%d", i)
		for day := 1; day <= 3; day++ {
			row := makeDigestRowWithType(node, "m5.xlarge", day, 3000, 6000, 6000, 12000, 4000, 16000, allocCPU, allocMem)
			row.MachineSetName = "worker-a"
			digests = append(digests, row)
		}
	}
	// Same instance type but different MachineSet — should not fleet-consolidate together.
	for i := 1; i <= 2; i++ {
		node := fmt.Sprintf("other-ms-%d", i)
		for day := 1; day <= 3; day++ {
			row := makeDigestRowWithType(node, "m5.xlarge", day, 3000, 6000, 6000, 12000, 4000, 16000, allocCPU, allocMem)
			row.MachineSetName = "worker-b"
			digests = append(digests, row)
		}
	}

	results := RecommendNodes(digests, cfg, defaultNodeThresholdSettings, singleMediumTerm())
	msAReduction := 0
	msBReduction := 0
	for _, r := range results {
		if r.Engine != "cost" || r.Term != "medium" || r.NodeCountReduction <= 0 {
			continue
		}
		if r.MachineSetName == "worker-a" {
			msAReduction += r.NodeCountReduction
		}
		if r.MachineSetName == "worker-b" {
			msBReduction += r.NodeCountReduction
		}
	}
	assert.Equal(t, 1, msAReduction, "three underutilized nodes in one MachineSet should remove one node")
	assert.LessOrEqual(t, msBReduction, 1, "two nodes in another MachineSet consolidate independently, not with worker-a")

	// Without MachineSet labels, all five nodes fleet-consolidate as one instance_type group.
	var combined []NodeDigestRow
	for i := 1; i <= 5; i++ {
		node := fmt.Sprintf("flat-%d", i)
		for day := 1; day <= 3; day++ {
			combined = append(combined, makeDigestRowWithType(node, "m5.xlarge", day, 3000, 6000, 6000, 12000, 4000, 16000, allocCPU, allocMem))
		}
	}
	combinedResults := RecommendNodes(combined, cfg, defaultNodeThresholdSettings, singleMediumTerm())
	assert.Equal(t, 2, totalReduction(combinedResults, "medium", "cost"),
		"five homogeneous nodes without MachineSet should fleet-consolidate more aggressively than a three-node MachineSet pool")
}

func TestRecommendNodes_FleetConsolidationNotificationIncludesMachineSet(t *testing.T) {
	cfg := relaxedUnderutilConfig()
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)

	var digests []NodeDigestRow
	for i := 1; i <= 4; i++ {
		node := "fleet-" + string(rune('0'+i))
		for day := 1; day <= 3; day++ {
			row := makeDigestRowWithType(node, "m5.xlarge", day, 3000, 6000, 6000, 12000, 4000, 16000, allocCPU, allocMem)
			row.MachineSetName = "worker-fleet"
			digests = append(digests, row)
		}
	}

	results := RecommendNodes(digests, cfg, defaultNodeThresholdSettings, singleMediumTerm())
	var withFleetNotif int
	for _, r := range results {
		if r.Engine != "cost" || r.Term != "medium" || r.MachineSetName != "worker-fleet" {
			continue
		}
		for _, c := range r.NotificationCodes {
			if c == NotifNodeFleetConsolidation {
				withFleetNotif++
				break
			}
		}
	}
	assert.Greater(t, withFleetNotif, 0, "fleet consolidation should emit MachineSet notification on reduced nodes")
}

func TestComputeGroupNodeCountReduction(t *testing.T) {
	indices := []int{0, 1, 2, 3, 4}
	recs := make([]NodeRec, 5)
	classes := make(map[string]nodeClassification)
	for i := 0; i < 5; i++ {
		node := "n" + string(rune('0'+i))
		recs[i] = NodeRec{Node: node}
		classes[node] = nodeClassification{
			maxCPUUsageP95MC:  6000,
			maxMemUsageP95KiB: 12000,
			CurrentCPUMC:      16000,
			CurrentMemKiB:     65536,
		}
	}
	reduction := computeGroupNodeCountReduction(indices, recs, classes, 0.8)
	assert.Equal(t, 2, reduction)
}
