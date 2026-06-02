package engine

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
)

// nodeRecsAdvisoryLock is the pg_advisory_xact_lock key shared between
// PersistNodeRecommendations and migration 000058 (PK rebuild) to prevent
// deadlocks without requiring manual worker shutdown during migrations.
const nodeRecsAdvisoryLock = 7358001

// NodeEngineConfig holds per-engine sizing parameters for node recommendations.
type NodeEngineConfig struct {
	Name              string
	TargetUtilization float64
}

// NodeDigestRow represents a single daily digest for a node, loaded from the database.
type NodeDigestRow struct {
	BucketDate        time.Time
	Node              string
	CPUUsageP50MC     int64
	CPUUsageP95MC     int64
	MemUsageP50KiB    int64
	MemUsageP95KiB    int64
	MaxCPUAllocMC     *int64
	MaxMemAllocKiB    *int64
	MaxCPURequestsMC  int64
	MaxMemRequestsKiB int64
	MaxPodCount       int64
	PodCapacity       int64
	InstanceType      string
	MachineSetName    string
	SampleCount       int64
}

// NodeRecConfig holds configuration parameters for the node recommendation engine.
type NodeRecConfig struct {
	UnderutilThreshold         float64
	OvercommitThreshold        float64
	AllocatableFactor          float64
	StrandedImbalanceThreshold float64
	EMAAlpha                   float64
}

// NodeRec holds the computed recommendation for a single node within a single term and engine.
type NodeRec struct {
	Node                         string
	Term                         string
	Engine                       string
	CPUUtilP50                   float32
	CPUUtilP95                   float32
	MemUtilP50                   float32
	MemUtilP95                   float32
	CPUOvercommitRatio           float32
	IsUnderutilized              bool
	IsOvercommitted              bool
	IdleState                    IdleState
	StrandedResource             *string
	PodCount                     int64
	PodCapacity                  int64
	MachineSetName               string
	TrendSlope                   float32
	CurrentCPUMC                 int64
	CurrentMemKiB                int64
	RecommendedCPUMC             int64
	RecommendedMemKiB            int64
	NodeCountReduction           int
	EstimatedMonthlySavingsCents int64
	InstanceType                 string
	SuggestedInstanceType        string
	InstanceTypeReason           string
	NotificationCodes            []int16
}

// nodeClassification holds shared utilization signals and flags computed once per (node, term).
type nodeClassification struct {
	Node               string
	PodCount           int64
	validDays          int
	CPUUtilP50         float32
	CPUUtilP95         float32
	MemUtilP50         float32
	MemUtilP95         float32
	CPUOvercommitRatio float32
	IsUnderutilized    bool
	IsOvercommitted    bool
	IdleState          IdleState
	StrandedResource   *string
	PodCapacity            int64
	PodSchedulingHeadroom  float32 // fraction 0.0–1.0; -1 when pod capacity unknown
	MachineSetName         string
	TrendSlope         float32
	CurrentCPUMC       int64
	CurrentMemKiB      int64
	NotificationCodes  []int16
	maxCPUUsageP95MC   int64
	maxMemUsageP95KiB  int64
	maxCPURequestsMC   int64
	maxMemRequestsKiB  int64
}

// RecommendNodes evaluates node-level utilization signals from daily digest data.
// It produces one NodeRec per node per term per engine. Shared classification is
// computed once per (node, term); engine-specific sizing and consolidation differ.
func RecommendNodes(digests []NodeDigestRow, cfg NodeRecConfig, nodeSettings NodeThresholdSettings, terms []TermConfig) []NodeRec {
	nodeEngines := NodeEnginesFromThresholds(nodeSettings)
	grouped := map[string][]NodeDigestRow{}
	for _, d := range digests {
		grouped[d.Node] = append(grouped[d.Node], d)
	}

	results := make([]NodeRec, 0, len(grouped)*len(terms)*len(nodeEngines))
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

			for _, eng := range nodeEngines {
				rec := nodeRecFromClassification(class)
				rec.Term = tc.Name
				rec.Engine = eng.Name
				rec.InstanceType = instanceTypes[node]
				rec.PodCapacity = class.PodCapacity
				rec.MachineSetName = class.MachineSetName
				rec.RecommendedCPUMC, rec.RecommendedMemKiB, rec.NodeCountReduction =
					sizeNodeForEngine(class, eng, nodeSettings)
				results = append(results, rec)
			}
		}
	}

	applyInstanceTypeConsolidation(results, classesByNodeTerm, instanceTypes, nodeEngines, nodeSettings)
	applyFleetInstanceTypeSuggestions(results, digests, classesByNodeTerm, cfg.AllocatableFactor)
	return results
}

