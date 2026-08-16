package quota

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
	libquota "github.com/redhatinsights/ros-ocp-backend/librobne/quota"
)

// QuotaRecConfigFromApp builds quota config from application settings.
func QuotaRecConfigFromApp(cfg *config.Config) QuotaRecConfig {
	headroomBP := 10000 + cfg.QuotaHeadroomPercent*100
	if headroomBP < 10000 {
		headroomBP = 10000
	}
	return QuotaRecConfig{
		HeadroomBasisPoints:   headroomBP,
		HighRiskThresholdBP:   cfg.QuotaHighRiskThresholdPercent * 100,
		MediumRiskThresholdBP: cfg.QuotaMediumRiskThresholdPercent * 100,
		Currency:              money.DefaultCurrency,
	}
}

// RecommendQuotas reads quota digests and produces per-namespace recommendations.
func RecommendQuotas(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, cfg QuotaRecConfig) ([]QuotaRec, error) {
	snapshots, err := QueryLatestNamespaceQuotaSnapshots(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return nil, err
	}
	if len(snapshots) == 0 {
		return nil, nil
	}

	aggregates, err := QueryContainerQuotaAggregates(ctx, pool, orgID, clusterUUID, QuotaContainerTerm, QuotaContainerEngine)
	if err != nil {
		return nil, err
	}

	return libquota.RecommendQuotas(snapshots, aggregates, orgID, clusterUUID, cfg), nil
}

func utilizationBP(used, hard int64) *int {
	return UtilizationBP(used, hard)
}

// ApplyQuotaSavings computes estimated monthly savings in cents from freed capacity.
// hoursPerMonth should be HoursInMonth(year, month) for the target calendar month.
func ApplyQuotaSavings(recs []QuotaRec, costData *costdata.ClusterCostData, hoursPerMonth int64) {
	if costData == nil {
		return
	}
	cpuRate := core.RateMicroCentsPerMCHour(costdata.CPUCoreHourlyRate(costData))
	memRate := core.RateMicroCentsPerGiBHour(costdata.MemoryGBHourlyRate(costData))
	storageRate := core.RateMicroCentsPerGiBMonth(costdata.StorageRequestPerMonth(costData))

	for i := range recs {
		if recs[i].RecommendationType != QuotaRecTypeTighten {
			continue
		}
		savingsMicroCents := core.QuotaTightenSavingsMicroCents(
			recs[i].CapacityFreed.CPUMillicores,
			recs[i].CapacityFreed.MemoryBytes,
			recs[i].CapacityFreed.StorageBytes,
			cpuRate, memRate, storageRate, hoursPerMonth,
		)
		recs[i].EstimatedSavingsCents = core.MicroCentsToCents(savingsMicroCents)
	}
}

