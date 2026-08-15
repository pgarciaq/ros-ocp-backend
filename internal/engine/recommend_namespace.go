package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	libns "github.com/redhatinsights/ros-ocp-backend/librobne/namespace"
)

type NamespaceKey = libns.NamespaceKey
type namespaceKey = libns.NamespaceKey
type NamespaceEngineConfig = libns.NamespaceEngineConfig
type NamespaceRec = libns.NamespaceRec

// DefaultNamespaceEngineConfig builds a pool-free config from compiled defaults.
// Tests use this; production wrappers overlay tenant terms and thresholds.
func DefaultNamespaceEngineConfig(orgID, clusterUUID string, now, end time.Time) NamespaceEngineConfig {
	cfg := libns.DefaultNamespaceEngineConfig(orgID, clusterUUID, now, end)
	cfg.Terms = DefaultTerms()
	cfg.Sizing = DefaultNamespaceSizingThresholds()
	return cfg
}

// RecommendNamespaces runs the namespace recommendation loop with no pool.
// grouped is namespace → digest rows ordered by BucketDate (same as the digest SELECT).
// ApplyNamespaceSavingsEstimates is a separate call after this returns.
func RecommendNamespaces(ctx context.Context, grouped map[namespaceKey][]DigestRow, cfg NamespaceEngineConfig) ([]NamespaceRec, error) {
	return libns.RecommendNamespaces(ctx, grouped, cfg)
}

// RecommendAllNamespaces reads all-hours namespace digests for an org+cluster,
// groups by namespace, computes recommendations for all terms x engines, and returns results.
func RecommendAllNamespaces(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
) ([]NamespaceRec, error) {
	return recommendNamespaces(ctx, pool, orgID, clusterUUID, start, end, digestScheduleAllHours, nil)
}

// RecommendBusinessHoursNamespaces computes namespace recommendations from the
// business_hours digest stream for namespaces with an enabled business-hours schedule.
func RecommendBusinessHoursNamespaces(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
) ([]NamespaceRec, error) {
	if !config.BusinessHoursFeatureEnabled() {
		return nil, nil
	}

	cache, err := LoadSchedules(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("load business hours schedules: %w", err)
	}
	if cache == nil || !cache.HasAnyEnabled() {
		return nil, nil
	}

	allow := func(namespace string) bool {
		return cache.Resolve(namespace).Enabled
	}
	return recommendNamespaces(ctx, pool, orgID, clusterUUID, start, end, digestScheduleBusinessHours, allow)
}

func recommendNamespaces(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
	scheduleType string,
	namespaceAllow func(string) bool,
) ([]NamespaceRec, error) {
	if scheduleType == "" {
		scheduleType = digestScheduleAllHours
	}

	terms, err := LoadTermConfigCached(ctx, pool, orgID, "namespace")
	if err != nil {
		return nil, fmt.Errorf("load term config: %w", err)
	}

	sizingThresholds, err := ResolveNamespaceSizingThresholds(ctx, pool, orgID)
	if err != nil {
		return nil, fmt.Errorf("load namespace thresholds: %w", err)
	}

	grouped, err := queryNamespaceDigestsByScheduleType(ctx, pool, orgID, clusterUUID, start, end, scheduleType)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	return RecommendNamespaces(ctx, grouped, NamespaceEngineConfig{
		OrgID:               orgID,
		ClusterUUID:         clusterUUID,
		End:                 end,
		ScheduleType:        scheduleType,
		Terms:               terms,
		Sizing:              sizingThresholds,
		Now:                 now,
		StalenessThreshold:  StalenessThreshold(),
		ClusterLastReported: loadClusterLastReportedAt(ctx, pool, orgID, clusterUUID),
		NamespaceAllow:      namespaceAllow,
	})
}

