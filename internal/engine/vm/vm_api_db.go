package vm

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	rootengine "github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// VMRecommendationFilters holds list query filters for VM recommendations.
type VMRecommendationFilters struct {
	ClusterUUIDs       []string
	Namespace          string
	VMName             string
	Node               string
	Term               string
	Engine             string
	Confidence         string
	GuestAgentDetected *bool
	Categories         []string // filter[category]=idle,oversized
	IsNetworkBound     *bool
	HasGPU             *bool
	GPUClassification  string // comma-separated list
	GuestOS            string // comma-separated substrings (case-insensitive)
	OrderBy            string
	OrderDesc          bool
	Limit              int
	Offset             int
	UseKeyset          bool
	KeysetCursor       VMListCursor
	TagFilters         []model.TagFilter
}

var vmRecOrderColumns = map[string]string{
	"vm_name":                   "vm_name",
	"namespace":                 "namespace",
	"current_vcpu":              "current_vcpu",
	"current_memory_gib":        "current_memory_gib",
	"guest_os":                  "guest_os",
	"recommended_vcpu":          "recommended_vcpu",
	"recommended_memory_gib":    "recommended_memory_gib",
	"category":                  "category",
	"confidence":                "confidence",
	"last_recommended_at":       "last_recommended_at",
	"estimated_monthly_savings": "estimated_savings_cents",
	"savings":                   "estimated_savings_cents",
	"savings_amount":            "estimated_savings_cents",
}