func nodeRecFromClassification(class nodeClassification) NodeRec {
	return NodeRec{
		Node:               class.Node,
		PodCount:           class.PodCount,
		PodCapacity:        class.PodCapacity,
		MachineSetName:     class.MachineSetName,
		CPUUtilP50:         class.CPUUtilP50,
		CPUUtilP95:         class.CPUUtilP95,
		MemUtilP50:         class.MemUtilP50,
		MemUtilP95:         class.MemUtilP95,
		CPUOvercommitRatio: class.CPUOvercommitRatio,
		IsUnderutilized:    class.IsUnderutilized,
		IsOvercommitted:    class.IsOvercommitted,
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
func filterNodeByWindow(rows []NodeDigestRow, endDate time.Time, windowDays int) []NodeDigestRow {
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

	result := make([]NodeDigestRow, 0, len(rows)-lo)
	for i := lo; i < len(rows); i++ {
		d := rows[i].BucketDate.Truncate(24 * time.Hour)
		if d.After(endDay) {
			break
		}
		result = append(result, rows[i])
	}
	return result
}

// latestNodeDigest returns the NodeDigestRow with the most recent BucketDate.
func latestNodeDigest(rows []NodeDigestRow) NodeDigestRow {
	if len(rows) == 0 {
		return NodeDigestRow{}
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
func classifyNode(node string, days []NodeDigestRow, cfg NodeRecConfig, nodeSettings NodeThresholdSettings, halfLifeHours float64, endDate time.Time) nodeClassification {
	trendMinDays := nodeSettings.TrendMinDays
	class := nodeClassification{Node: node}

	var (
		cpuUtilWeighted50 float64
		cpuUtilWeighted95 float64
		memUtilWeighted50 float64
		memUtilWeighted95 float64
		totalWeight       float64
		maxRequests       int64
		maxMemReqs        int64
		maxPodCount       int64
		maxPodCapacity    int64
		maxCPUUsageP95MC  int64
		maxMemUsageP95KiB int64
		cpuMeans          []float64
		imbalances        []float64
	)

	for _, d := range days {
		allocCPU := resolveAllocatable(d.MaxCPUAllocMC, d.MaxCPURequestsMC, cfg.AllocatableFactor)
		allocMem := resolveAllocatableMem(d.MaxMemAllocKiB, d.MaxMemRequestsKiB, cfg.AllocatableFactor)

		if allocCPU > 0 && allocMem > 0 {
			cpuUtil50 := float64(d.CPUUsageP50MC) / float64(allocCPU)
			cpuUtil95 := float64(d.CPUUsageP95MC) / float64(allocCPU)
			memUtil50 := float64(d.MemUsageP50KiB) / float64(allocMem)
			memUtil95 := float64(d.MemUsageP95KiB) / float64(allocMem)

			ageHours := endDate.Sub(d.BucketDate).Hours()
			if ageHours < 0 {
				ageHours = 0
			}
			w := DecayWeight(ageHours, halfLifeHours)
			if w > 0 {
				cpuUtilWeighted50 += cpuUtil50 * w
				cpuUtilWeighted95 += cpuUtil95 * w
				memUtilWeighted50 += memUtil50 * w
				memUtilWeighted95 += memUtil95 * w
				totalWeight += w
			}

			cpuMeans = append(cpuMeans, cpuUtil50)

			high := cpuUtil95
			if memUtil95 > high {
				high = memUtil95
			}
			if high > 1e-9 {
				diff := cpuUtil95 - memUtil95
				if diff < 0 {
					diff = -diff
				}
				imbalances = append(imbalances, diff/high)
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
	if maxPodCapacity > 0 {
		class.PodSchedulingHeadroom = float32(maxPodCapacity-maxPodCount) / float32(maxPodCapacity)
	} else {
		class.PodSchedulingHeadroom = -1
	}
	notificationTh := float32(nodeSettings.PodHeadroomNotificationThreshold)
	if class.PodSchedulingHeadroom >= 0 && class.PodSchedulingHeadroom < notificationTh {
		class.NotificationCodes = append(class.NotificationCodes, NotifNodePodSchedulingLimit)
	}
	class.maxCPUUsageP95MC = maxCPUUsageP95MC
	class.maxMemUsageP95KiB = maxMemUsageP95KiB
	class.maxCPURequestsMC = maxRequests
	class.maxMemRequestsKiB = maxMemReqs
	class.validDays = len(days)

	if totalWeight == 0 {
		return class
	}

	avgCPU50 := cpuUtilWeighted50 / totalWeight
	avgCPU95 := cpuUtilWeighted95 / totalWeight
	avgMem50 := memUtilWeighted50 / totalWeight
	avgMem95 := memUtilWeighted95 / totalWeight

	class.CPUUtilP50 = float32(avgCPU50)
	class.CPUUtilP95 = float32(avgCPU95)
	class.MemUtilP50 = float32(avgMem50)
	class.MemUtilP95 = float32(avgMem95)

	if avgCPU95 < cfg.UnderutilThreshold && avgMem95 < cfg.UnderutilThreshold {
		class.IsUnderutilized = true
		class.NotificationCodes = append(class.NotificationCodes, NotifNodeUnderutilized)
	}

	lastDay := days[len(days)-1]
	allocCPU := resolveAllocatable(lastDay.MaxCPUAllocMC, lastDay.MaxCPURequestsMC, cfg.AllocatableFactor)
	allocMem := resolveAllocatableMem(lastDay.MaxMemAllocKiB, lastDay.MaxMemRequestsKiB, cfg.AllocatableFactor)
	if allocCPU > 0 {
		class.CurrentCPUMC = allocCPU
	}
	if allocMem > 0 {
		class.CurrentMemKiB = allocMem
	}

	if allocCPU > 0 && maxRequests > 0 {
		ratio := float64(maxRequests) / float64(allocCPU)
		class.CPUOvercommitRatio = float32(ratio)
		if ratio > cfg.OvercommitThreshold {
			class.IsOvercommitted = true
			class.NotificationCodes = append(class.NotificationCodes, NotifNodeOvercommitted)
		}
	}

	if len(imbalances) >= 2 {
		imbalanceThresh := cfg.StrandedImbalanceThreshold
		if imbalanceThresh == 0 {
			imbalanceThresh = 0.6
		}
		alpha := cfg.EMAAlpha
		if alpha == 0 {
			alpha = 0.3
		}
		smoothed := emaSmooth(imbalances, alpha)
		finalImbalance := smoothed[len(smoothed)-1]
		if finalImbalance > imbalanceThresh {
			if avgCPU95 > avgMem95 {
				s := "memory"
				class.StrandedResource = &s
			} else {
				s := "cpu"
				class.StrandedResource = &s
			}
			class.NotificationCodes = append(class.NotificationCodes, NotifStrandedResources)
		}
	}

	if len(cpuMeans) >= trendMinDays {
		alpha := cfg.EMAAlpha
		if alpha == 0 {
			alpha = 0.3
		}
		smoothed := emaSmooth(cpuMeans, alpha)
		class.TrendSlope = float32(linearRegressionSlope(smoothed))
	}

	return class
}

func applyNodeIdleClassification(class *nodeClassification, nodeSettings NodeThresholdSettings) {
	class.IdleState = ClassifyNodeIdleState(*class, nodeSettings)
	if class.IdleState == IdleStateIdle || class.IdleState == IdleStateZombie {
		class.NotificationCodes = append(class.NotificationCodes, NotifAIdle)
	}
}

// sizeNodeForEngine derives engine-specific recommended capacity and consolidation flag.
func sizeNodeForEngine(class nodeClassification, eng NodeEngineConfig, nodeSettings NodeThresholdSettings) (cpuMC, memKiB int64, nodeCountReduction int) {
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

// resolveNodeInstanceType returns the most recent non-empty instance type for a node.
func resolveNodeInstanceType(days []NodeDigestRow) string {
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
	recs []NodeRec,
	digests []NodeDigestRow,
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
func clusterInstanceTypeCapacityRatios(digests []NodeDigestRow, allocatableFactor float64) map[string]float64 {
	latest := make(map[string]NodeDigestRow)
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
		cpu := resolveAllocatable(d.MaxCPUAllocMC, d.MaxCPURequestsMC, allocatableFactor)
		mem := resolveAllocatableMem(d.MaxMemAllocKiB, d.MaxMemRequestsKiB, allocatableFactor)
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
func nodeInstanceTypesFromDigests(digests []NodeDigestRow) map[string]string {
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
	recs []NodeRec,
	classesByNodeTerm map[string]map[string]nodeClassification,
	instanceTypes map[string]string,
	nodeEngines []NodeEngineConfig,
	nodeSettings NodeThresholdSettings,
) {
	if len(recs) == 0 {
		return
	}

	engineByName := make(map[string]NodeEngineConfig, len(nodeEngines))
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
func podSchedulingBlocksConsolidation(class nodeClassification, nodeSettings NodeThresholdSettings) bool {
	gate := float32(nodeSettings.PodHeadroomConsolidationGate)
	return class.PodSchedulingHeadroom >= 0 && class.PodSchedulingHeadroom < gate
}

func binaryNodeCountReduction(rec NodeRec, class nodeClassification, eng NodeEngineConfig, nodeSettings NodeThresholdSettings) int {
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

func nodeEligibleForConsolidation(rec NodeRec, class nodeClassification, eng NodeEngineConfig, nodeSettings NodeThresholdSettings) bool {
	return binaryNodeCountReduction(rec, class, eng, nodeSettings) > 0
}

// appendFleetConsolidationNotifications adds a fleet consolidation notification when
// multiple nodes in the same MachineSet can be removed.
func appendFleetConsolidationNotifications(recs []NodeRec, indices []int, machineSetName string, groupReduction int) {
	if machineSetName == "" || groupReduction <= 0 {
		return
	}
	for _, i := range indices {
		if recs[i].NodeCountReduction <= 0 {
			continue
		}
		recs[i].NotificationCodes = appendNotificationCode(recs[i].NotificationCodes, NotifNodeFleetConsolidation)
	}
}

func appendNotificationCode(codes []int16, code int16) []int16 {
	for _, c := range codes {
		if c == code {
			return codes
		}
	}
	return append(codes, code)
}

// assignGroupNodeCountReduction distributes fleet consolidation across eligible nodes in a group.
// Returns the total number of nodes recommended for removal from the fleet group.
func assignGroupNodeCountReduction(
	recs []NodeRec,
	indices []int,
	classes map[string]nodeClassification,
	eng NodeEngineConfig,
	nodeSettings NodeThresholdSettings,
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
	recs []NodeRec,
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
	targetScaled := int64(math.Round(targetUtilization * float64(MarginScale)))
	if targetScaled <= 0 {
		targetScaled = int64(0.8 * float64(MarginScale))
	}

	var nodesCPU, nodesMem int64 = 1, 1
	if nodeCPUMC > 0 && totalCPUP95 > 0 {
		capacity := nodeCPUMC * targetScaled / MarginScale
		if capacity > 0 {
			nodesCPU = ceilDivInt64(totalCPUP95, capacity)
		}
	}
	if nodeMemKiB > 0 && totalMemP95 > 0 {
		capacity := nodeMemKiB * targetScaled / MarginScale
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
	multScaled := int64(math.Round(multiplier * float64(MarginScale)))
	return currentCPUmc*MarginScale >= recCPUmc*multScaled && currentMemKiB*MarginScale >= recMemKiB*multScaled
}

// recommendedNodeCapacity derives right-sized CPU millicores and memory KiB from peak
// usage and request totals, targeting the given utilization headroom.
// Results are rounded up to whole cores / whole GiB (matching prior behavior).
func recommendedNodeCapacity(maxCPUUsageP95MC, maxMemUsageP95KiB, maxCPURequestsMC, maxMemRequestsKiB int64, targetUtilization float64) (cpuMC, memKiB int64) {
	targetScaled := int64(math.Round(targetUtilization * float64(MarginScale)))
	if targetScaled <= 0 {
		targetScaled = int64(0.8 * float64(MarginScale))
	}

	var recommendedCPUMC, recommendedMemKiB int64
	if maxCPUUsageP95MC > 0 {
		recommendedCPUMC = ceilDivInt64(maxCPUUsageP95MC*MarginScale, targetScaled)
	}
	if maxCPURequestsMC > 0 {
		requestBased := ceilDivInt64(maxCPURequestsMC*MarginScale, targetScaled)
		if requestBased > recommendedCPUMC {
			recommendedCPUMC = requestBased
		}
	}
	if maxMemUsageP95KiB > 0 {
		recommendedMemKiB = ceilDivInt64(maxMemUsageP95KiB*MarginScale, targetScaled)
	}
	if maxMemRequestsKiB > 0 {
		requestBased := ceilDivInt64(maxMemRequestsKiB*MarginScale, targetScaled)
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

// resolveAllocatable returns the effective allocatable CPU in millicores.
// Prefers max_cpu_allocatable_mc from daily_node_digests (operator allocatable when
// present, otherwise capacity * ROS_NODE_ALLOCATABLE_FACTOR at ingest). When that
// column is unset, falls back to a request-based estimate.
func resolveAllocatable(storedAlloc *int64, maxRequests int64, factor float64) int64 {
	if storedAlloc != nil && *storedAlloc > 0 {
		return *storedAlloc
	}
	if maxRequests > 0 {
		return int64(float64(maxRequests) / factor)
	}
	return 0
}

// resolveAllocatableMem returns the effective allocatable memory in KiB.
// See resolveAllocatable for precedence of stored vs fallback values.
func resolveAllocatableMem(storedAlloc *int64, maxRequests int64, factor float64) int64 {
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

// linearRegressionSlope computes the slope of a simple OLS linear regression
// over equally-spaced points (index as X, value as Y).
func linearRegressionSlope(ys []float64) float64 {
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

// QueryNodeDigests reads daily_node_digests for a cluster within a time range.
func QueryNodeDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, start, end time.Time) ([]NodeDigestRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT bucket_date, node,
			COALESCE(cpu_usage_p50_mc, 0), COALESCE(cpu_usage_p95_mc, 0),
			COALESCE(mem_usage_p50_kib, 0), COALESCE(mem_usage_p95_kib, 0),
			max_cpu_allocatable_mc, max_mem_allocatable_kib,
			COALESCE(max_cpu_requests_mc, 0), COALESCE(max_mem_requests_kib, 0),
			COALESCE(max_pod_count, 0), COALESCE(pod_capacity, 0),
			COALESCE(instance_type, ''), COALESCE(machineset_name, ''),
			COALESCE(sample_count, 0)
		FROM daily_node_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date <= $4
		ORDER BY node, bucket_date`,
		orgID, clusterUUID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	// N.B. filterNodeByWindow uses binary search and relies on bucket_date sort order above.
	if err != nil {
		return nil, fmt.Errorf("query node digests: %w", err)
	}
	defer rows.Close()

	var result []NodeDigestRow
	for rows.Next() {
		var d NodeDigestRow
		err := rows.Scan(
			&d.BucketDate, &d.Node,
			&d.CPUUsageP50MC, &d.CPUUsageP95MC,
			&d.MemUsageP50KiB, &d.MemUsageP95KiB,
			&d.MaxCPUAllocMC, &d.MaxMemAllocKiB,
			&d.MaxCPURequestsMC, &d.MaxMemRequestsKiB,
			&d.MaxPodCount, &d.PodCapacity, &d.InstanceType, &d.MachineSetName, &d.SampleCount,
		)
		if err != nil {
			return nil, fmt.Errorf("scan node digest row: %w", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate node digest rows: %w", err)
	}
	return result, nil
}

// PersistNodeRecommendations upserts computed node recommendations into the database.
func PersistNodeRecommendations(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, recs []NodeRec, validTerms []string) error {
	if len(recs) == 0 {
		return nil
	}

	t0 := time.Now()
	defer func() { metrics.ObserveDB("persist_node_recommendations", t0) }()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Advisory lock serializes with migration 000058 (PK rebuild).
	// If the migration is running, this blocks until it completes rather than deadlocking.
	if _, err := tx.Exec(ctx, fmt.Sprintf("SELECT pg_advisory_xact_lock(%d)", nodeRecsAdvisoryLock)); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}

	for _, r := range recs {
		recommendedCPUCores := float64(r.RecommendedCPUMC) / 1000.0
		recommendedMemGiB := float64(r.RecommendedMemKiB) / (1024.0 * 1024.0)
		_, err := tx.Exec(ctx, `
			INSERT INTO node_recommendations (
				org_id, cluster_uuid, node, term, engine,
				cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
				cpu_overcommit_ratio, is_underutilized, is_overcommitted, idle_state,
				stranded_resource, pod_count, pod_capacity, machineset_name, trend_slope, notification_codes,
				recommended_cpu_cores, recommended_memory_gib, node_count_reduction,
				estimated_monthly_savings_usd, instance_type,
				suggested_instance_type, instance_type_reason,
				updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,now())
			ON CONFLICT (org_id, cluster_uuid, node, term, engine) DO UPDATE SET
				cpu_util_p50 = EXCLUDED.cpu_util_p50,
				cpu_util_p95 = EXCLUDED.cpu_util_p95,
				mem_util_p50 = EXCLUDED.mem_util_p50,
				mem_util_p95 = EXCLUDED.mem_util_p95,
				cpu_overcommit_ratio = EXCLUDED.cpu_overcommit_ratio,
				is_underutilized = EXCLUDED.is_underutilized,
				is_overcommitted = EXCLUDED.is_overcommitted,
				idle_state = EXCLUDED.idle_state,
				stranded_resource = EXCLUDED.stranded_resource,
				pod_count = EXCLUDED.pod_count,
				pod_capacity = EXCLUDED.pod_capacity,
				machineset_name = EXCLUDED.machineset_name,
				trend_slope = EXCLUDED.trend_slope,
				notification_codes = EXCLUDED.notification_codes,
				recommended_cpu_cores = EXCLUDED.recommended_cpu_cores,
				recommended_memory_gib = EXCLUDED.recommended_memory_gib,
				node_count_reduction = EXCLUDED.node_count_reduction,
				estimated_monthly_savings_usd = EXCLUDED.estimated_monthly_savings_usd,
				instance_type = EXCLUDED.instance_type,
				suggested_instance_type = EXCLUDED.suggested_instance_type,
				instance_type_reason = EXCLUDED.instance_type_reason,
				updated_at = now()`,
			orgID, clusterUUID, r.Node, r.Term, r.Engine,
			r.CPUUtilP50, r.CPUUtilP95, r.MemUtilP50, r.MemUtilP95,
			r.CPUOvercommitRatio, r.IsUnderutilized, r.IsOvercommitted, idleStateForWrite(r.IdleState),
			r.StrandedResource, r.PodCount, nullInt64PodCapacity(r.PodCapacity), nullStringMachineSet(r.MachineSetName), r.TrendSlope, r.NotificationCodes,
			recommendedCPUCores, recommendedMemGiB, r.NodeCountReduction,
			r.EstimatedMonthlySavingsCents, r.InstanceType,
			nullStringOptional(r.SuggestedInstanceType), nullStringOptional(r.InstanceTypeReason),
		)
		if err != nil {
			return fmt.Errorf("upsert node rec %s: %w", r.Node, err)
		}
	}

	// Remove rows for terms no longer in the active config (stale term cleanup).
	if len(validTerms) > 0 {
		_, err = tx.Exec(ctx, `
			DELETE FROM node_recommendations
			WHERE org_id = $1 AND cluster_uuid = $2
			  AND term != ALL($3)`,
			orgID, clusterUUID, validTerms,
		)
		if err != nil {
			return fmt.Errorf("cleanup stale terms: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit node recs: %w", err)
	}

	logging.ForOrg(orgID, clusterUUID).Infof("PersistNodeRecommendations: upserted %d recs", len(recs))
	return nil
}

func nullInt64PodCapacity(v int64) any {
	if v <= 0 {
		return nil
	}
	return v
}

func nullStringMachineSet(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullStringOptional(s string) any {
	if s == "" {
		return nil
	}
	return s
}
