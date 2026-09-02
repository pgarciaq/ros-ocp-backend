package node

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/fixedpoint"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// nodeClassification holds shared utilization signals and flags computed once per (node, term).
type nodeClassification struct {
	Node                  string
	PodCount              int64
	validDays             int
	CPUUtilP50            float32
	CPUUtilP95            float32
	MemUtilP50            float32
	MemUtilP95            float32
	CPUOvercommitRatio    float32
	IsUnderutilized       bool
	IsOvercommitted       bool
	Category              string
	IdleState             types.IdleState
	StrandedResource      *string
	PodCapacity           int64
	PodSchedulingHeadroom float32 // fraction 0.0–1.0; -1 when pod capacity unknown
	MachineSetName        string
	TrendSlope            float32
	CurrentCPUMC          int64
	CurrentMemKiB         int64
	NotificationCodes     []int16
	maxCPUUsageP95MC      int64
	maxMemUsageP95KiB     int64
	maxCPURequestsMC      int64
	maxMemRequestsKiB     int64
	EMAImbalanceBP        int32
}

// RecommendNodes evaluates node-level utilization signals from daily digest data.
// It produces one Rec per node per term per engine. Shared classification is
// computed once per (node, term); engine-specific sizing and consolidation differ.
func RecommendNodes(digests []DigestRow, cfg RecConfig, nodeSettings ThresholdSettings, terms []types.TermConfig) []Rec {
	nodeEngines := EnginesFromThresholds(nodeSettings)
	grouped := map[string][]DigestRow{}
	for _, d := range digests {
		grouped[d.Node] = append(grouped[d.Node], d)
	}

	results := make([]Rec, 0, len(grouped)*len(terms)*len(nodeEngines))
	classesByNodeTerm := make(map[string]map[string]nodeClassification)
	instanceTypes := nodeInstanceTypesFromDigests(digests)

	for node, allDays := range grouped {
		latest := latestNodeDigest(allDays)

		for _, tc := range terms {
			windowDays := filterNodeByWindow(allDays, latest.BucketDate, tc.WindowDays)
			if len(windowDays) < tc.MinDataDays {
				continue
			}
			class := classifyNode(node, windowDays, cfg, nodeSettings, tc.DecayHalfLifeHours, latest.BucketDate)
			applyNodeIdleClassification(&class, nodeSettings)
			if classesByNodeTerm[tc.Name] == nil {
				classesByNodeTerm[tc.Name] = make(map[string]nodeClassification)
			}
			classesByNodeTerm[tc.Name][node] = class

			dataDays := len(windowDays)
			confidence := types.ComputeConfidence(dataDays, tc.MinDataDays, tc.WindowDays)
			for _, eng := range nodeEngines {
				rec := nodeRecFromClassification(class)
				rec.Term = tc.Name
				rec.Engine = eng.Name
				rec.InstanceType = instanceTypes[node]
				rec.PodCapacity = class.PodCapacity
				rec.MachineSetName = class.MachineSetName
				rec.DataDays = dataDays
				rec.ConfidenceLevel = confidence
				rec.NodeGPUCount = latest.NodeGPUCount
				rec.NotificationCodes = evaluateNodeNotifications(rec.NotificationCodes, confidence, dataDays)
				rec.RecommendedCPUMC, rec.RecommendedMemKiB, rec.NodeCountReduction =
					sizeNodeForEngine(class, eng, nodeSettings)
				rec.Expl = nodeExplanationFromClass(class, eng, rec.NodeCountReduction)
				rec.Expl.DataDays = dataDays
				results = append(results, rec)
			}
		}
	}

	applyInstanceTypeConsolidation(results, classesByNodeTerm, instanceTypes, nodeEngines, nodeSettings)
	applyFleetInstanceTypeSuggestions(results, digests, classesByNodeTerm, cfg.AllocatableFactor)
	return results
}

// evaluateNodeNotifications appends data-coverage notification codes for node recommendations.
func evaluateNodeNotifications(codes []int16, confidence float32, dataDays int) []int16 {
	if confidence < defaultLowConfidenceThreshold && dataDays > 0 {
		codes = types.AppendUnique(codes, types.NotifLowConfidence)
	}
	if dataDays > 0 && dataDays <= defaultSparseDataThreshold {
		codes = types.AppendUnique(codes, types.NotifSparseData)
	}
	return codes
}