// ListVMRecommendations returns a page of VM recommendations and total count.
func ListVMRecommendations(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID string,
	filters VMRecommendationFilters,
) ([]model.VMRecommendation, int64, error) {
	if pool == nil {
		return nil, 0, fmt.Errorf("database pool unavailable")
	}

	where, args := buildVMRecWhere(orgID, filters)
	orderCol := vmRecOrderColumns["vm_name"]
	if col, ok := vmRecOrderColumns[filters.OrderBy]; ok {
		orderCol = col
	}
	orderHow := "ASC"
	if filters.OrderDesc {
		orderHow = "DESC"
	}

	countQuery := `SELECT COUNT(*) FROM vm_recommendations` + where
	var total int64
	if err := pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count VM recommendations: %w", err)
	}

	limit := filters.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := filters.Offset
	if offset < 0 {
		offset = 0
	}
	if filters.UseKeyset {
		offset = 0
	}

	listArgs := append([]any{}, args...)
	argIdx := len(args) + 1

	query := `
		SELECT
			id, org_id, cluster_uuid, vm_name, namespace, guest_os,
			current_vcpu, current_memory_gib, current_disk_gib, current_instance_type,
			recommended_vcpu, recommended_memory_gib, recommended_disk_gib,
			recommended_instance_type, recommended_series,
			guest_agent_detected, confidence, term, engine,
			category, power_off_idle_ratio, is_network_bound,
			is_redundant_placement, has_shared_storage, numa_oversized,
			io_read_iops_p95, io_write_iops_p95, io_read_bps_p95, io_write_bps_p95, io_hint, io_pattern,
			disk_days_until_full, disk_growth_gib_per_day, disk_recommended_expand_gib,
			notifications,
			gpu_count, gpu_model, gpu_classification, recommended_gpu_action,
			recommended_gpu_profile, recommended_time_slice_count,
			gpu_timeslice_confidence, gpu_timeslice_rationale, recommended_vgpu_profile,
			gpu_utilization_avg_bp,
			estimated_savings_cents, savings_currency,
			last_recommended_at, created_at, updated_at,
			expl_data_days, expl_max_cpu_usage_mc, expl_max_mem_usage_kib,
			expl_cpu_margin_bp, expl_mem_margin_bp,
			expl_raw_recommended_vcpu, expl_raw_recommended_mem_gib,
			expl_downsize_hysteresis_held, expl_guest_agent_used,
			expl_idle_detected, expl_abandoned_detected, expl_power_off_candidate,
			expl_sizing_branch, expl_gpu_action, expl_gpu_rationale
		FROM vm_recommendations` + where

	if filters.UseKeyset {
		seekSQL, seekArgs, nextIdx, seekErr := vmKeysetSeekClause(orderCol, orderHow, filters.KeysetCursor, argIdx)
		if seekErr != nil {
			return nil, 0, seekErr
		}
		query += ` AND ` + seekSQL
		listArgs = append(listArgs, seekArgs...)
		argIdx = nextIdx
	}

	query += ` ORDER BY ` + vmOrderNulls(orderCol, orderHow) +
		`, cluster_uuid ASC, vm_name ASC, namespace ASC, term ASC, engine ASC`

	pageLimit := limit
	if pageLimit > 0 {
		pageLimit++
	}
	query += fmt.Sprintf(` LIMIT $%d`, argIdx)
	listArgs = append(listArgs, pageLimit)
	argIdx++

	if !filters.UseKeyset {
		query += fmt.Sprintf(` OFFSET $%d`, argIdx)
		listArgs = append(listArgs, offset)
	}

	rows, err := pool.Query(ctx, query, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list VM recommendations: %w", err)
	}
	defer rows.Close()

	var recs []model.VMRecommendation
	for rows.Next() {
		rec, scanErr := scanVMRecommendation(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		recs = append(recs, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate VM recommendations: %w", err)
	}
	return recs, total, nil
}

// GetVMRecommendationDetail returns one VM recommendation and its daily digests for the term window.
func GetVMRecommendationDetail(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, vmName, namespace, term, engine string,
) (*model.VMRecommendation, []model.DailyVMDigest, error) {
	if pool == nil {
		return nil, nil, fmt.Errorf("database pool unavailable")
	}
	clusterID, err := uuid.Parse(clusterUUID)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid cluster_uuid: %w", err)
	}

	recQuery := `
		SELECT
			id, org_id, cluster_uuid, vm_name, namespace, guest_os,
			current_vcpu, current_memory_gib, current_disk_gib, current_instance_type,
			recommended_vcpu, recommended_memory_gib, recommended_disk_gib,
			recommended_instance_type, recommended_series,
			guest_agent_detected, confidence, term, engine,
			category, power_off_idle_ratio, is_network_bound,
			is_redundant_placement, has_shared_storage, numa_oversized,
			io_read_iops_p95, io_write_iops_p95, io_read_bps_p95, io_write_bps_p95, io_hint, io_pattern,
			disk_days_until_full, disk_growth_gib_per_day, disk_recommended_expand_gib,
			notifications,
			gpu_count, gpu_model, gpu_classification, recommended_gpu_action,
			recommended_gpu_profile, recommended_time_slice_count,
			gpu_timeslice_confidence, gpu_timeslice_rationale, recommended_vgpu_profile,
			gpu_utilization_avg_bp,
			estimated_savings_cents, savings_currency,
			last_recommended_at, created_at, updated_at,
			expl_data_days, expl_max_cpu_usage_mc, expl_max_mem_usage_kib,
			expl_cpu_margin_bp, expl_mem_margin_bp,
			expl_raw_recommended_vcpu, expl_raw_recommended_mem_gib,
			expl_downsize_hysteresis_held, expl_guest_agent_used,
			expl_idle_detected, expl_abandoned_detected, expl_power_off_candidate,
			expl_sizing_branch, expl_gpu_action, expl_gpu_rationale
		FROM vm_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2 AND vm_name = $3 AND namespace = $4
		  AND term = $5 AND engine = $6`

	row := pool.QueryRow(ctx, recQuery, orgID, clusterID, vmName, namespace, term, engine)
	rec, err := scanVMRecommendationRow(row)
	if err == pgx.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get VM recommendation: %w", err)
	}

	lookback := 30
	termConfigs, termErr := rootengine.LoadTermConfigCached(ctx, pool, orgID, "vm")
	if termErr == nil {
		for _, tw := range VMTermWindowsFromConfig(termConfigs) {
			if tw.Name == term {
				lookback = tw.LookbackDays
				break
			}
		}
	}
	since := time.Now().UTC().AddDate(0, 0, -lookback).Truncate(24 * time.Hour)
	digests, err := QueryDailyVMDigestsForVM(ctx, pool, orgID, clusterID, vmName, namespace, since)
	if err != nil {
		return nil, nil, err
	}
	return &rec, digests, nil
}