// WriteNamespaceRecommendations batch-upserts NamespaceRec results into
// namespace_recommendation_sets using the native relational columns.
func WriteNamespaceRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []NamespaceRec) error {
	if len(recs) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, r := range recs {
		namespaceID := model.NativeNamespaceID(r.ClusterUUID, r.Namespace)
		scheduleType := r.ScheduleType
		if scheduleType == "" {
			scheduleType = digestScheduleAllHours
		}
		batch.Queue(`
			INSERT INTO namespace_recommendation_sets (
				org_id, cluster_uuid, namespace_name,
				term, engine, namespace_id, schedule_type,
				rec_cpu_request_millicores, rec_cpu_limit_millicores,
				rec_memory_request_kib, rec_memory_limit_kib,
				current_cpu_request_millicores, current_cpu_limit_millicores,
				current_memory_request_kib, current_memory_limit_kib,
				variation_cpu_request_pct, variation_cpu_limit_pct,
				variation_memory_request_pct, variation_memory_limit_pct,
				notification_codes, confidence_level, stale,
				monitoring_start_time, monitoring_end_time,
				estimated_savings_cents, estimated_cpu_savings_cents, estimated_memory_savings_cents,
				category, category_cpu, category_memory,`+containerExplSQLColumns+`, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7::digest_schedule_type,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,`+containerExplValuePlaceholders(31)+`, now())
			ON CONFLICT (org_id, cluster_uuid, namespace_name, term, engine, schedule_type)
			  WHERE term IS NOT NULL
			DO UPDATE SET
				rec_cpu_request_millicores = EXCLUDED.rec_cpu_request_millicores,
				rec_cpu_limit_millicores = EXCLUDED.rec_cpu_limit_millicores,
				rec_memory_request_kib = EXCLUDED.rec_memory_request_kib,
				rec_memory_limit_kib = EXCLUDED.rec_memory_limit_kib,
				current_cpu_request_millicores = EXCLUDED.current_cpu_request_millicores,
				current_cpu_limit_millicores = EXCLUDED.current_cpu_limit_millicores,
				current_memory_request_kib = EXCLUDED.current_memory_request_kib,
				current_memory_limit_kib = EXCLUDED.current_memory_limit_kib,
				variation_cpu_request_pct = EXCLUDED.variation_cpu_request_pct,
				variation_cpu_limit_pct = EXCLUDED.variation_cpu_limit_pct,
				variation_memory_request_pct = EXCLUDED.variation_memory_request_pct,
				variation_memory_limit_pct = EXCLUDED.variation_memory_limit_pct,
				notification_codes = EXCLUDED.notification_codes,
				confidence_level = EXCLUDED.confidence_level,
				stale = EXCLUDED.stale,
				namespace_id = EXCLUDED.namespace_id,
				monitoring_start_time = EXCLUDED.monitoring_start_time,
				monitoring_end_time = EXCLUDED.monitoring_end_time,
				estimated_savings_cents = EXCLUDED.estimated_savings_cents,
				estimated_cpu_savings_cents = EXCLUDED.estimated_cpu_savings_cents,
				estimated_memory_savings_cents = EXCLUDED.estimated_memory_savings_cents,
				category = EXCLUDED.category,
				category_cpu = EXCLUDED.category_cpu,
				category_memory = EXCLUDED.category_memory,`+containerExplUpdateSet+`,
				updated_at = now()`,
			appendContainerExplArgs([]any{
				r.OrgID, r.ClusterUUID, r.Namespace,
				r.Term, r.Engine, namespaceID, scheduleType,
				r.RecCPURequestMC, r.RecCPULimitMC,
				r.RecMemRequestKiB, r.RecMemLimitKiB,
				r.CurrentCPURequestMC, r.CurrentCPULimitMC,
				r.CurrentMemRequestKiB, r.CurrentMemLimitKiB,
				r.VariationCPURequestPct, r.VariationCPULimitPct,
				r.VariationMemRequestPct, r.VariationMemLimitPct,
				r.NotificationCodes, r.ConfidenceLevel, r.Stale,
				r.MonitoringStartTime, r.MonitoringEndTime,
				r.EstimatedSavingsCents, r.EstimatedCPUSavingsCents, r.EstimatedMemSavingsCents,
				nullIfEmpty(r.Category), nullIfEmpty(r.CategoryCPU), nullIfEmpty(r.CategoryMemory),
			}, r.Expl)...,
		)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for range recs {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("namespace rec batch exec: %w", err)
		}
	}
	return nil
}

