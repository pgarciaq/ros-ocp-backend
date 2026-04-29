package engine

import "time"

const (
	// NotifGPUTimeSharingCandidate is emitted on containers and nodes where
	// GPU time-slicing is recommended.
	NotifGPUTimeSharingCandidate int16 = 29

	nodeFreshnessDays             = 7
	timeslicingBasePenalty        = float32(0.7)
	impactedContainerPenaltyWt   = float32(0.3)
	minReplicas                  = 2
	maxReplicas                  = 8
)

// TimeslicingRec holds the time-slicing recommendation for a single node × GPU model.
type TimeslicingRec struct {
	NodeName            string
	ClusterUUID         string
	GPUModel            string
	RecommendedReplicas int
	SavingsPerGPU       *float32
	TotalNodeSavings    *float32
	Confidence          float32
	CandidateContainers []GPUContainerRef
	ImpactedContainers  []GPUContainerRef
	NotificationCodes   []int16
}

// GPUContainerRef identifies a container within a time-slicing recommendation.
type GPUContainerRef struct {
	Namespace      string
	Workload       string
	Container      string
	SMActiveAvg    float32
	Classification GPUClassification
}

// NodeGPUGroup is the input to ComputeNodeTimeslicingRec: all GPU containers
// on the same node with the same GPU model.
type NodeGPUGroup struct {
	NodeName    string
	ClusterUUID string
	GPUModel    string
	LastSeen    time.Time
	Containers  []NodeGPUContainer
}

// NodeGPUContainer pairs a container identity with its per-container GPU recommendation.
type NodeGPUContainer struct {
	Namespace string
	Workload  string
	Container string
	Rec       *GPURec
}

// computeReplicas determines the recommended nvidia.com/gpu.replicas value.
func computeReplicas(avgSM, avgDRAM, avgFBFrac float32) (int, bool) {
	peak := avgSM
	if avgDRAM > peak {
		peak = avgDRAM
	}
	if avgFBFrac > peak {
		peak = avgFBFrac
	}
	if peak <= 0 {
		return maxReplicas, true
	}
	r := int(1.0 / peak)
	if r < minReplicas {
		return 0, false
	}
	if r > maxReplicas {
		r = maxReplicas
	}
	return r, true
}

// computeTimeslicingConfidence computes confidence for a time-slicing recommendation.
func computeTimeslicingConfidence(avgCandidateConf float32, nImpacted, nTotal int) float32 {
	if nTotal == 0 {
		return 0
	}
	impactedRatio := float32(nImpacted) / float32(nTotal)
	return avgCandidateConf * timeslicingBasePenalty * (1.0 - impactedContainerPenaltyWt*impactedRatio)
}

// computeTimeslicingSavings calculates per-GPU and total-node savings.
func computeTimeslicingSavings(replicas, nCandidates int, gpuMonthlyRate *float32) (perGPU, total *float32) {
	if gpuMonthlyRate == nil {
		return nil, nil
	}
	pg := *gpuMonthlyRate * (1.0 - 1.0/float32(replicas))
	tot := pg * float32(nCandidates)
	return &pg, &tot
}

// partitionContainers separates GPU containers into time-slicing candidates
// and impacted (collateral) containers.
func partitionContainers(containers []NodeGPUContainer) (candidates, impacted []NodeGPUContainer) {
	for _, c := range containers {
		if c.Rec == nil {
			continue
		}
		switch {
		case c.Rec.Classification == GPUClassIdle:
			// Excluded: idle GPUs should be removed, not time-sliced
		case c.Rec.Classification == GPUClassMemoryBound:
			// Excluded: sharing memory-bound workloads risks OOM
		case c.Rec.HasMIGRecommendation():
			// Excluded: MIG takes precedence over time-slicing
		case c.Rec.Classification == GPUClassUnderutilized ||
			c.Rec.Classification == GPUClassComputeBoundUnderutil:
			candidates = append(candidates, c)
		default:
			impacted = append(impacted, c)
		}
	}
	return
}

// HasMIGRecommendation returns true if this GPURec has a MIG profile recommendation.
func (r *GPURec) HasMIGRecommendation() bool {
	return r.RecommendedGPUProfile != "" && r.RecommendedGPUProfile != "full_gpu"
}

