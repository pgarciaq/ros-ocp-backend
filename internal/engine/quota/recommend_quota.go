package quota

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

const (
	QuotaRecTypeTighten  = "tighten"
	QuotaRecTypeRaise    = "raise"
	QuotaRecTypeOptimal  = "optimal"
	QuotaRecTypeNone     = "none"
	QuotaRiskHigh        = "high"
	QuotaRiskMedium      = "medium"
	QuotaRiskLow         = "low"
	QuotaRiskNone        = "none"
	QuotaContainerTerm   = "medium"
	QuotaContainerEngine = "cost"
)

// QuotaRecConfig holds quota recommendation thresholds (basis points).
type QuotaRecConfig struct {
	HeadroomBasisPoints     int
	HighRiskThresholdBP     int
	MediumRiskThresholdBP   int
}

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
	}
}

// NamespaceQuotaSnapshot is the latest hard/used quota per namespace from digests.
type NamespaceQuotaSnapshot struct {
	Namespace              string
	QuotaName              string
	CPURequestHardMC       int64
	CPULimitHardMC         int64
	MemoryRequestHardBytes int64
	MemoryLimitHardBytes   int64
	CPURequestUsedMC       int64
	CPULimitUsedMC         int64
	MemoryRequestUsedBytes int64
	MemoryLimitUsedBytes   int64
	StorageRequestHardBytes int64
	StorageRequestUsedBytes int64
	PodsHard               int64
	PodsUsed               int64
	ObjectCountHard        int64
	ObjectCountUsed        int64
	LastObservedAt         time.Time
}

// ContainerQuotaAggregate sums container recommendations per namespace.
type ContainerQuotaAggregate struct {
	CPURequestSumMC       int64
	CPULimitSumMC         int64
	MemoryRequestSumBytes int64
	MemoryLimitSumBytes   int64
}

// QuotaRec is the output of the quota recommendation engine.
type QuotaRec struct {
	OrgID        string
	ClusterUUID  string
	Namespace    string
	QuotaName    string
	Snapshot     NamespaceQuotaSnapshot
	Recommended  QuotaResourceBundle
	HeadroomBP   int
	Utilization  QuotaUtilizationBP
	CapacityFreed QuotaCapacityFreed
	EstimatedSavingsCents int64
	Currency              string
	RecommendationType    string
	RiskLevel             string
	NotificationCodes     []int16
	Expl                  core.QuotaExplanationFactors
}

// QuotaResourceBundle holds quota hard, used, or recommended values.
type QuotaResourceBundle struct {
	CPURequestMillicores int64
	CPULimitMillicores   int64
	MemoryRequestBytes   int64
	MemoryLimitBytes     int64
	StorageRequestBytes  int64
	Pods                 int64
}

// QuotaUtilizationBP holds utilization ratios in basis points (used or recommended vs hard).
type QuotaUtilizationBP struct {
	CPURequestBP    *int
	CPULimitBP      *int
	MemoryRequestBP *int
	MemoryLimitBP   *int
	StorageRequestBP *int
	PodsBP          *int
	ObjectCountBP   *int
}

// QuotaCapacityFreed holds capacity that could be reclaimed by tightening quota.
type QuotaCapacityFreed struct {
	CPUMillicores int64
	MemoryBytes   int64
	StorageBytes  int64
	PodsFreed     int64
}

// RecommendQuotas produces per-namespace quota recommendations for a cluster.
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

	var recs []QuotaRec
	for _, snap := range snapshots {
		if !snap.hasHardLimits() {
			continue
		}
		agg := aggregates[snap.Namespace]
		rec := ComputeQuotaRecommendation(orgID, clusterUUID, snap, agg, cfg)
		recs = append(recs, rec)
	}
	return recs, nil
}

func (s NamespaceQuotaSnapshot) hasHardLimits() bool {
	return s.CPURequestHardMC > 0 || s.CPULimitHardMC > 0 ||
		s.MemoryRequestHardBytes > 0 || s.MemoryLimitHardBytes > 0 ||
		s.StorageRequestHardBytes > 0 || s.PodsHard > 0 || s.ObjectCountHard > 0
}