// QueryDailyVMDigestsForVM returns daily digests for a single VM since the given date.
func QueryDailyVMDigestsForVM(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID string,
	clusterUUID uuid.UUID,
	vmName, namespace string,
	since time.Time,
) ([]model.DailyVMDigest, error) {
	rows, err := pool.Query(ctx, `
		SELECT
			id, org_id, cluster_uuid, vm_name, namespace, node_name, guest_os, bucket_date,
			cpu_usage_p50_mc, cpu_usage_p95_mc, cpu_usage_p99_mc, cpu_usage_max_mc,
			cpu_request_mc, cpu_limit_mc,
			mem_usage_p50_kib, mem_usage_p95_kib, mem_usage_p99_kib, mem_usage_max_kib,
			mem_request_kib,
			mem_available_p50_kib, mem_available_p95_kib,
			disk_allocated_max_bytes,
			filesystem_used_max_bytes, filesystem_capacity_bytes,
			disk_read_iops_p95, disk_write_iops_p95, disk_read_bps_p95, disk_write_bps_p95,
			sample_count, agent_sample_count, restart_count_sum,
			gpu_count, gpu_model, gpu_util_avg_bp, gpu_util_max_bp,
			gpu_fb_used_avg_mib, gpu_fb_used_max_mib, gpu_sm_active_avg_bp,
			gpu_tensor_avg_bp, gpu_dram_avg_bp, gpu_mig_profile, gpu_max_slices, has_gpu
		FROM daily_vm_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND vm_name = $3 AND namespace = $4
		  AND bucket_date >= $5::date
		ORDER BY bucket_date`,
		orgID, clusterUUID, vmName, namespace, since.Format("2006-01-02"),
	)
	if err != nil {
		return nil, fmt.Errorf("query VM digests for VM: %w", err)
	}
	defer rows.Close()

	var result []model.DailyVMDigest
	for rows.Next() {
		var d model.DailyVMDigest
		err := rows.Scan(
			&d.ID, &d.OrgID, &d.ClusterUUID, &d.VMName, &d.Namespace, &d.NodeName, &d.GuestOS, &d.BucketDate,
			&d.CPUUsageP50MC, &d.CPUUsageP95MC, &d.CPUUsageP99MC, &d.CPUUsageMaxMC,
			&d.CPURequestMC, &d.CPULimitMC,
			&d.MemUsageP50KiB, &d.MemUsageP95KiB, &d.MemUsageP99KiB, &d.MemUsageMaxKiB,
			&d.MemRequestKiB,
			&d.MemAvailableP50KiB, &d.MemAvailableP95KiB,
			&d.DiskAllocatedMaxBytes,
			&d.FilesystemUsedMaxBytes, &d.FilesystemCapacityBytes,
			&d.DiskReadIOPSP95, &d.DiskWriteIOPSP95, &d.DiskReadBPS95, &d.DiskWriteBPS95,
			&d.SampleCount, &d.AgentSampleCount, &d.RestartCountSum,
			&d.GPUCount, &d.GPUModel, &d.GPUUtilAvgBP, &d.GPUUtilMaxBP,
			&d.GPUFBUsedAvgMiB, &d.GPUFBUsedMaxMiB, &d.GPUSMActiveAvgBP,
			&d.GPUTensorAvgBP, &d.GPUDRAMAvgBP, &d.GPUMIGProfile, &d.GPUMaxSlices, &d.HasGPU,
		)
		if err != nil {
			return nil, fmt.Errorf("scan VM digest: %w", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate VM digests: %w", err)
	}
	if err := AttachGPUDevicesToDigests(ctx, pool, result); err != nil {
		return nil, err
	}
	if err := AttachPVCsToDigests(ctx, pool, result); err != nil {
		return nil, err
	}
	return result, nil
}

func buildVMRecWhere(orgID string, filters VMRecommendationFilters) (string, []any) {
	clauses := []string{"WHERE org_id = $1"}
	args := []any{orgID}
	argIdx := 2

	if len(filters.ClusterUUIDs) > 0 {
		clauses = append(clauses, "AND cluster_uuid::text = ANY($"+strconv.Itoa(argIdx)+")")
		args = append(args, filters.ClusterUUIDs)
		argIdx++
	}
	if filters.Namespace != "" {
		clauses = append(clauses, "AND namespace = $"+strconv.Itoa(argIdx))
		args = append(args, filters.Namespace)
		argIdx++
	}
	if filters.VMName != "" {
		clauses = append(clauses, "AND vm_name = $"+strconv.Itoa(argIdx))
		args = append(args, filters.VMName)
		argIdx++
	}
	if filters.Node != "" {
		clauses = append(clauses, "AND node_name = $"+strconv.Itoa(argIdx))
		args = append(args, filters.Node)
		argIdx++
	}
	if filters.Term != "" {
		clauses = append(clauses, "AND term = $"+strconv.Itoa(argIdx))
		args = append(args, filters.Term)
		argIdx++
	}
	if filters.Engine != "" {
		clauses = append(clauses, "AND engine = $"+strconv.Itoa(argIdx))
		args = append(args, filters.Engine)
		argIdx++
	}
	if filters.Confidence != "" {
		clauses = append(clauses, "AND confidence = $"+strconv.Itoa(argIdx))
		args = append(args, filters.Confidence)
		argIdx++
	}
	if filters.GuestAgentDetected != nil {
		clauses = append(clauses, "AND guest_agent_detected = $"+strconv.Itoa(argIdx))
		args = append(args, *filters.GuestAgentDetected)
		argIdx++
	}
	if len(filters.Categories) > 0 {
		clauses = append(clauses, "AND category = ANY($"+strconv.Itoa(argIdx)+")")
		args = append(args, filters.Categories)
		argIdx++
	}
	if filters.IsNetworkBound != nil {
		clauses = append(clauses, "AND is_network_bound = $"+strconv.Itoa(argIdx))
		args = append(args, *filters.IsNetworkBound)
		argIdx++
	}
	if filters.HasGPU != nil {
		if *filters.HasGPU {
			clauses = append(clauses, "AND gpu_count > 0")
		} else {
			clauses = append(clauses, "AND gpu_count = 0")
		}
	}
	if filters.GPUClassification != "" {
		parts := strings.Split(filters.GPUClassification, ",")
		var classes []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				classes = append(classes, p)
			}
		}
		if len(classes) > 0 {
			clauses = append(clauses, "AND gpu_classification = ANY($"+strconv.Itoa(argIdx)+")")
			args = append(args, classes)
			argIdx++
		}
	}
	if filters.GuestOS != "" {
		parts := strings.Split(filters.GuestOS, ",")
		var guestClauses []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			guestClauses = append(guestClauses, "guest_os ILIKE $"+strconv.Itoa(argIdx))
			args = append(args, "%"+p+"%")
			argIdx++
		}
		if len(guestClauses) > 0 {
			clauses = append(clauses, "AND ("+strings.Join(guestClauses, " OR ")+")")
		}
	}
	if len(filters.TagFilters) > 0 {
		tagClause, tagArgs, _ := model.TagFilterExistsClause(
			orgID, "vm_recommendations.cluster_uuid", "vm_recommendations.namespace", filters.TagFilters, argIdx)
		if tagClause != "" {
			clauses = append(clauses, "AND "+tagClause)
			args = append(args, tagArgs...)
		}
	}
	return " " + strings.Join(clauses, " "), args
}