// QueryLatestNamespaceQuotaSnapshots returns the newest digest row per namespace (schedule_type=all).
func QueryLatestNamespaceQuotaSnapshots(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) ([]NamespaceQuotaSnapshot, error) {
	query := `
		SELECT DISTINCT ON (namespace, quota_name)
			namespace, quota_name,
			cpu_request_hard, cpu_limit_hard,
			memory_request_hard, memory_limit_hard,
			cpu_request_used, cpu_limit_used,
			memory_request_used, memory_limit_used,
			storage_request_hard, storage_request_used,
			pods_hard, pods_used,
			object_count_hard, object_count_used,
			report_date
		FROM daily_namespace_quota_digests
		WHERE org_id = $1 AND cluster_uuid = $2::uuid
			AND (
				cpu_request_hard IS NOT NULL OR cpu_limit_hard IS NOT NULL OR
				memory_request_hard IS NOT NULL OR memory_limit_hard IS NOT NULL OR
				storage_request_hard IS NOT NULL OR pods_hard IS NOT NULL OR object_count_hard IS NOT NULL
			)
		ORDER BY namespace, quota_name, report_date DESC`

	rows, err := pool.Query(ctx, query, orgID, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("query namespace quota snapshots: %w", err)
	}
	defer rows.Close()

	var out []NamespaceQuotaSnapshot
	for rows.Next() {
		var s NamespaceQuotaSnapshot
		var bucketDate time.Time
		var cpuReqHard, cpuLimHard, memReqHard, memLimHard *int64
		var cpuReqUsed, cpuLimUsed, memReqUsed, memLimUsed *int64
		var storageHard, storageUsed, podsHard, podsUsed, objHard, objUsed *int64
		if err := rows.Scan(
			&s.Namespace, &s.QuotaName,
			&cpuReqHard, &cpuLimHard, &memReqHard, &memLimHard,
			&cpuReqUsed, &cpuLimUsed, &memReqUsed, &memLimUsed,
			&storageHard, &storageUsed, &podsHard, &podsUsed, &objHard, &objUsed,
			&bucketDate,
		); err != nil {
			return nil, fmt.Errorf("scan namespace quota snapshot: %w", err)
		}
		s.CPURequestHardMC = derefInt64(cpuReqHard)
		s.CPULimitHardMC = derefInt64(cpuLimHard)
		s.MemoryRequestHardBytes = derefInt64(memReqHard)
		s.MemoryLimitHardBytes = derefInt64(memLimHard)
		s.CPURequestUsedMC = derefInt64(cpuReqUsed)
		s.CPULimitUsedMC = derefInt64(cpuLimUsed)
		s.MemoryRequestUsedBytes = derefInt64(memReqUsed)
		s.MemoryLimitUsedBytes = derefInt64(memLimUsed)
		s.StorageRequestHardBytes = derefInt64(storageHard)
		s.StorageRequestUsedBytes = derefInt64(storageUsed)
		s.PodsHard = derefInt64(podsHard)
		s.PodsUsed = derefInt64(podsUsed)
		s.ObjectCountHard = derefInt64(objHard)
		s.ObjectCountUsed = derefInt64(objUsed)
		s.LastObservedAt = bucketDate
		out = append(out, s)
	}
	return out, rows.Err()
}

func derefInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// QueryContainerQuotaAggregates sums container recommendation requests per namespace.
//
// Data comes from recommendation_sets (term=medium, engine=cost), not from in-memory
// container plugin output. Rows are written in processContainerCSVNative after digest
// ingest; the quota plugin runs on namespace CSV ingest and again after container recs
// in the same payload. If a report has only namespace CSV, aggregates reflect the
// previous ingestion cycle until container metrics arrive (one-cycle lag on deploy).
func QueryContainerQuotaAggregates(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, term, engine string) (map[string]ContainerQuotaAggregate, error) {
	if term == "" {
		term = QuotaContainerTerm
	}
	if engine == "" {
		engine = QuotaContainerEngine
	}
	query := `
		SELECT namespace,
			COALESCE(SUM(rec_cpu_request_millicores), 0),
			COALESCE(SUM(rec_cpu_limit_millicores), 0),
			COALESCE(SUM(rec_memory_request_kib), 0) * 1024,
			COALESCE(SUM(rec_memory_limit_kib), 0) * 1024
		FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid
			AND term = $3 AND engine = $4
		GROUP BY namespace`

	rows, err := pool.Query(ctx, query, orgID, clusterUUID, term, engine)
	if err != nil {
		return nil, fmt.Errorf("query container quota aggregates: %w", err)
	}
	defer rows.Close()

	out := make(map[string]ContainerQuotaAggregate)
	for rows.Next() {
		var ns string
		var agg ContainerQuotaAggregate
		if err := rows.Scan(&ns, &agg.CPURequestSumMC, &agg.CPULimitSumMC, &agg.MemoryRequestSumBytes, &agg.MemoryLimitSumBytes); err != nil {
			return nil, fmt.Errorf("scan container quota aggregate: %w", err)
		}
		out[ns] = agg
	}
	return out, rows.Err()
}

// WriteQuotaRecommendations upserts quota recommendations for a cluster.
func WriteQuotaRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []QuotaRec) error {
	return pgrec.WriteQuotaRecommendations(ctx, pool, recs)
}

func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
