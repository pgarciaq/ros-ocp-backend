package namespace

import (
	"context"

	"github.com/redhatinsights/ros-ocp-backend/librobne/container"
	"github.com/redhatinsights/ros-ocp-backend/librobne/engine"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// RecommendNamespaces runs the namespace recommendation loop with no pool.
// grouped is namespace → digest rows ordered by BucketDate (same as the digest SELECT).
// ApplyNamespaceSavingsEstimates is a separate call after this returns (product).
func RecommendNamespaces(ctx context.Context, grouped map[NamespaceKey][]types.DigestRow, cfg NamespaceEngineConfig) ([]NamespaceRec, error) {
	scheduleType := cfg.ScheduleType
	if scheduleType == "" {
		scheduleType = ScheduleAllHours
	}
	sizingThresholds := cfg.Sizing
	notifThresholds := types.NotificationThresholdsFromSizing(sizingThresholds)
	now := cfg.Now
	stalenessThreshold := cfg.StalenessThreshold
	if stalenessThreshold <= 0 {
		stalenessThreshold = engine.DefaultStalenessThreshold
	}
	results := make([]NamespaceRec, 0, len(grouped)*2)

	for key, digestRows := range grouped {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if cfg.NamespaceAllow != nil && !cfg.NamespaceAllow(key.Namespace) {
			continue
		}
		latest := engine.LatestDigest(digestRows)
		currentCPUReqMC := latest.CPURequestP50MC
		currentCPULimMC := latest.CPURequestP95MC
		currentMemReqKiB := latest.MemRequestP50KiB
		currentMemLimKiB := latest.MemRequestP95KiB

		stale := engine.IsStaleRecommendation(now, latest.BucketDate, cfg.ClusterLastReported, stalenessThreshold)

		for _, tc := range cfg.Terms {
			winLo, winHi := engine.WindowBounds(digestRows, latest.BucketDate, tc.WindowDays)
			windowRows := digestRows[winLo:winHi]
			if len(windowRows) < tc.MinDataDays {
				continue
			}

			dataDays := len(windowRows)
			confidence := types.ComputeConfidence(dataDays, tc.MinDataDays, tc.WindowDays)
			monStart := latest.BucketDate.AddDate(0, 0, -tc.WindowDays)

			for _, profile := range []string{"cost", "performance"} {
				cpuCfg := types.CPUConfigFromSizing(sizingThresholds, now, tc.DecayHalfLifeHours, profile)
				memCfg := types.MemoryConfigFromSizing(sizingThresholds, now, tc.DecayHalfLifeHours, 0, 0, profile)

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

				rec := NamespaceRec{
					OrgID:                cfg.OrgID,
					ClusterUUID:          cfg.ClusterUUID,
					Namespace:            key.Namespace,
					Term:                 tc.Name,
					Engine:               profile,
					ScheduleType:         scheduleType,
					RecCPURequestMC:      recCPUReq,
					RecCPULimitMC:        recCPULim,
					RecMemRequestKiB:     recMemReq,
					RecMemLimitKiB:       recMemLim,
					CurrentCPURequestMC:  currentCPUReqMC,
					CurrentCPULimitMC:    currentCPULimMC,
					CurrentMemRequestKiB: currentMemReqKiB,
					CurrentMemLimitKiB:   currentMemLimKiB,
					ConfidenceLevel:      confidence,
					MemTrendSlope:        memRec.TrendSlope,
					DataDays:             dataDays,
					Stale:                stale,
					MonitoringStartTime:  monStart,
					MonitoringEndTime:    cfg.End,
					Expl:                 expl,
				}
				rec.VariationCPURequestPct = types.ComputeVariation(currentCPUReqMC, rec.RecCPURequestMC)
				rec.VariationCPULimitPct = types.ComputeVariation(currentCPULimMC, rec.RecCPULimitMC)
				rec.VariationMemRequestPct = types.ComputeVariation(currentMemReqKiB, rec.RecMemRequestKiB)
				rec.VariationMemLimitPct = types.ComputeVariation(currentMemLimKiB, rec.RecMemLimitKiB)
				rec.NotificationCodes = EvaluateNamespaceNotificationsWithThresholds(rec, notifThresholds)

				rec.CategoryCPU = types.ClassifyResource(rec.VariationCPURequestPct)
				rec.CategoryMemory = types.ClassifyResource(rec.VariationMemRequestPct)
				rec.Category = types.ClassifyOverall(rec.CategoryCPU, rec.CategoryMemory)

				results = append(results, rec)
			}
		}
	}

	return results, nil
}