// avgCandidateUtilization computes average SM, DRAM, and FB fraction across candidates.
func avgCandidateUtilization(candidates []NodeGPUContainer, totalFBMiB float32) (avgSM, avgDRAM, avgFBFrac float32) {
	if len(candidates) == 0 {
		return 0, 0, 0
	}
	var sumSM, sumDRAM, sumFB float32
	for _, c := range candidates {
		sumSM += c.Rec.SMActiveAvg
		sumDRAM += c.Rec.DRAMActiveAvg
		sumFB += c.Rec.FBUsageMaxMiB
	}
	n := float32(len(candidates))
	avgSM = sumSM / n
	avgDRAM = sumDRAM / n
	if totalFBMiB > 0 {
		avgFBFrac = (sumFB / n) / totalFBMiB
	}
	return
}

// isNodeFresh returns true if the node was seen within the freshness window.
func isNodeFresh(lastSeen, now time.Time) bool {
	return now.Sub(lastSeen) <= time.Duration(nodeFreshnessDays)*24*time.Hour
}

// AnnotateTimeslicingCandidates runs the time-slicing engine for the given node
// group, stamping each candidate GPURec with TimeSlicingNode, TimeSlicingReplicas,
// and notification code 29. Use this when you only need the side effects on
// GPURec objects (e.g. container-level enrichment), not the full TimeslicingRec.
func AnnotateTimeslicingCandidates(group NodeGPUGroup) {
	ComputeNodeTimeslicingRec(group, nil)
}

// ComputeNodeTimeslicingRec produces a time-slicing recommendation for a single
// node × GPU model group. Returns nil if the node is not a good candidate.
func ComputeNodeTimeslicingRec(group NodeGPUGroup, gpuRate *float32) *TimeslicingRec {
	if len(group.Containers) == 0 {
		return nil
	}

	if !group.LastSeen.IsZero() && !isNodeFresh(group.LastSeen, time.Now().UTC()) {
		return nil
	}

	candidates, impacted := partitionContainers(group.Containers)
	if len(candidates) == 0 {
		return nil
	}

	// Majority threshold: candidates must be >= 50% of eligible (candidates + impacted).
	eligible := len(candidates) + len(impacted)
	if eligible > 0 && float32(len(candidates))/float32(eligible) < 0.5 {
		return nil
	}

	// Get GPU model spec for FB capacity
	spec := MatchGPUModel(group.GPUModel)
	var totalFBMiB float32
	if spec != nil {
		totalFBMiB = float32(spec.TotalFBMiB)
	}

	avgSM, avgDRAM, avgFBFrac := avgCandidateUtilization(candidates, totalFBMiB)

	replicas, ok := computeReplicas(avgSM, avgDRAM, avgFBFrac)
	if !ok {
		return nil
	}

	// Average confidence of candidate containers
	var sumConf float32
	for _, c := range candidates {
		sumConf += c.Rec.Confidence
	}
	avgCandConf := sumConf / float32(len(candidates))

	perGPU, totalSavings := computeTimeslicingSavings(replicas, len(candidates), gpuRate)
	confidence := computeTimeslicingConfidence(avgCandConf, len(impacted), eligible)

	rec := &TimeslicingRec{
		NodeName:            group.NodeName,
		ClusterUUID:         group.ClusterUUID,
		GPUModel:            group.GPUModel,
		RecommendedReplicas: replicas,
		SavingsPerGPU:       perGPU,
		TotalNodeSavings:    totalSavings,
		Confidence:          confidence,
		NotificationCodes:   []int16{NotifGPUTimeSharingCandidate},
	}

	for _, c := range candidates {
		rec.CandidateContainers = append(rec.CandidateContainers, GPUContainerRef{
			Namespace:      c.Namespace,
			Workload:       c.Workload,
			Container:      c.Container,
			SMActiveAvg:    c.Rec.SMActiveAvg,
			Classification: c.Rec.Classification,
		})
		c.Rec.NotificationCodes = append(c.Rec.NotificationCodes, NotifGPUTimeSharingCandidate)
		c.Rec.TimeSlicingNode = group.NodeName
		c.Rec.TimeSlicingReplicas = replicas
	}
	for _, c := range impacted {
		rec.ImpactedContainers = append(rec.ImpactedContainers, GPUContainerRef{
			Namespace:      c.Namespace,
			Workload:       c.Workload,
			Container:      c.Container,
			SMActiveAvg:    c.Rec.SMActiveAvg,
			Classification: c.Rec.Classification,
		})
	}

	return rec
}