func ComputeQuotaRecommendation(orgID, clusterUUID string, snap NamespaceQuotaSnapshot, agg ContainerQuotaAggregate, cfg QuotaRecConfig) QuotaRec {
	rec := QuotaRec{
		OrgID:       orgID,
		ClusterUUID: clusterUUID,
		Namespace:   snap.Namespace,
		QuotaName:   snap.QuotaName,
		Snapshot:    snap,
		HeadroomBP:  cfg.HeadroomBasisPoints,
		Currency:    money.DefaultCurrency,
	}

	storageRecommended := applyHeadroom(snap.StorageRequestUsedBytes, cfg.HeadroomBasisPoints)
	podsRecommended := applyHeadroom(snap.PodsUsed, cfg.HeadroomBasisPoints)

	rec.Recommended = QuotaResourceBundle{
		CPURequestMillicores: applyHeadroom(agg.CPURequestSumMC, cfg.HeadroomBasisPoints),
		CPULimitMillicores:   applyHeadroom(agg.CPULimitSumMC, cfg.HeadroomBasisPoints),
		MemoryRequestBytes:   applyHeadroom(agg.MemoryRequestSumBytes, cfg.HeadroomBasisPoints),
		MemoryLimitBytes:     applyHeadroom(agg.MemoryLimitSumBytes, cfg.HeadroomBasisPoints),
		StorageRequestBytes:  storageRecommended,
		Pods:                 podsRecommended,
	}

	// Signal C: utilization vs hard uses the greater of quota "used" and container rec sums.
	rec.Utilization = QuotaUtilizationBP{
		CPURequestBP: utilizationBP(maxInt64(snap.CPURequestUsedMC, agg.CPURequestSumMC), snap.CPURequestHardMC),
		CPULimitBP:   utilizationBP(maxInt64(snap.CPULimitUsedMC, agg.CPULimitSumMC), snap.CPULimitHardMC),
		MemoryRequestBP: utilizationBP(
			maxInt64(snap.MemoryRequestUsedBytes, agg.MemoryRequestSumBytes), snap.MemoryRequestHardBytes),
		MemoryLimitBP: utilizationBP(
			maxInt64(snap.MemoryLimitUsedBytes, agg.MemoryLimitSumBytes), snap.MemoryLimitHardBytes),
		StorageRequestBP: utilizationBP(maxInt64(snap.StorageRequestUsedBytes, storageRecommended), snap.StorageRequestHardBytes),
		PodsBP:           utilizationBP(maxInt64(snap.PodsUsed, podsRecommended), snap.PodsHard),
		ObjectCountBP:    utilizationBP(snap.ObjectCountUsed, snap.ObjectCountHard),
	}

	rec.RiskLevel = classifyQuotaRisk(rec.Utilization, cfg)
	rec.RecommendationType, rec.CapacityFreed = classifyQuotaRecommendation(snap, rec.Recommended, rec.Utilization, cfg)
	rec.NotificationCodes = QuotaNotificationCodes(snap, rec)

	signalCPU := maxInt64(snap.CPURequestUsedMC, agg.CPURequestSumMC)
	rec.Expl = core.QuotaExplanationFactors{
		HeadroomBP:           int32(cfg.HeadroomBasisPoints),
		ContainerCPUSumMC:    agg.CPURequestSumMC,
		ContainerMemSumBytes: agg.MemoryRequestSumBytes,
		SignalCCPUUsedMC:     signalCPU,
		MaxUtilizationBP:     int32(maxUtilizationBP(rec.Utilization)),
		RiskLevel:            rec.RiskLevel,
		RecommendationReason: rec.RecommendationType,
	}

	return rec
}

func applyHeadroom(value int64, headroomBP int) int64 {
	if value <= 0 {
		return 0
	}
	return (value * int64(headroomBP)) / 10000
}

func utilizationBP(used, hard int64) *int {
	if hard <= 0 {
		return nil
	}
	bp := int((used * 10000) / hard)
	return &bp
}

func maxUtilizationBP(util QuotaUtilizationBP) int {
	max := 0
	for _, v := range []*int{
		util.CPURequestBP, util.CPULimitBP, util.MemoryRequestBP, util.MemoryLimitBP,
		util.StorageRequestBP, util.PodsBP, util.ObjectCountBP,
	} {
		if v != nil && *v > max {
			max = *v
		}
	}
	return max
}

func classifyQuotaRisk(util QuotaUtilizationBP, cfg QuotaRecConfig) string {
	maxBP := maxUtilizationBP(util)
	switch {
	case maxBP >= cfg.HighRiskThresholdBP:
		return QuotaRiskHigh
	case maxBP >= cfg.MediumRiskThresholdBP:
		return QuotaRiskMedium
	case maxBP > 0:
		return QuotaRiskLow
	default:
		return QuotaRiskNone
	}
}