type vmRecScanner interface {
	Scan(dest ...any) error
}

func scanVMRecommendation(rows vmRecScanner) (model.VMRecommendation, error) {
	return scanVMRecommendationRow(rows)
}

func scanVMRecommendationRow(row pgx.Row) (model.VMRecommendation, error) {
	var r model.VMRecommendation
	err := row.Scan(
		&r.ID, &r.OrgID, &r.ClusterUUID, &r.VMName, &r.Namespace, &r.GuestOS,
		&r.CurrentVCPU, &r.CurrentMemoryGiB, &r.CurrentDiskGiB, &r.CurrentInstanceType,
		&r.RecommendedVCPU, &r.RecommendedMemoryGiB, &r.RecommendedDiskGiB,
		&r.RecommendedInstanceType, &r.RecommendedSeries,
		&r.GuestAgentDetected, &r.Confidence, &r.Term, &r.Engine,
		&r.Category, &r.PowerOffIdleRatio, &r.IsNetworkBound,
		&r.IsRedundantPlacement, &r.HasSharedStorage, &r.NUMAOversized,
		&r.IOReadIOPSP95, &r.IOWriteIOPSP95, &r.IOReadBPS95, &r.IOWriteBPS95, &r.IOHint, &r.IOPattern,
		&r.DiskDaysUntilFull, &r.DiskGrowthGiBPerDay, &r.DiskRecommendedExpandGiB,
		&r.Notifications,
		&r.GPUCount, &r.GPUModel, &r.GPUClassification, &r.RecommendedGPUAction,
		&r.RecommendedGPUProfile, &r.RecommendedTimeSliceCount,
		&r.GPUTimeSliceConfidence, &r.GPUTimeSliceRationale, &r.RecommendedVGPUProfile,
		&r.GPUUtilizationAvgBP,
		&r.EstimatedSavingsCents, &r.SavingsCurrency,
		&r.LastRecommendedAt, &r.CreatedAt, &r.UpdatedAt,
		&r.ExplDataDays, &r.ExplMaxCPUUsageMC, &r.ExplMaxMemUsageKiB,
		&r.ExplCPUMarginBP, &r.ExplMemMarginBP,
		&r.ExplRawRecommendedVCPU, &r.ExplRawRecommendedMemGiB,
		&r.ExplDownsizeHysteresisHeld, &r.ExplGuestAgentUsed,
		&r.ExplIdleDetected, &r.ExplAbandonedDetected, &r.ExplPowerOffCandidate,
		&r.ExplSizingBranch, &r.ExplGPUAction, &r.ExplGPURationale,
	)
	if err != nil {
		return model.VMRecommendation{}, fmt.Errorf("scan VM recommendation: %w", err)
	}
	return r, nil
}
