package engine

import (
	"context"

	"github.com/redhatinsights/ros-ocp-backend/librobne/container"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// RecommendWorkloads runs the container recommendation loop with no pool.
// rows must be ordered by container key then BucketDate (same as the digest SELECT).
// emit is called every BatchSize containers (default 500); the batch backing
// array is reused — copy if retaining. ApplySavingsEstimates is a separate call.
func RecommendWorkloads(ctx context.Context, rows []KeyedDigest, cfg EngineConfig, emit EmitContainer) error {
	if emit == nil {
		return nil
	}
	if len(rows) == 0 {
		return nil
	}

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = defaultRecommendBatchSize
	}
	terms := cfg.Terms
	sizingThresholds := cfg.Sizing
	notifThresholds := types.NotificationThresholdsFromSizing(sizingThresholds)
	idleCfg := cfg.Idle
	now := cfg.Now
	stalenessThreshold := cfg.StalenessThreshold
	if stalenessThreshold <= 0 {
		stalenessThreshold = DefaultStalenessThreshold
	}
	clusterLastReported := cfg.ClusterLastReported
	maxIdleWindowDays := types.MaxWindowDays(terms, 0)

	var currentKey ContainerKey
	var currentDigests []DigestRow
	var latestDigestRow DigestRow
	var hasLatestDigest bool
	batch := make([]ContainerRec, 0, batchSize*6)
	containerCount := 0
	firstRow := true

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := emit(batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}

	processContainer := func(key ContainerKey, digests []DigestRow, latest DigestRow) {
		currentCPUReqMC := latest.CPURequestP50MC
		currentCPULimMC := latest.CPURequestP95MC
		currentMemReqKiB := latest.MemRequestP50KiB
		currentMemLimKiB := latest.MemRequestP95KiB
		stale := IsStaleRecommendation(now, latest.BucketDate, clusterLastReported, stalenessThreshold)

		idleRows := digests
		if maxIdleWindowDays > 0 {
			idleLo, idleHi := WindowBounds(digests, latest.BucketDate, maxIdleWindowDays)
			idleRows = digests[idleLo:idleHi]
		}
		idleResult := types.ClassifyIdleState(
			idleRows, currentCPUReqMC, currentMemReqKiB,
			key.WorkloadType, key.Namespace, idleCfg,
		)

		for _, tc := range terms {
			winLo, winHi := WindowBounds(digests, latest.BucketDate, tc.WindowDays)
			windowRows := digests[winLo:winHi]
			if len(windowRows) < tc.MinDataDays {
				continue
			}

			dataDays := len(windowRows)
			confidence := types.ComputeConfidence(dataDays, tc.MinDataDays, tc.WindowDays)
			oomTotal := SumOOMCounts(windowRows)
			pcMin, pcMax, pcAvg := AggregatePodCounts(windowRows)
			desiredReplicas, availableReplicas := LatestReplicaCounts(windowRows)
			monStart := windowRows[0].BucketDate
			monEnd := windowRows[len(windowRows)-1].BucketDate

			cpuCfgCost := types.CPUConfigFromSizing(sizingThresholds, now, tc.DecayHalfLifeHours, "cost")
			cpuCfgPerf := types.CPUConfigFromSizing(sizingThresholds, now, tc.DecayHalfLifeHours, "performance")
			memCfgCost := types.MemoryConfigFromSizing(sizingThresholds, now, tc.DecayHalfLifeHours, cfg.OOMBaseBump, cfg.OOMMaxBump, "cost")
			memCfgPerf := types.MemoryConfigFromSizing(sizingThresholds, now, tc.DecayHalfLifeHours, cfg.OOMBaseBump, cfg.OOMMaxBump, "performance")

			for _, profile := range []string{"cost", "performance"} {
				var cpuCfg CPUConfig
				var memCfg MemoryConfig
				if profile == "performance" {
					cpuCfg = cpuCfgPerf
					memCfg = memCfgPerf
				} else {
					cpuCfg = cpuCfgCost
					memCfg = memCfgCost
				}
				memCfg.OOMCountSum = oomTotal
				if memCfg.OOMMaxBump < 1.0 {
					memCfg.OOMMaxBump = 1.0
				}

				cpuRec, memRec, expl := container.RecommendCPUAndMemory(windowRows, cpuCfg, memCfg)
				expl.DataDays = dataDays

				var recCPUReq, recCPULim, recMemReq, recMemLim int64
				if profile == "performance" {
					recCPUReq = cpuRec.PerfRequestMC
					recCPULim = cpuRec.PerfLimitMC
					recMemReq = memRec.PerfRequestKiB
					recMemLim = memRec.PerfLimitKiB
				} else {
					recCPUReq = cpuRec.CostRequestMC
					recCPULim = cpuRec.CostLimitMC
					recMemReq = memRec.CostRequestKiB
					recMemLim = memRec.CostLimitKiB
				}

				rec := ContainerRec{
					OrgID:                cfg.OrgID,
					ClusterUUID:          cfg.ClusterUUID,
					Namespace:            key.Namespace,
					Workload:             key.Workload,
					WorkloadType:         key.WorkloadType,
					ContainerName:        key.ContainerName,
					Term:                 tc.Name,
					Engine:               profile,
					RecCPURequestMC:      recCPUReq,
					RecCPULimitMC:        recCPULim,
					RecMemRequestKiB:     recMemReq,
					RecMemLimitKiB:       recMemLim,
					CurrentCPURequestMC:  currentCPUReqMC,
					CurrentCPULimitMC:    currentCPULimMC,
					CurrentMemRequestKiB: currentMemReqKiB,
					CurrentMemLimitKiB:   currentMemLimKiB,
					ConfidenceLevel:      confidence,
					CPUTrendSlope:        cpuRec.TrendSlope,
					MemTrendSlope:        memRec.TrendSlope,
					IdleState:            idleResult.State,
					IdleSince:            idleResult.IdleSince,
					IdleDurationDays:     idleResult.DurationDays,
					PeakCPUMC:            idleResult.PeakCPUMC,
					PeakMemoryBytes:      idleResult.PeakMemoryBytes,
					OOMCountSum:          oomTotal,
					DataDays:             dataDays,
					Stale:                stale,
					PodCountMin:          pcMin,
					PodCountMax:          pcMax,
					PodCountAvg:          pcAvg,
					DesiredReplicas:      desiredReplicas,
					AvailableReplicas:    availableReplicas,
					MonitoringStartTime:  monStart,
					MonitoringEndTime:    monEnd,
					Expl:                 expl,
				}
				rec.VariationCPURequestPct = types.ComputeVariation(currentCPUReqMC, rec.RecCPURequestMC)
				rec.VariationCPULimitPct = types.ComputeVariation(currentCPULimMC, rec.RecCPULimitMC)
				rec.VariationMemRequestPct = types.ComputeVariation(currentMemReqKiB, rec.RecMemRequestKiB)
				rec.VariationMemLimitPct = types.ComputeVariation(currentMemLimKiB, rec.RecMemLimitKiB)
				rec.NotificationCodes = container.EvaluateNotificationsWithThresholds(rec, tc.MinDataDays, notifThresholds)

				if cat := types.CategoryFromIdleState(idleResult.State); cat != "" {
					rec.Category = cat
				} else {
					rec.CategoryCPU = types.ClassifyResource(rec.VariationCPURequestPct)
					rec.CategoryMemory = types.ClassifyResource(rec.VariationMemRequestPct)
					rec.Category = types.ClassifyOverall(rec.CategoryCPU, rec.CategoryMemory)
				}

				container.ComputeRecommendedReplicas(&rec, tc.ReplicaTargetUtilizationPct, latest)

				batch = append(batch, rec)
			}
		}
	}

	for _, rk := range rows {
		if !firstRow && rk.Key != currentKey {
			processContainer(currentKey, currentDigests, latestDigestRow)
			containerCount++
			currentDigests = currentDigests[:0]
			hasLatestDigest = false

			if containerCount%batchSize == 0 {
				if ctx != nil {
					if err := ctx.Err(); err != nil {
						return err
					}
				}
				if err := flush(); err != nil {
					return err
				}
			}
		}

		firstRow = false
		currentKey = rk.Key
		currentDigests = append(currentDigests, rk.Row)
		if !hasLatestDigest || rk.Row.BucketDate.After(latestDigestRow.BucketDate) {
			latestDigestRow = rk.Row
			hasLatestDigest = true
		}
	}

	if len(currentDigests) > 0 {
		processContainer(currentKey, currentDigests, latestDigestRow)
	}
	return flush()
}
