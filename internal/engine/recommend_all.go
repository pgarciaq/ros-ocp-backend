package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/clustercache"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	db "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetheatmap"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	libengine "github.com/redhatinsights/ros-ocp-backend/librobne/engine"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgdigest"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// PgxBatchSender is a type alias for db.PgxBatchSender.
type PgxBatchSender = db.PgxBatchSender

type containerKey = core.ContainerKey

// OOMConfig holds configurable OOM bump parameters, typically read from
// environment variables (ROS_OOM_BASE_BUMP, ROS_OOM_MAX_BUMP).
// Zero values cause DefaultMemoryConfig defaults to be used.
type OOMConfig struct {
	BaseBump float64
	MaxBump  float64
}

// streamBatchSize is the number of containers accumulated before emitting a batch (ADR-0171).
const streamBatchSize = 500

// ErrDigestRowCapExceeded is returned when loadDigestRows hits the configured
// ROS_MAX_DIGEST_ROWS_PER_CLUSTER limit. Callers should skip that cluster's
// recommendations rather than crash. The error message includes actionable
// guidance for operators.
var ErrDigestRowCapExceeded = fmt.Errorf("digest row cap exceeded")

// loadDigestRows fetches all digest rows for a cluster in a transaction with the
// ingest statement timeout. Rows are buffered in memory and the database connection
// is released before any recommendation processing begins. This avoids TCP
// backpressure timeouts that occur when long-running recommendation writes block
// the client from consuming a streaming result set (see issue #263).
//
// maxRows caps the number of rows buffered to prevent OOM on anomalous clusters
// (see issue #290). 0 means unlimited. Exceeding the cap is a hard error — the
// caller must skip that cluster rather than process truncated data.
func loadDigestRows(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
	maxRows int,
) ([]KeyedDigest, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin digest read tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := db.SetLocalIngestStatementTimeout(ctx, tx); err != nil {
		return nil, fmt.Errorf("set ingest statement timeout: %w", err)
	}

	warnThreshold := 0
	if maxRows > 0 {
		warnThreshold = maxRows * 4 / 5 // 80% of cap
	}
	warnLogged := false

	const defaultDigestRowCapacity = 8192
	result := make([]KeyedDigest, 0, defaultDigestRowCapacity)
	err = pgdigest.ForEachAllHours(ctx, tx, orgID, clusterUUID, start, end, func(d KeyedDigest) error {
		result = append(result, d)
		count := len(result)
		if maxRows > 0 {
			if count > maxRows {
				metrics.DigestRowsCapExceeded.Inc()
				return fmt.Errorf("%w: loaded %d rows (cap=%d) for cluster %s — "+
					"reduce ROS_MAX_LOOKBACK_DAYS or increase ROS_MAX_DIGEST_ROWS_PER_CLUSTER",
					ErrDigestRowCapExceeded, count, maxRows, clusterUUID)
			}
			if !warnLogged && count >= warnThreshold {
				warnLogged = true
				metrics.DigestRowsCapWarning.Inc()
				logging.GetLogger().Warnf(
					"digest row count at 80%% of cap: org_id=%s cluster=%s count=%d cap=%d",
					orgID, clusterUUID, count, maxRows,
				)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit digest read tx: %w", err)
	}

	return result, nil
}

// RecommendWorkloadsStreaming loads terms/thresholds/idle and digest rows from
// PostgreSQL, then calls RecommendWorkloads (no pool in the compute loop).
// emit is invoked every ~streamBatchSize containers.
func RecommendWorkloadsStreaming(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
	oomCfg OOMConfig,
	emit func([]ContainerRec) error,
) error {
	terms, err := LoadTermConfigCached(ctx, pool, orgID, "container")
	if err != nil {
		return fmt.Errorf("load term config: %w", err)
	}

	sizingThresholds, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
	if err != nil {
		return fmt.Errorf("load container thresholds: %w", err)
	}
	idleCfg := LoadIdleConfig(ctx, pool, orgID)

	maxRows := config.GetConfig().MaxDigestRowsPerCluster
	allRows, err := loadDigestRows(ctx, pool, orgID, clusterUUID, start, end, maxRows)
	if err != nil {
		return err
	}
	logging.GetLogger().Infof("loaded %d digest rows for recommendation (cluster %s)", len(allRows), clusterUUID)

	cfg := EngineConfig{
		OrgID:               orgID,
		ClusterUUID:         clusterUUID,
		Terms:               terms,
		Sizing:              sizingThresholds,
		Idle:                idleCfg,
		OOMBaseBump:         oomCfg.BaseBump,
		OOMMaxBump:          oomCfg.MaxBump,
		Now:                 time.Now().UTC(),
		StalenessThreshold:  StalenessThreshold(),
		ClusterLastReported: loadClusterLastReportedAt(ctx, pool, orgID, clusterUUID),
		BatchSize:           streamBatchSize,
	}
	return RecommendWorkloads(ctx, allRows, cfg, emit)
}

// RecommendAllWorkloads is a convenience wrapper that collects all streaming results
// into a single slice. Prefer RecommendWorkloadsStreaming in production for bounded memory.
func RecommendAllWorkloads(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
	oomCfg OOMConfig,
) ([]ContainerRec, error) {
	var results []ContainerRec
	err := RecommendWorkloadsStreaming(ctx, pool, orgID, clusterUUID, start, end, oomCfg, func(batch []ContainerRec) error {
		results = append(results, batch...)
		return nil
	})
	return results, err
}

// FlushRecommendationBatch delegates to db.FlushRecommendationBatch.
var FlushRecommendationBatch = db.FlushRecommendationBatch

// WriteRecommendations batch-upserts ContainerRec results into recommendation_sets.
func WriteRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []ContainerRec) error {
	if len(recs) == 0 {
		return nil
	}
	t0 := time.Now()
	defer func() { metrics.ObserveDB("write_recommendations", t0) }()
	return pgrec.WriteRecommendations(ctx, pool, recs)
}

// WriteRecommendationsAndRefreshOrg persists recommendations and refreshes org metadata.
// Use for single-batch writes (tests, tooling). Streaming reconcile cycles should call
// WriteRecommendations per batch and RefreshOrgMetadata once at the end.
func WriteRecommendationsAndRefreshOrg(ctx context.Context, pool *pgxpool.Pool, recs []ContainerRec) error {
	if err := WriteRecommendations(ctx, pool, recs); err != nil {
		return err
	}
	if len(recs) == 0 {
		return nil
	}
	return RefreshOrgMetadata(ctx, pool, recs[0].OrgID)
}

// RefreshOrgMetadata updates org_container_keys and org_recommendation_stats for an org.
// Call once at the end of a reconcile cycle instead of after every write batch.
func RefreshOrgMetadata(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	if err := pgrec.RefreshOrgMetadata(ctx, pool, orgID); err != nil {
		return err
	}
	if orgID == "" {
		return nil
	}
	fleetsummary.InvalidateOrg(orgID)
	fleetheatmap.InvalidateOrg(orgID)
	clustercache.InvalidateOrg(orgID)
	return nil
}

var (
	windowBounds          = libengine.WindowBounds
	computeConfidence     = core.ComputeConfidence
	computeVariation      = core.ComputeVariation
	aggregatePodCounts    = libengine.AggregatePodCounts
	latestReplicaCounts   = libengine.LatestReplicaCounts
	sumOOMCounts          = libengine.SumOOMCounts
	isStaleRecommendation = libengine.IsStaleRecommendation
	latestDigest          = libengine.LatestDigest
)

// DefaultStalenessThreshold is used when ROS_STALENESS_THRESHOLD_HOURS is not set.
const DefaultStalenessThreshold = libengine.DefaultStalenessThreshold

// loadClusterLastReportedAt returns clusters.last_reported_at for org+cluster, or zero time if unknown.
func loadClusterLastReportedAt(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) time.Time {
	var ts time.Time
	err := pool.QueryRow(ctx, `
		SELECT c.last_reported_at
		FROM clusters c
		WHERE c.org_id = $1 AND c.cluster_uuid = $2::uuid`,
		orgID, clusterUUID).Scan(&ts)
	if err != nil {
		return time.Time{}
	}
	return ts.UTC()
}

// StalenessThreshold returns the configured staleness threshold duration.
func StalenessThreshold() time.Duration {
	cfg := config.GetConfig()
	if cfg.StalenessThresholdHours > 0 {
		return time.Duration(cfg.StalenessThresholdHours) * time.Hour
	}
	return DefaultStalenessThreshold
}

// MarkUnreportedContainersStale marks recommendation_sets rows stale when their
// composite key no longer appears in the current digest data. This handles the
// case where a container's workload_type (or other key column) changes: the old
// row is never overwritten by the ON CONFLICT upsert (different key = new row),
// so without this sweep the old recommendation lingers with stale=false despite
// having no matching digest data.
//
// The mechanism relies on WriteRecommendations setting updated_at = now() for
// every row it upserts. After a full reconcile cycle, any non-stale row whose
// updated_at is older than cycleStart was not refreshed — its composite key has
// no matching digests.
//
// A 5-minute grace window accounts for clock skew and transaction commit delays.
func MarkUnreportedContainersStale(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, cycleStart time.Time) (int64, error) {
	return pgrec.MarkUnreportedContainersStale(ctx, pool, orgID, clusterUUID, cycleStart)
}