func classifyQuotaRecommendation(snap NamespaceQuotaSnapshot, recommended QuotaResourceBundle, util QuotaUtilizationBP, cfg QuotaRecConfig) (string, QuotaCapacityFreed) {
	freed := QuotaCapacityFreed{}
	needsRaise := maxUtilizationBP(util) >= cfg.HighRiskThresholdBP

	cpuTighten := snap.CPURequestHardMC > 0 && recommended.CPURequestMillicores > 0 && recommended.CPURequestMillicores < snap.CPURequestHardMC
	memTighten := snap.MemoryRequestHardBytes > 0 && recommended.MemoryRequestBytes > 0 && recommended.MemoryRequestBytes < snap.MemoryRequestHardBytes
	storageTighten := snap.StorageRequestHardBytes > 0 && recommended.StorageRequestBytes > 0 &&
		recommended.StorageRequestBytes < snap.StorageRequestHardBytes
	podsTighten := snap.PodsHard > 0 && recommended.Pods > 0 && recommended.Pods < snap.PodsHard

	if cpuTighten {
		freed.CPUMillicores = snap.CPURequestHardMC - recommended.CPURequestMillicores
	}
	if memTighten {
		freed.MemoryBytes = snap.MemoryRequestHardBytes - recommended.MemoryRequestBytes
	}
	if storageTighten {
		freed.StorageBytes = snap.StorageRequestHardBytes - recommended.StorageRequestBytes
	}
	if podsTighten {
		freed.PodsFreed = snap.PodsHard - recommended.Pods
	}

	if needsRaise {
		return QuotaRecTypeRaise, freed
	}
	if cpuTighten || memTighten || storageTighten || podsTighten {
		return QuotaRecTypeTighten, freed
	}
	if snap.hasHardLimits() {
		return QuotaRecTypeOptimal, freed
	}
	return QuotaRecTypeNone, freed
}