// WriteNamespaceRecommendationHistory batch-inserts namespace recommendation
// snapshots into historical_namespace_recommendation_sets using native
// relational columns (no JSONB).
func WriteNamespaceRecommendationHistory(ctx context.Context, pool *pgxpool.Pool, recs []NamespaceRec) error {
	if len(recs) == 0 {
		return nil
	}

	now := time.Now().UTC()
	batch := &pgx.Batch{}

	for _, r := range recs {
		namespaceID := model.NativeNamespaceID(r.ClusterUUID, r.Namespace)
		scheduleType := r.ScheduleType
		if scheduleType == "" {
			scheduleType = digestScheduleAllHours
		}
		batch.Queue(`
			INSERT INTO historical_namespace_recommendation_sets (
				org_id, cluster_uuid, namespace_name, namespace_id,
				term, engine, schedule_type,
				rec_cpu_request_millicores, rec_cpu_limit_millicores,
				rec_memory_request_kib, rec_memory_limit_kib,
				current_cpu_request_millicores, current_cpu_limit_millicores,
				current_memory_request_kib, current_memory_limit_kib,
				variation_cpu_request_pct, variation_cpu_limit_pct,
				variation_memory_request_pct, variation_memory_limit_pct,
				notification_codes, confidence_level,
				monitoring_start_time, monitoring_end_time,
				created_at, updated_at,`+containerExplSQLColumns+`
			) VALUES ($1,$2,$3,$4,$5,$6,$7::digest_schedule_type,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$24,`+containerExplValuePlaceholders(25)+`)
			ON CONFLICT (org_id, cluster_uuid, namespace_name, term, engine, schedule_type, created_at)
			  WHERE term IS NOT NULL
			DO UPDATE SET
				rec_cpu_request_millicores = EXCLUDED.rec_cpu_request_millicores,
				rec_cpu_limit_millicores = EXCLUDED.rec_cpu_limit_millicores,
				rec_memory_request_kib = EXCLUDED.rec_memory_request_kib,
				rec_memory_limit_kib = EXCLUDED.rec_memory_limit_kib,
				notification_codes = EXCLUDED.notification_codes,
				confidence_level = EXCLUDED.confidence_level,
				updated_at = EXCLUDED.updated_at,`+containerExplUpdateSet,
			appendContainerExplArgs([]any{
				r.OrgID, r.ClusterUUID, r.Namespace, namespaceID,
				r.Term, r.Engine, scheduleType,
				r.RecCPURequestMC, r.RecCPULimitMC,
				r.RecMemRequestKiB, r.RecMemLimitKiB,
				r.CurrentCPURequestMC, r.CurrentCPULimitMC,
				r.CurrentMemRequestKiB, r.CurrentMemLimitKiB,
				r.VariationCPURequestPct, r.VariationCPULimitPct,
				r.VariationMemRequestPct, r.VariationMemLimitPct,
				r.NotificationCodes, r.ConfidenceLevel,
				r.MonitoringStartTime, r.MonitoringEndTime,
				now,
			}, r.Expl)...,
		)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for range recs {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("WriteNamespaceRecommendationHistory batch exec: %w", err)
		}
	}
	return nil
}

// ApplyNamespaceSavingsEstimates computes EstimatedSavingsCents for each
// namespace recommendation using cost data from Koku. hoursPerMonth should be
// HoursInMonth(year, month) for the target calendar month. If costData is nil,
// NotifNoCostData is appended and savings remain nil.
func ApplyNamespaceSavingsEstimates(recs []NamespaceRec, costData *costdata.ClusterCostData, hoursPerMonth int64) {
	if costData == nil {
		for i := range recs {
			recs[i].NotificationCodes = appendUnique(recs[i].NotificationCodes, NotifNoCostData)
		}
		return
	}

	distType := costData.DistributionType
	if distType == "" {
		distType = "cpu"
	}

	for i := range recs {
		ns, ok := costData.Namespaces[recs[i].Namespace]
		if !ok {
			recs[i].NotificationCodes = appendUnique(recs[i].NotificationCodes, NotifNoCostData)
			continue
		}

		cpuDeltaMC := recs[i].CurrentCPURequestMC - recs[i].RecCPURequestMC
		memDeltaKiB := recs[i].CurrentMemRequestKiB - recs[i].RecMemRequestKiB

		modelCPURate := EffectiveRateMicroCentsPerMCHour(ns.CostModelCPUCost, ns.CPURequestHours)
		modelMemRate := EffectiveRateMicroCentsPerGiBHour(ns.CostModelMemCost, ns.MemRequestHours)

		cpuMicro := CPUSavingsMicroCents(cpuDeltaMC, modelCPURate, hoursPerMonth, 1)
		memMicro := MemSavingsMicroCentsFromKiB(memDeltaKiB, modelMemRate, hoursPerMonth, 1)

		totalInfraUSD := clampNonNegativeUSD(ns.InfraCost + ns.DistributedCost)
		if distType == "memory" {
			infraRate := EffectiveRateMicroCentsPerGiBHour(totalInfraUSD, ns.MemRequestHours)
			memMicro += MemSavingsMicroCentsFromKiB(memDeltaKiB, infraRate, hoursPerMonth, 1)
		} else {
			infraRate := EffectiveRateMicroCentsPerMCHour(totalInfraUSD, ns.CPURequestHours)
			cpuMicro += CPUSavingsMicroCents(cpuDeltaMC, infraRate, hoursPerMonth, 1)
		}

		total := MicroCentsToCents(cpuMicro + memMicro)
		cpuCents := MicroCentsToCents(cpuMicro)
		memCents := MicroCentsToCents(memMicro)
		recs[i].EstimatedSavingsCents = &total
		recs[i].EstimatedCPUSavingsCents = &cpuCents
		recs[i].EstimatedMemSavingsCents = &memCents
	}
}

// EvaluateNamespaceNotifications produces notification codes for a namespace recommendation.
func EvaluateNamespaceNotifications(rec NamespaceRec) []int16 {
	return libns.EvaluateNamespaceNotificationsWithThresholds(rec, NotificationThresholdsFromSizing(defaultNamespaceSizingThresholds))
}

// EvaluateNamespaceNotificationsWithThresholds produces namespace notification codes using explicit thresholds.
func EvaluateNamespaceNotificationsWithThresholds(rec NamespaceRec, th NotificationThresholds) []int16 {
	return libns.EvaluateNamespaceNotificationsWithThresholds(rec, th)
}
