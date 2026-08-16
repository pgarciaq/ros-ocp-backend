package gpu

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// ComputeAndPersistNodeGPUTimeSlicingRecs computes node GPU time-slicing recommendations
// for a cluster and persists live rows, history, and recommendation_sets cross-references.
func ComputeAndPersistNodeGPUTimeSlicingRecs(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	terms []core.TermConfig,
	costData *costdata.ClusterCostData,
) error {
	t0 := time.Now()
	defer func() { metrics.ObserveDB("persist_node_gpu_timeslicing_recs", t0) }()

	now := time.Now().UTC()
	start := now.AddDate(0, 0, -core.MaxWindowDays(terms, 30))

	validTerms := make([]string, len(terms))
	for i, tc := range terms {
		validTerms[i] = tc.Name
	}

	settings, err := ResolveGPUThresholdSettings(ctx, pool, orgID)
	if err != nil {
		settings = CurrentGPUThresholdSettings()
	}

	var gpuRate *float32
	if costData != nil {
		if rate := GPUMonthlyRate(costData); rate > 0 {
			r := float32(rate)
			gpuRate = &r
		}
	}

	gpuRecs, nodeMap, nodeLastSeen, err := QueryGPURecommendations(ctx, pool, orgID, clusterUUID, start, now, terms, nil)
	if err != nil {
		return fmt.Errorf("query GPU recommendations for time-slicing persist: %w", err)
	}

	groups := GroupGPURecsByNodeAndModel(gpuRecs, nodeMap, nodeLastSeen, clusterUUID)
	recs := make([]*TimeslicingRec, 0, len(groups))
	for _, group := range groups {
		if tsRec := ComputeNodeTimeslicingRecWithSettings(group, gpuRate, now, settings); tsRec != nil {
			recs = append(recs, tsRec)
		}
	}

	if err := pgrec.WriteNodeGPUTimeslicingRecs(ctx, pool, orgID, clusterUUID, derefTimeslicing(recs), validTerms, nodeLastSeen); err != nil {
		return err
	}

	logging.ForOrg(orgID, clusterUUID).Infof(
		"ComputeAndPersistNodeGPUTimeSlicingRecs: upserted %d recs", len(recs),
	)
	return nil
}

func derefTimeslicing(recs []*TimeslicingRec) []TimeslicingRec {
	out := make([]TimeslicingRec, 0, len(recs))
	for _, rec := range recs {
		if rec != nil {
			out = append(out, *rec)
		}
	}
	return out
}

func UpsertNodeGPUTimeslicingRec(
	ctx context.Context,
	tx pgx.Tx,
	orgID, clusterUUID string,
	rec *TimeslicingRec,
	lastSeenAt *time.Time,
) error {
	if rec == nil {
		return fmt.Errorf("upsert node GPU time-slicing rec: nil rec")
	}
	return pgrec.UpsertNodeGPUTimeslicingRec(ctx, tx, orgID, clusterUUID, *rec, lastSeenAt)
}

func AppendNodeGPUTimeslicingHistory(
	ctx context.Context,
	tx pgx.Tx,
	orgID, clusterUUID string,
	recs []*TimeslicingRec,
) error {
	return pgrec.AppendNodeGPUTimeslicingHistory(ctx, tx, orgID, clusterUUID, derefTimeslicing(recs))
}

func Float32USDCentsPtr(v *float32) *int64 {
	return pgrec.Float32USDCentsPtr(v)
}