// ApplyQuotaSavings computes estimated monthly savings in cents from freed capacity.
func ApplyQuotaSavings(recs []QuotaRec, costData *costdata.ClusterCostData) {
	if costData == nil {
		return
	}
	cpuRate := core.RateMicroCentsPerMCHour(core.CPUCoreHourlyRate(costData))
	memRate := core.RateMicroCentsPerGiBHour(core.MemoryGBHourlyRate(costData))
	storageRate := core.RateMicroCentsPerGiBMonth(core.StorageRequestPerMonth(costData))

	for i := range recs {
		if recs[i].RecommendationType != QuotaRecTypeTighten {
			continue
		}
		savingsMicroCents := core.QuotaTightenSavingsMicroCents(
			recs[i].CapacityFreed.CPUMillicores,
			recs[i].CapacityFreed.MemoryBytes,
			recs[i].CapacityFreed.StorageBytes,
			cpuRate, memRate, storageRate,
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

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
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
	for _, r := range recs {
		s := r.Snapshot
		_, err := pool.Exec(ctx, `
			INSERT INTO quota_recommendation_sets (
				org_id, cluster_uuid, namespace, quota_name,
				cpu_request_hard_millicores, cpu_limit_hard_millicores,
				memory_request_hard_bytes, memory_limit_hard_bytes,
				cpu_request_used_millicores, cpu_limit_used_millicores,
				memory_request_used_bytes, memory_limit_used_bytes,
				storage_request_hard_bytes, storage_request_used_bytes,
				storage_request_recommended_bytes,
				pods_hard, pods_used, pods_recommended,
				cpu_request_recommended_millicores, cpu_limit_recommended_millicores,
				memory_request_recommended_bytes, memory_limit_recommended_bytes,
				headroom_basis_points,
				cpu_request_utilization_bp, cpu_limit_utilization_bp,
				memory_request_utilization_bp, memory_limit_utilization_bp,
				utilization_storage_request_bp, utilization_pods_bp,
				cpu_freed_millicores, memory_freed_bytes,
				storage_freed_bytes, pods_freed,
				estimated_savings_cents, currency,
				recommendation_type, risk_level, notification_codes,
				quota_id, last_observed_at, updated_at,`+core.QuotaExplSQLColumns+`
			) VALUES (
				$1, $2::uuid, $3, $4,
				$5, $6, $7, $8,
				$9, $10, $11, $12,
				$13, $14, $15,
				$16, $17, $18,
				$19, $20, $21, $22,
				$23,
				$24, $25, $26, $27,
				$28, $29,
				$30, $31, $32, $33,
				$34, $35, $36,
				$37, $38, $39, $40, NOW(), $41, $42, $43, $44, $45, $46, $47
			)
			ON CONFLICT (org_id, cluster_uuid, namespace, quota_name)
			DO UPDATE SET
				cpu_request_hard_millicores = EXCLUDED.cpu_request_hard_millicores,
				cpu_limit_hard_millicores = EXCLUDED.cpu_limit_hard_millicores,
				memory_request_hard_bytes = EXCLUDED.memory_request_hard_bytes,
				memory_limit_hard_bytes = EXCLUDED.memory_limit_hard_bytes,
				cpu_request_used_millicores = EXCLUDED.cpu_request_used_millicores,
				cpu_limit_used_millicores = EXCLUDED.cpu_limit_used_millicores,
				memory_request_used_bytes = EXCLUDED.memory_request_used_bytes,
				memory_limit_used_bytes = EXCLUDED.memory_limit_used_bytes,
				storage_request_hard_bytes = EXCLUDED.storage_request_hard_bytes,
				storage_request_used_bytes = EXCLUDED.storage_request_used_bytes,
				storage_request_recommended_bytes = EXCLUDED.storage_request_recommended_bytes,
				pods_hard = EXCLUDED.pods_hard,
				pods_used = EXCLUDED.pods_used,
				pods_recommended = EXCLUDED.pods_recommended,
				cpu_request_recommended_millicores = EXCLUDED.cpu_request_recommended_millicores,
				cpu_limit_recommended_millicores = EXCLUDED.cpu_limit_recommended_millicores,
				memory_request_recommended_bytes = EXCLUDED.memory_request_recommended_bytes,
				memory_limit_recommended_bytes = EXCLUDED.memory_limit_recommended_bytes,
				headroom_basis_points = EXCLUDED.headroom_basis_points,
				cpu_request_utilization_bp = EXCLUDED.cpu_request_utilization_bp,
				cpu_limit_utilization_bp = EXCLUDED.cpu_limit_utilization_bp,
				memory_request_utilization_bp = EXCLUDED.memory_request_utilization_bp,
				memory_limit_utilization_bp = EXCLUDED.memory_limit_utilization_bp,
				utilization_storage_request_bp = EXCLUDED.utilization_storage_request_bp,
				utilization_pods_bp = EXCLUDED.utilization_pods_bp,
				cpu_freed_millicores = EXCLUDED.cpu_freed_millicores,
				memory_freed_bytes = EXCLUDED.memory_freed_bytes,
				storage_freed_bytes = EXCLUDED.storage_freed_bytes,
				pods_freed = EXCLUDED.pods_freed,
				estimated_savings_cents = EXCLUDED.estimated_savings_cents,
				currency = EXCLUDED.currency,
				recommendation_type = EXCLUDED.recommendation_type,
				risk_level = EXCLUDED.risk_level,
				notification_codes = EXCLUDED.notification_codes,
				quota_id = EXCLUDED.quota_id,
				last_observed_at = EXCLUDED.last_observed_at,
				updated_at = NOW(),`+core.QuotaExplUpdateSet,
			append([]any{
				r.OrgID, r.ClusterUUID, r.Namespace, r.QuotaName,
				nullableInt64(s.CPURequestHardMC), nullableInt64(s.CPULimitHardMC),
				nullableInt64(s.MemoryRequestHardBytes), nullableInt64(s.MemoryLimitHardBytes),
				nullableInt64(s.CPURequestUsedMC), nullableInt64(s.CPULimitUsedMC),
				nullableInt64(s.MemoryRequestUsedBytes), nullableInt64(s.MemoryLimitUsedBytes),
				nullableInt64(s.StorageRequestHardBytes), nullableInt64(s.StorageRequestUsedBytes),
				nullableInt64(r.Recommended.StorageRequestBytes),
				nullableInt64(s.PodsHard), nullableInt64(s.PodsUsed), nullableInt64(r.Recommended.Pods),
				nullableInt64(r.Recommended.CPURequestMillicores), nullableInt64(r.Recommended.CPULimitMillicores),
				nullableInt64(r.Recommended.MemoryRequestBytes), nullableInt64(r.Recommended.MemoryLimitBytes),
				r.HeadroomBP,
				r.Utilization.CPURequestBP, r.Utilization.CPULimitBP,
				r.Utilization.MemoryRequestBP, r.Utilization.MemoryLimitBP,
				utilizationBP(s.StorageRequestUsedBytes, s.StorageRequestHardBytes),
				utilizationBP(s.PodsUsed, s.PodsHard),
				nullableInt64(r.CapacityFreed.CPUMillicores), nullableInt64(r.CapacityFreed.MemoryBytes),
				nullableInt64(r.CapacityFreed.StorageBytes), nullableInt64(r.CapacityFreed.PodsFreed),
				nullableInt64(r.EstimatedSavingsCents), r.Currency,
				r.RecommendationType, r.RiskLevel, r.NotificationCodes,
				model.NativeQuotaID(r.ClusterUUID, r.Namespace, r.QuotaName),
				s.LastObservedAt,
			}, core.AppendQuotaExplArgs(nil, r.Expl)...)...,
		)
		if err != nil {
			return fmt.Errorf("upsert quota recommendation %s/%s: %w", r.Namespace, r.QuotaName, err)
		}
	}
	return nil
}

func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