func nodeRecFromClassification(class nodeClassification) Rec {
	return Rec{
		Node:               class.Node,
		PodCount:           class.PodCount,
		PodCapacity:        class.PodCapacity,
		MachineSetName:     class.MachineSetName,
		CPUUtilP50:         class.CPUUtilP50,
		CPUUtilP95:         class.CPUUtilP95,
		MemUtilP50:         class.MemUtilP50,
		MemUtilP95:         class.MemUtilP95,
		CPUOvercommitRatio: class.CPUOvercommitRatio,
		Category:           class.Category,
		IdleState:          class.IdleState,
		StrandedResource:   class.StrandedResource,
		TrendSlope:         class.TrendSlope,
		CurrentCPUMC:       class.CurrentCPUMC,
		CurrentMemKiB:      class.CurrentMemKiB,
		NotificationCodes:  append([]int16(nil), class.NotificationCodes...),
	}
}

// filterNodeByWindow returns node digest rows within the last windowDays
// from endDate (inclusive), mirroring filterByWindow for container digests.
// Rows are assumed sorted by BucketDate (ascending) from the DB query.
func filterNodeByWindow(rows []DigestRow, endDate time.Time, windowDays int) []DigestRow {
	cutoffDay := endDate.AddDate(0, 0, -(windowDays - 1)).Truncate(24 * time.Hour)
	endDay := endDate.Truncate(24 * time.Hour)

	lo := 0
	hi := len(rows)
	for lo < hi {
		mid := (lo + hi) / 2
		if rows[mid].BucketDate.Truncate(24 * time.Hour).Before(cutoffDay) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	result := make([]DigestRow, 0, len(rows)-lo)
	for i := lo; i < len(rows); i++ {
		d := rows[i].BucketDate.Truncate(24 * time.Hour)
		if d.After(endDay) {
			break
		}
		result = append(result, rows[i])
	}
	return result
}

// latestNodeDigest returns the DigestRow with the most recent BucketDate.
func latestNodeDigest(rows []DigestRow) DigestRow {
	if len(rows) == 0 {
		return DigestRow{}
	}
	best := rows[0]
	for _, r := range rows[1:] {
		if r.BucketDate.After(best.BucketDate) {
			best = r
		}
	}
	return best
}

// classifyNode computes shared utilization classification for a node over a term window.
// Utilization percentiles are decay-weighted when halfLifeHours > 0 (recent days weigh more).
func classifyNode(node string, days []DigestRow, cfg RecConfig, nodeSettings ThresholdSettings, halfLifeHours float64, endDate time.Time) nodeClassification {
	trendMinDays := nodeSettings.TrendMinDays
	class := nodeClassification{Node: node}

	var (
		cpuUtilWeighted50BP float64
		cpuUtilWeighted95BP float64
		memUtilWeighted50BP float64
		memUtilWeighted95BP float64
		totalWeight         float64
		maxRequests         int64
		maxMemReqs          int64
		maxPodCount         int64
		maxPodCapacity      int64
		maxCPUUsageP95MC    int64
		maxMemUsageP95KiB   int64
	)
	cpuMeans := make([]float64, 0, len(days))
	imbalances := make([]float64, 0, len(days))

	for _, d := range days {
		allocCPU := ResolveAllocatable(d.MaxCPUAllocMC, d.MaxCPURequestsMC, cfg.AllocatableFactor)
		allocMem := ResolveAllocatableMem(d.MaxMemAllocKiB, d.MaxMemRequestsKiB, cfg.AllocatableFactor)

		if allocCPU > 0 && allocMem > 0 {
			cpuUtil50BP := fixedpoint.UtilizationBasisPoints(d.CPUUsageP50MC, allocCPU)
			cpuUtil95BP := fixedpoint.UtilizationBasisPoints(d.CPUUsageP95MC, allocCPU)
			memUtil50BP := fixedpoint.UtilizationBasisPoints(d.MemUsageP50KiB, allocMem)
			memUtil95BP := fixedpoint.UtilizationBasisPoints(d.MemUsageP95KiB, allocMem)

			ageHours := endDate.Sub(d.BucketDate).Hours()
			if ageHours < 0 {
				ageHours = 0
			}
			w := types.DecayWeight(ageHours, halfLifeHours)
			if w > 0 {
				cpuUtilWeighted50BP += float64(cpuUtil50BP) * w
				cpuUtilWeighted95BP += float64(cpuUtil95BP) * w
				memUtilWeighted50BP += float64(memUtil50BP) * w
				memUtilWeighted95BP += float64(memUtil95BP) * w
				totalWeight += w
			}

			cpuMeans = append(cpuMeans, fixedpoint.BasisPointsToFloat(cpuUtil50BP))

			highBP := cpuUtil95BP
			if memUtil95BP > highBP {
				highBP = memUtil95BP
			}
			if highBP > 0 {
				diffBP := cpuUtil95BP - memUtil95BP
				if diffBP < 0 {
					diffBP = -diffBP
				}
				imbalanceBP := int32(int64(diffBP) * int64(fixedpoint.BasisPointsScale) / int64(highBP))
				imbalances = append(imbalances, float64(imbalanceBP))
			} else {
				imbalances = append(imbalances, 0)
			}
		}

		if d.CPUUsageP95MC > maxCPUUsageP95MC {
			maxCPUUsageP95MC = d.CPUUsageP95MC
		}
		if d.MemUsageP95KiB > maxMemUsageP95KiB {
			maxMemUsageP95KiB = d.MemUsageP95KiB
		}
		if d.MaxCPURequestsMC > maxRequests {
			maxRequests = d.MaxCPURequestsMC
		}
		if d.MaxMemRequestsKiB > maxMemReqs {
			maxMemReqs = d.MaxMemRequestsKiB
		}
		if d.MaxPodCount > maxPodCount {
			maxPodCount = d.MaxPodCount
		}
		if d.PodCapacity > maxPodCapacity {
			maxPodCapacity = d.PodCapacity
		}
		if class.MachineSetName == "" && d.MachineSetName != "" {
			class.MachineSetName = d.MachineSetName
		}
	}

	class.PodCount = maxPodCount
	class.PodCapacity = maxPodCapacity
	var headroomBP int32
	if maxPodCapacity > 0 {
		headroomBP = fixedpoint.UtilizationBasisPoints(maxPodCapacity-maxPodCount, maxPodCapacity)
		class.PodSchedulingHeadroom = fixedpoint.BasisPointsToFloat32(headroomBP)
	} else {
		class.PodSchedulingHeadroom = -1
	}
	notificationThBP := fixedpoint.FloatToBasisPoints(nodeSettings.PodHeadroomNotificationThreshold)
	if class.PodSchedulingHeadroom >= 0 && headroomBP < notificationThBP {
		class.NotificationCodes = append(class.NotificationCodes, types.NotifNodePodSchedulingLimit)
	}
	class.maxCPUUsageP95MC = maxCPUUsageP95MC
	class.maxMemUsageP95KiB = maxMemUsageP95KiB
	class.maxCPURequestsMC = maxRequests
	class.maxMemRequestsKiB = maxMemReqs
	class.validDays = len(days)

	if totalWeight == 0 {
		return class
	}

	avgCPU50BP := int32(cpuUtilWeighted50BP / totalWeight)
	avgCPU95BP := int32(cpuUtilWeighted95BP / totalWeight)
	avgMem50BP := int32(memUtilWeighted50BP / totalWeight)
	avgMem95BP := int32(memUtilWeighted95BP / totalWeight)

	class.CPUUtilP50 = fixedpoint.BasisPointsToFloat32(avgCPU50BP)
	class.CPUUtilP95 = fixedpoint.BasisPointsToFloat32(avgCPU95BP)
	class.MemUtilP50 = fixedpoint.BasisPointsToFloat32(avgMem50BP)
	class.MemUtilP95 = fixedpoint.BasisPointsToFloat32(avgMem95BP)

	if avgCPU95BP < cfg.UnderutilThresholdBP && avgMem95BP < cfg.UnderutilThresholdBP {
		class.IsUnderutilized = true
		class.NotificationCodes = append(class.NotificationCodes, types.NotifNodeUnderutilized)
	}

	lastDay := days[len(days)-1]
	allocCPU := ResolveAllocatable(lastDay.MaxCPUAllocMC, lastDay.MaxCPURequestsMC, cfg.AllocatableFactor)
	allocMem := ResolveAllocatableMem(lastDay.MaxMemAllocKiB, lastDay.MaxMemRequestsKiB, cfg.AllocatableFactor)
	if allocCPU > 0 {
		class.CurrentCPUMC = allocCPU
	}
	if allocMem > 0 {
		class.CurrentMemKiB = allocMem
	}

	if allocCPU > 0 && maxRequests > 0 {
		ratioBP := fixedpoint.UtilizationBasisPoints(maxRequests, allocCPU)
		class.CPUOvercommitRatio = fixedpoint.BasisPointsToFloat32(ratioBP)
		if ratioBP > cfg.OvercommitThresholdBP {
			class.IsOvercommitted = true
			class.NotificationCodes = append(class.NotificationCodes, types.NotifNodeOvercommitted)
		}
	}

	if len(imbalances) >= 2 {
		imbalanceThreshBP := cfg.StrandedImbalanceThresholdBP
		if imbalanceThreshBP == 0 {
			imbalanceThreshBP = fixedpoint.FloatToBasisPoints(0.6)
		}
		alpha := cfg.EMAAlpha
		if alpha == 0 {
			alpha = 0.3
		}
		smoothed := emaSmooth(imbalances, alpha)
		finalImbalanceBP := int32(smoothed[len(smoothed)-1])
		class.EMAImbalanceBP = finalImbalanceBP
		if finalImbalanceBP > imbalanceThreshBP {
			if avgCPU95BP > avgMem95BP {
				s := "memory"
				class.StrandedResource = &s
			} else {
				s := "cpu"
				class.StrandedResource = &s
			}
			class.NotificationCodes = append(class.NotificationCodes, types.NotifStrandedResources)
		}
	}

	if len(cpuMeans) >= trendMinDays {
		alpha := cfg.EMAAlpha
		if alpha == 0 {
			alpha = 0.3
		}
		smoothed := emaSmooth(cpuMeans, alpha)
		class.TrendSlope = float32(LinearRegressionSlope(smoothed))
	}

	return class
}

func applyNodeIdleClassification(class *nodeClassification, nodeSettings ThresholdSettings) {
	class.IdleState = ClassifyNodeIdleState(*class, nodeSettings)
	if class.IdleState == types.IdleStateIdle || class.IdleState == types.IdleStateZombie {
		class.NotificationCodes = append(class.NotificationCodes, types.NotifNodeIdle)
	}
	class.Category = deriveNodeCategory(class)
}

// deriveNodeCategory applies the priority ordering:
// idle > overcommitted > stranded_cpu > stranded_memory > underutilized > optimized
func deriveNodeCategory(class *nodeClassification) string {
	if class.IdleState == types.IdleStateIdle || class.IdleState == types.IdleStateZombie {
		return "idle"
	}
	if class.IsOvercommitted {
		return "overcommitted"
	}
	if class.StrandedResource != nil {
		if *class.StrandedResource == "cpu" {
			return "stranded_cpu"
		}
		return "stranded_memory"
	}
	if class.IsUnderutilized {
		return "underutilized"
	}
	return "optimized"
}

// sizeNodeForEngine derives engine-specific recommended capacity and consolidation flag.
func sizeNodeForEngine(class nodeClassification, eng EngineConfig, nodeSettings ThresholdSettings) (cpuMC, memKiB int64, nodeCountReduction int) {
	cpuMC, memKiB = recommendedNodeCapacity(
		class.maxCPUUsageP95MC, class.maxMemUsageP95KiB,
		class.maxCPURequestsMC, class.maxMemRequestsKiB,
		eng.TargetUtilization,
	)

	if !class.IsUnderutilized || podSchedulingBlocksConsolidation(class, nodeSettings) {
		return cpuMC, memKiB, 0
	}

	switch eng.Name {
	case "cost":
		nodeCountReduction = 1
	case "performance":
		if hasFullSpareNodeHeadroom(class.CurrentCPUMC, class.CurrentMemKiB, cpuMC, memKiB, nodeSettings.PerfConsolidationHeadroomMultiplier) {
			nodeCountReduction = 1
		}
	}
	return cpuMC, memKiB, nodeCountReduction
}

func nodeExplanationFromClass(class nodeClassification, eng EngineConfig, nodeCountReduction int) types.NodeExplanationFactors {
	formula := "target_util"
	if class.IdleState == types.IdleStateIdle || class.IdleState == types.IdleStateZombie {
		formula = "idle"
	} else if class.IsUnderutilized && nodeCountReduction > 0 {
		formula = "headroom_2x"
	}
	headroomBP := int32(-1)
	if class.PodSchedulingHeadroom >= 0 {
		headroomBP = int32(class.PodSchedulingHeadroom * float32(fixedpoint.BasisPointsScale))
	}
	return types.NodeExplanationFactors{
		TargetUtilizationBP:     int32(eng.TargetUtilization * float64(fixedpoint.BasisPointsScale)),
		CurrentCPUMC:            class.CurrentCPUMC,
		CurrentMemKiB:           class.CurrentMemKiB,
		MaxCPUUsageP95MC:        class.maxCPUUsageP95MC,
		MaxMemUsageP95KiB:       class.maxMemUsageP95KiB,
		PodSchedulingHeadroomBP: headroomBP,
		EMAImbalanceBP:          class.EMAImbalanceBP,
		ConsolidationApplied:    nodeCountReduction > 0,
		SizingFormula:           formula,
	}
}

// resolveNodeInstanceType returns the most recent non-empty instance type for a node.
func resolveNodeInstanceType(days []DigestRow) string {
	best := ""
	var bestDate time.Time
	for _, d := range days {
		if d.InstanceType == "" {
			continue
		}
		if d.BucketDate.After(bestDate) {
			bestDate = d.BucketDate
			best = d.InstanceType
		}
	}
	return best
}

// applyFleetInstanceTypeSuggestions recommends an instance type already present in the
// cluster when a node has stranded CPU or memory (Tier 1 "recommend from your own fleet").
func applyFleetInstanceTypeSuggestions(
	recs []Rec,
	digests []DigestRow,
	classesByNodeTerm map[string]map[string]nodeClassification,
	allocatableFactor float64,
) {
	fleetRatios := clusterInstanceTypeCapacityRatios(digests, allocatableFactor)
	if len(fleetRatios) == 0 {
		return
	}
	for i := range recs {
		classes := classesByNodeTerm[recs[i].Term]
		if classes == nil {
			continue
		}
		class, ok := classes[recs[i].Node]
		if !ok || class.StrandedResource == nil {
			continue
		}
		suggested, reason := suggestFleetInstanceType(
			*class.StrandedResource,
			recs[i].InstanceType,
			class.CurrentCPUMC,
			class.CurrentMemKiB,
			fleetRatios,
		)
		recs[i].SuggestedInstanceType = suggested
		recs[i].InstanceTypeReason = reason
	}
}

// clusterInstanceTypeCapacityRatios returns mean allocatable CPU/memory ratio per instance type.
func clusterInstanceTypeCapacityRatios(digests []DigestRow, allocatableFactor float64) map[string]float64 {
	latest := make(map[string]DigestRow)
	for _, d := range digests {
		if d.InstanceType == "" {
			continue
		}
		if prev, ok := latest[d.Node]; !ok || d.BucketDate.After(prev.BucketDate) {
			latest[d.Node] = d
		}
	}
	sum := make(map[string]float64)
	count := make(map[string]int)
	for _, d := range latest {
		cpu := ResolveAllocatable(d.MaxCPUAllocMC, d.MaxCPURequestsMC, allocatableFactor)
		mem := ResolveAllocatableMem(d.MaxMemAllocKiB, d.MaxMemRequestsKiB, allocatableFactor)
		if cpu <= 0 || mem <= 0 {
			continue
		}
		sum[d.InstanceType] += float64(cpu) / float64(mem)
		count[d.InstanceType]++
	}
	out := make(map[string]float64, len(sum))
	for it, total := range sum {
		if n := count[it]; n > 0 {
			out[it] = total / float64(n)
		}
	}
	return out
}

// suggestFleetInstanceType picks a different instance type in the same cluster with a
// capacity ratio better matched to the stranded dimension.
func suggestFleetInstanceType(
	strandedResource, currentInstanceType string,
	nodeCPUMC, nodeMemKiB int64,
	fleetRatios map[string]float64,
) (instanceType, reason string) {
	if currentInstanceType == "" || nodeCPUMC <= 0 || nodeMemKiB <= 0 {
		return "", ""
	}
	nodeRatio := float64(nodeCPUMC) / float64(nodeMemKiB)
	bestType := ""
	bestRatio := 0.0
	found := false

	for candidate, ratio := range fleetRatios {
		if candidate == "" || candidate == currentInstanceType {
			continue
		}
		switch strandedResource {
		case "cpu":
			// Memory-heavy workload on a CPU-heavy shape: prefer lower CPU:memory ratio.
			if ratio >= nodeRatio {
				continue
			}
			if !found || ratio > bestRatio {
				bestType, bestRatio, found = candidate, ratio, true
			}
		case "memory":
			// CPU-heavy workload on a memory-heavy shape: prefer higher CPU:memory ratio.
			if ratio <= nodeRatio {
				continue
			}
			if !found || ratio < bestRatio {
				bestType, bestRatio, found = candidate, ratio, true
			}
		default:
			return "", ""
		}
	}
	if !found {
		return "", ""
	}
	switch strandedResource {
	case "cpu":
		return bestType, fmt.Sprintf(
			"CPU-stranded node; %s in same cluster has lower CPU:memory allocatable ratio",
			bestType,
		)
	case "memory":
		return bestType, fmt.Sprintf(
			"Memory-stranded node; %s in same cluster has higher CPU:memory allocatable ratio",
			bestType,
		)
	default:
		return "", ""
	}
}

// nodeInstanceTypesFromDigests maps each node to its latest non-empty instance type.
func nodeInstanceTypesFromDigests(digests []DigestRow) map[string]string {
	types := make(map[string]string)
	dates := make(map[string]time.Time)
	for _, d := range digests {
		if d.InstanceType == "" {
			continue
		}
		if prev, ok := dates[d.Node]; !ok || d.BucketDate.After(prev) {
			dates[d.Node] = d.BucketDate
			types[d.Node] = d.InstanceType
		}
	}
	return types
}

// fleetGroupKey returns the fleet consolidation grouping key for a node.
// Precedence: MachineSet name > instance_type > similar allocatable capacity bucket.
func fleetGroupKey(node string, class nodeClassification, instanceTypes map[string]string) string {
	if class.MachineSetName != "" {
		return "ms:" + class.MachineSetName
	}
	if it := instanceTypes[node]; it != "" {
		return "it:" + it
	}
	if capKey := nodeCapacityFleetKey(class); capKey != "" {
		return "cap:" + capKey
	}
	return ""
}

// nodeCapacityFleetKey groups nodes with similar allocatable CPU and memory (within ~10%
// when expressed at one decimal core/GiB precision). Used when instance_type is absent.
func nodeCapacityFleetKey(class nodeClassification) string {
	cpu := class.CurrentCPUMC
	mem := class.CurrentMemKiB
	if cpu <= 0 || mem <= 0 {
		return ""
	}
	cores := float64(cpu) / 1000.0
	gib := float64(mem) / (1024.0 * 1024.0)
	return fmt.Sprintf("%.1f|%.1f", math.Round(cores*10)/10, math.Round(gib*10)/10)
}

// applyInstanceTypeConsolidation adjusts node_count_reduction using fleet-level grouping.
// Precedence: MachineSet > instance_type > similar allocatable capacity.
func applyInstanceTypeConsolidation(
	recs []Rec,
	classesByNodeTerm map[string]map[string]nodeClassification,
	instanceTypes map[string]string,
	nodeEngines []EngineConfig,
	nodeSettings ThresholdSettings,
) {
	if len(recs) == 0 {
		return
	}

	engineByName := make(map[string]EngineConfig, len(nodeEngines))
	for _, eng := range nodeEngines {
		engineByName[eng.Name] = eng
	}

	type termEngine struct {
		term   string
		engine string
	}
	groups := make(map[termEngine][]int)
	for i, rec := range recs {
		key := termEngine{term: rec.Term, engine: rec.Engine}
		groups[key] = append(groups[key], i)
	}

	for key, indices := range groups {
		eng, ok := engineByName[key.engine]
		if !ok {
			continue
		}
		classes := classesByNodeTerm[key.term]
		if classes == nil {
			continue
		}

		byFleetKey := make(map[string][]int)
		for _, i := range indices {
			rec := recs[i]
			class := classes[rec.Node]
			fk := fleetGroupKey(rec.Node, class, instanceTypes)
			if fk == "" {
				recs[i].NodeCountReduction = binaryNodeCountReduction(recs[i], class, eng, nodeSettings)
				continue
			}
			byFleetKey[fk] = append(byFleetKey[fk], i)
		}
		for fleetKey, groupIndices := range byFleetKey {
			if len(groupIndices) == 1 {
				i := groupIndices[0]
				recs[i].NodeCountReduction = binaryNodeCountReduction(recs[i], classes[recs[i].Node], eng, nodeSettings)
				continue
			}
			groupReduction := assignGroupNodeCountReduction(recs, groupIndices, classes, eng, nodeSettings)
			if groupReduction > 0 && strings.HasPrefix(fleetKey, "ms:") {
				machineSetName := strings.TrimPrefix(fleetKey, "ms:")
				appendFleetConsolidationNotifications(recs, groupIndices, machineSetName, groupReduction)
			}
		}
	}
}

// podSchedulingBlocksConsolidation returns true when pod scheduling headroom is below
// PodHeadroomConsolidationGate and the node should not absorb additional workloads via consolidation.
func podSchedulingBlocksConsolidation(class nodeClassification, nodeSettings ThresholdSettings) bool {
	gate := float32(nodeSettings.PodHeadroomConsolidationGate)
	return class.PodSchedulingHeadroom >= 0 && class.PodSchedulingHeadroom < gate
}

func binaryNodeCountReduction(rec Rec, class nodeClassification, eng EngineConfig, nodeSettings ThresholdSettings) int {
	if !class.IsUnderutilized || podSchedulingBlocksConsolidation(class, nodeSettings) {
		return 0
	}
	switch eng.Name {
	case "cost":
		return 1
	case "performance":
		if hasFullSpareNodeHeadroom(class.CurrentCPUMC, class.CurrentMemKiB, rec.RecommendedCPUMC, rec.RecommendedMemKiB, nodeSettings.PerfConsolidationHeadroomMultiplier) {
			return 1
		}
	}
	return 0
}

func nodeEligibleForConsolidation(rec Rec, class nodeClassification, eng EngineConfig, nodeSettings ThresholdSettings) bool {
	return binaryNodeCountReduction(rec, class, eng, nodeSettings) > 0
}

// appendFleetConsolidationNotifications adds a fleet consolidation notification when
// multiple nodes in the same MachineSet can be removed.
func appendFleetConsolidationNotifications(recs []Rec, indices []int, machineSetName string, groupReduction int) {
	if machineSetName == "" || groupReduction <= 0 {
		return
	}
	for _, i := range indices {
		if recs[i].NodeCountReduction <= 0 {
			continue
		}
		recs[i].NotificationCodes = types.AppendUnique(recs[i].NotificationCodes, types.NotifNodeFleetConsolidation)
	}
}

// assignGroupNodeCountReduction distributes fleet consolidation across eligible nodes in a group.
// Returns the total number of nodes recommended for removal from the fleet group.
func assignGroupNodeCountReduction(
	recs []Rec,
	indices []int,
	classes map[string]nodeClassification,
	eng EngineConfig,
	nodeSettings ThresholdSettings,
) int {
	for _, i := range indices {
		recs[i].NodeCountReduction = 0
	}

	var eligible []int
	for _, i := range indices {
		class := classes[recs[i].Node]
		if nodeEligibleForConsolidation(recs[i], class, eng, nodeSettings) {
			eligible = append(eligible, i)
		}
	}
	if len(eligible) == 0 {
		return 0
	}

	groupReduction := computeGroupNodeCountReduction(eligible, recs, classes, eng.TargetUtilization)
	if groupReduction <= 0 {
		return 0
	}

	sort.Slice(eligible, func(a, b int) bool {
		utilA := nodeUnderutilScore(classes[recs[eligible[a]].Node])
		utilB := nodeUnderutilScore(classes[recs[eligible[b]].Node])
		return utilA < utilB
	})

	assigned := 0
	for _, i := range eligible {
		if assigned >= groupReduction {
			break
		}
		recs[i].NodeCountReduction = 1
		assigned++
	}
	return groupReduction
}

func nodeUnderutilScore(class nodeClassification) float32 {
	return max(class.CPUUtilP95, class.MemUtilP95)
}

// computeGroupNodeCountReduction estimates how many nodes can be removed from a homogeneous group.
func computeGroupNodeCountReduction(
	indices []int,
	recs []Rec,
	classes map[string]nodeClassification,
	targetUtilization float64,
) int {
	n := len(indices)
	if n <= 1 {
		return 0
	}

	var totalCPUP95, totalMemP95 int64
	var capCPU, capMem int64
	for _, i := range indices {
		class := classes[recs[i].Node]
		totalCPUP95 += class.maxCPUUsageP95MC
		totalMemP95 += class.maxMemUsageP95KiB
		if class.CurrentCPUMC > capCPU {
			capCPU = class.CurrentCPUMC
		}
		if class.CurrentMemKiB > capMem {
			capMem = class.CurrentMemKiB
		}
	}

	minNodes := minimumNodesForWorkload(totalCPUP95, totalMemP95, capCPU, capMem, targetUtilization)
	if minNodes < 1 {
		minNodes = 1
	}
	if minNodes > int64(n) {
		minNodes = int64(n)
	}
	return n - int(minNodes)
}

// minimumNodesForWorkload returns the node count needed for summed P95 CPU and memory usage.
func minimumNodesForWorkload(totalCPUP95, totalMemP95, nodeCPUMC, nodeMemKiB int64, targetUtilization float64) int64 {
	targetScaled := int64(math.Round(targetUtilization * float64(types.MarginScale)))
	if targetScaled <= 0 {
		targetScaled = int64(0.8 * float64(types.MarginScale))
	}

	var nodesCPU, nodesMem int64 = 1, 1
	if nodeCPUMC > 0 && totalCPUP95 > 0 {
		capacity := nodeCPUMC * targetScaled / types.MarginScale
		if capacity > 0 {
			nodesCPU = ceilDivInt64(totalCPUP95, capacity)
		}
	}
	if nodeMemKiB > 0 && totalMemP95 > 0 {
		capacity := nodeMemKiB * targetScaled / types.MarginScale
		if capacity > 0 {
			nodesMem = ceilDivInt64(totalMemP95, capacity)
		}
	}
	minNodes := nodesCPU
	if nodesMem > minNodes {
		minNodes = nodesMem
	}
	if minNodes < 1 {
		minNodes = 1
	}
	return minNodes
}

// hasFullSpareNodeHeadroom reports whether freed capacity could fit another copy of the workload.
func hasFullSpareNodeHeadroom(currentCPUmc, currentMemKiB, recCPUmc, recMemKiB int64, multiplier float64) bool {
	if recCPUmc <= 0 || recMemKiB <= 0 || currentCPUmc <= 0 || currentMemKiB <= 0 || multiplier <= 0 {
		return false
	}
	multScaled := int64(math.Round(multiplier * float64(types.MarginScale)))
	return currentCPUmc*types.MarginScale >= recCPUmc*multScaled && currentMemKiB*types.MarginScale >= recMemKiB*multScaled
}

// recommendedNodeCapacity derives right-sized CPU millicores and memory KiB from peak
// usage and request totals, targeting the given utilization headroom.
// Results are rounded up to whole cores / whole GiB (matching prior behavior).
func recommendedNodeCapacity(maxCPUUsageP95MC, maxMemUsageP95KiB, maxCPURequestsMC, maxMemRequestsKiB int64, targetUtilization float64) (cpuMC, memKiB int64) {
	targetScaled := int64(math.Round(targetUtilization * float64(types.MarginScale)))
	if targetScaled <= 0 {
		targetScaled = int64(0.8 * float64(types.MarginScale))
	}

	var recommendedCPUMC, recommendedMemKiB int64
	if maxCPUUsageP95MC > 0 {
		recommendedCPUMC = ceilDivInt64(maxCPUUsageP95MC*types.MarginScale, targetScaled)
	}
	if maxCPURequestsMC > 0 {
		requestBased := ceilDivInt64(maxCPURequestsMC*types.MarginScale, targetScaled)
		if requestBased > recommendedCPUMC {
			recommendedCPUMC = requestBased
		}
	}
	if maxMemUsageP95KiB > 0 {
		recommendedMemKiB = ceilDivInt64(maxMemUsageP95KiB*types.MarginScale, targetScaled)
	}
	if maxMemRequestsKiB > 0 {
		requestBased := ceilDivInt64(maxMemRequestsKiB*types.MarginScale, targetScaled)
		if requestBased > recommendedMemKiB {
			recommendedMemKiB = requestBased
		}
	}
	const mibPerGiB int64 = 1024 * 1024
	if recommendedCPUMC > 0 {
		cpuMC = ceilDivInt64(recommendedCPUMC, 1000) * 1000
	}
	if recommendedMemKiB > 0 {
		memKiB = ceilDivInt64(recommendedMemKiB, mibPerGiB) * mibPerGiB
	}
	return cpuMC, memKiB
}

func ceilDivInt64(a, b int64) int64 {
	if b <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

// ResolveAllocatable returns the effective allocatable CPU in millicores.
// Prefers max_cpu_allocatable_mc from daily_node_digests (operator allocatable when
// present, otherwise capacity * ROS_NODE_ALLOCATABLE_FACTOR at ingest). When that
// column is unset, falls back to a request-based estimate.
func ResolveAllocatable(storedAlloc *int64, maxRequests int64, factor float64) int64 {
	if storedAlloc != nil && *storedAlloc > 0 {
		return *storedAlloc
	}
	if maxRequests > 0 {
		return int64(float64(maxRequests) / factor)
	}
	return 0
}

// ResolveAllocatableMem returns the effective allocatable memory in KiB.
// See ResolveAllocatable for precedence of stored vs fallback values.
func ResolveAllocatableMem(storedAlloc *int64, maxRequests int64, factor float64) int64 {
	if storedAlloc != nil && *storedAlloc > 0 {
		return *storedAlloc
	}
	if maxRequests > 0 {
		return int64(float64(maxRequests) / factor)
	}
	return 0
}

// emaSmooth applies exponential moving average smoothing.
// alpha in (0,1]: higher = less smoothing, lower = more smoothing.
func emaSmooth(ys []float64, alpha float64) []float64 {
	if len(ys) == 0 {
		return ys
	}
	smoothed := make([]float64, len(ys))
	smoothed[0] = ys[0]
	for i := 1; i < len(ys); i++ {
		smoothed[i] = alpha*ys[i] + (1-alpha)*smoothed[i-1]
	}
	return smoothed
}

// LinearRegressionSlope computes the slope of a simple OLS linear regression
// over equally-spaced points (index as X, value as Y).
func LinearRegressionSlope(ys []float64) float64 {
	n := float64(len(ys))
	if n < 2 {
		return 0
	}
	var sumX, sumY, sumXY, sumX2 float64
	for i, y := range ys {
		x := float64(i)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}
	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / denom
}
