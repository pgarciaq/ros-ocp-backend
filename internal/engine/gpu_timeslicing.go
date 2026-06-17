package engine

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// NotifGPUTimeSharingCandidate is emitted on containers and nodes where
	// GPU time-slicing is recommended. Code 36 (code 29 is NotifPVCOversized).
	NotifGPUTimeSharingCandidate int16 = 36

	// NodeGPUFreshnessDays is the compiled default node telemetry freshness window.
	NodeGPUFreshnessDays = 7
)

// TimeslicingRec holds the time-slicing recommendation for a single node × GPU model × term.
type TimeslicingRec struct {
	NodeName            string
	ClusterUUID         string
	GPUModel            string
	Term                string
	RecommendedReplicas int
	SavingsPerGPU       *float32
	TotalNodeSavings    *float32
	Confidence          float32
	CandidateContainers []GPUContainerRef
	ImpactedContainers  []GPUContainerRef
	NotificationCodes   []int16
	Expl                NodeGPUTimeslicingExplanationFactors
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
// on the same node with the same GPU model within a single term.
type NodeGPUGroup struct {
	NodeName    string
	ClusterUUID string
	GPUModel    string
	Term        string
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
func computeReplicas(avgSM, avgDRAM, avgFBFrac float32, settings GPUThresholdSettings) (int, bool) {
	peak := avgSM
	if avgDRAM > peak {
		peak = avgDRAM
	}
	if avgFBFrac > peak {
		peak = avgFBFrac
	}
	if peak <= 0 {
		return settings.TimeslicingMaxReplicas, true
	}
	r := int(1.0 / peak)
	if r < settings.TimeslicingMinReplicas {
		return 0, false
	}
	if r > settings.TimeslicingMaxReplicas {
		r = settings.TimeslicingMaxReplicas
	}
	return r, true
}

// computeTimeslicingConfidence computes confidence for a time-slicing recommendation.
func computeTimeslicingConfidence(avgCandidateConf float32, nImpacted, nTotal int, settings GPUThresholdSettings) float32 {
	if nTotal == 0 {
		return 0
	}
	impactedRatio := float32(nImpacted) / float32(nTotal)
	basePenalty := float32(settings.TimeslicingBasePenalty)
	impactedWeight := float32(settings.TimeslicingImpactedWeight)
	return avgCandidateConf * basePenalty * (1.0 - impactedWeight*impactedRatio)
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
func isNodeFresh(lastSeen, now time.Time, freshnessDays int) bool {
	return now.Sub(lastSeen) <= time.Duration(freshnessDays)*24*time.Hour
}

// ComputeNodeTimeslicingRec produces a time-slicing recommendation for a single
// node × GPU model group using process-wide default GPU thresholds.
// Prefer ComputeNodeTimeslicingRecForOrg when org-specific settings are available.
func ComputeNodeTimeslicingRec(group NodeGPUGroup, gpuRate *float32, now time.Time) *TimeslicingRec {
	return ComputeNodeTimeslicingRecWithSettings(group, gpuRate, now, defaultGPUThresholdSettings)
}

// ComputeNodeTimeslicingRecForOrg resolves per-org GPU thresholds (including time-slicing
// parameters) and produces a time-slicing recommendation for a single node × GPU model group.
func ComputeNodeTimeslicingRecForOrg(ctx context.Context, pool *pgxpool.Pool, orgID string, group NodeGPUGroup, gpuRate *float32, now time.Time) *TimeslicingRec {
	settings, err := ResolveGPUThresholdSettings(ctx, pool, orgID)
	if err != nil {
		settings = defaultGPUThresholdSettings
	}
	return ComputeNodeTimeslicingRecWithSettings(group, gpuRate, now, settings)
}

// ComputeNodeTimeslicingRecWithSettings produces a time-slicing recommendation using explicit settings.
func ComputeNodeTimeslicingRecWithSettings(group NodeGPUGroup, gpuRate *float32, now time.Time, settings GPUThresholdSettings) *TimeslicingRec {
	if len(group.Containers) == 0 {
		return nil
	}

	if !group.LastSeen.IsZero() && !isNodeFresh(group.LastSeen, now, settings.NodeFreshnessDays) {
		return nil
	}

	candidates, impacted := partitionContainers(group.Containers)
	if len(candidates) == 0 {
		return nil
	}

	eligible := len(candidates) + len(impacted)
	if eligible > 0 && float32(len(candidates))/float32(eligible) < float32(settings.TimeslicingMajorityThreshold) {
		return nil
	}

	spec := MatchGPUModel(group.GPUModel)
	var totalFBMiB float32
	if spec != nil {
		totalFBMiB = float32(spec.TotalFBMiB)
	}

	avgSM, avgDRAM, avgFBFrac := avgCandidateUtilization(candidates, totalFBMiB)

	replicas, ok := computeReplicas(avgSM, avgDRAM, avgFBFrac, settings)
	if !ok {
		return nil
	}

	var sumConf float32
	for _, c := range candidates {
		sumConf += c.Rec.Confidence
	}
	avgCandConf := sumConf / float32(len(candidates))

	perGPU, totalSavings := computeTimeslicingSavings(replicas, len(candidates), gpuRate)
	confidence := computeTimeslicingConfidence(avgCandConf, len(impacted), eligible, settings)

	rec := &TimeslicingRec{
		NodeName:            group.NodeName,
		ClusterUUID:         group.ClusterUUID,
		GPUModel:            group.GPUModel,
		Term:                group.Term,
		RecommendedReplicas: replicas,
		SavingsPerGPU:       perGPU,
		TotalNodeSavings:    totalSavings,
		Confidence:          confidence,
		NotificationCodes:   []int16{NotifGPUTimeSharingCandidate},
		Expl: NodeGPUTimeslicingExplanationFactors{
			DataDays:           len(group.Containers),
			CandidateCount:     len(candidates),
			ImpactedCount:      len(impacted),
			ClassificationRule: "majority underutilized GPU containers eligible for time-slicing",
		},
	}

	for _, c := range candidates {
		rec.CandidateContainers = append(rec.CandidateContainers, GPUContainerRef{
			Namespace:      c.Namespace,
			Workload:       c.Workload,
			Container:      c.Container,
			SMActiveAvg:    c.Rec.SMActiveAvg,
			Classification: c.Rec.Classification,
		})
		c.Rec.TimeSlicingNode = group.NodeName
		c.Rec.TimeSlicingReplicas = replicas
		c.Rec.NotificationCodes = append(c.Rec.NotificationCodes, NotifGPUTimeSharingCandidate)
		if perGPU != nil {
			savings := *perGPU
			c.Rec.EstimatedTimeslicingSavingsUSD = &savings
		}
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
