package vm

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

const vmDigestScheduleBusinessHours = "business_hours"

// VMBHRecommendation is the thin nested business-hours block on GET .../vm/detail.
// Detail-read still invokes RecommendVM on the business_hours stream; only vCPU/GiB
// (plus reason / code 82) are copied. Instance-type SKU, idle/abandoned/power-off,
// guest GPU, disk, I/O, network, and the parent notification list are omitted.
type VMBHRecommendation struct {
	RecommendedVCPU      *int32                                     `json:"recommended_vcpu,omitempty"`
	RecommendedMemoryGiB *int32                                     `json:"recommended_memory_gib,omitempty"`
	Reason               string                                     `json:"reason,omitempty"`
	Notifications        map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
}

// EnrichVMDetailWithBusinessHours returns nested business_hours VM sizing for
// GET .../vm/detail only. List, history, CSV, and group-by stay all-hours.
// Nil result means omit the object (disabled schedule or feature off).
func EnrichVMDetailWithBusinessHours(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, vmName, namespace, term, engineName string,
) (*VMBHRecommendation, error) {
	if !config.BusinessHoursFeatureEnabled() || pool == nil {
		return nil, nil
	}
	if clusterUUID == "" || vmName == "" || namespace == "" {
		return nil, nil
	}

	cache, err := engine.LoadSchedules(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("load business hours schedules: %w", err)
	}
	if cache == nil || !cache.ProducesBusinessHoursDigests() {
		return nil, nil
	}
	sched := cache.Resolve(namespace)
	if !sched.Enabled {
		return nil, nil
	}

	termConfigs, err := engine.LoadTermConfigCached(ctx, pool, orgID, "vm")
	if err != nil {
		return nil, fmt.Errorf("load term config for VM business hours: %w", err)
	}
	terms := VMTermWindowsFromConfig(termConfigs)
	tw := vmTermWindowByName(terms, term)
	lookback := MaxVMLookbackDays(terms)
	if lookback < 1 {
		lookback = tw.LookbackDays
	}
	if lookback < 1 {
		lookback = 30
	}
	since := time.Now().UTC().AddDate(0, 0, -lookback).Truncate(24 * time.Hour)

	clusterID, err := uuid.Parse(clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("parse cluster UUID: %w", err)
	}

	bhDigests, err := QueryDailyVMDigestsForVMBySchedule(ctx, pool, orgID, clusterID, vmName, namespace, since, vmDigestScheduleBusinessHours)
	if err != nil {
		return nil, fmt.Errorf("query VM business_hours digests: %w", err)
	}
	bhDayCount := countVMDigestDaysInLookback(bhDigests, tw.LookbackDays)
	if bhDayCount < tw.MinDataDays {
		return &VMBHRecommendation{
			Reason: insufficientVMBusinessHoursReason(bhDayCount, tw.MinDataDays),
		}, nil
	}

	cfg, err := ResolveVMRecConfig(ctx, pool, orgID)
	if err != nil {
		cfg = DefaultVMRecConfig()
	}
	clusterTypes, err := QueryClusterInstanceTypes(ctx, pool, orgID, clusterID)
	if err != nil {
		return nil, fmt.Errorf("load cluster instance types for VM business hours: %w", err)
	}
	prefCtx, err := QueryClusterVMPreferences(ctx, pool, orgID, clusterID)
	if err != nil {
		return nil, fmt.Errorf("load VM preferences for business hours: %w", err)
	}
	clusterLatest := buildClusterLatestDigests(bhDigests)
	clusterCtx := NewClusterContext(clusterLatest)

	nodeMemGiBByNode := map[string]float64(nil)
	end := time.Now().UTC()
	if nodeDigests, nodeErr := engine.QueryNodeDigests(ctx, pool, orgID, clusterUUID, since, end); nodeErr != nil {
		return nil, fmt.Errorf("load node digests for VM business hours: %w", nodeErr)
	} else if len(nodeDigests) > 0 {
		nodeMemGiBByNode = buildNodeMemoryGiBMap(nodeDigests)
	}

	rec, recErr := RecommendVM(bhDigests, cfg, tw, engineName, clusterTypes, prefCtx, clusterCtx, nodeMemGiBByNode)
	if recErr != nil {
		return nil, fmt.Errorf("recommend VM business hours: %w", recErr)
	}
	if rec == nil {
		return &VMBHRecommendation{
			Reason: insufficientVMBusinessHoursReason(bhDayCount, tw.MinDataDays),
		}, nil
	}

	vcpu := rec.RecommendedVCPU
	mem := rec.RecommendedMemoryGiB
	return &VMBHRecommendation{
		RecommendedVCPU:      &vcpu,
		RecommendedMemoryGiB: &mem,
		Notifications:        notifications.MapToKruizeFormat([]int16{engine.NotifVMBHOfficeWindow}),
	}, nil
}

func vmTermWindowByName(terms []TermWindow, name string) TermWindow {
	for _, tw := range terms {
		if tw.Name == name {
			return tw
		}
	}
	return TermWindow{Name: name, LookbackDays: 15, MinDataDays: 7}
}

func countVMDigestDaysInLookback(digests []Digest, lookbackDays int) int {
	if lookbackDays < 1 {
		return len(digests)
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -lookbackDays).Truncate(24 * time.Hour)
	n := 0
	for _, d := range digests {
		if !d.BucketDate.Before(cutoff) {
			n++
		}
	}
	return n
}

func insufficientVMBusinessHoursReason(dataDays, minDataDays int) string {
	return fmt.Sprintf("insufficient business hours data: %d days available, %d required", dataDays, minDataDays)
}
