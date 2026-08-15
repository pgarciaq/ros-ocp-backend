package snapshot

import (
	"math"
	"strings"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

type pvcGroup struct {
	snapshots []int
}

var managedToolPrefixes = map[string]string{
	"velero.io/":             "Velero",
	"k10.kasten.io/":         "Kasten K10",
	"backup.openshift.io/":   "OpenShift Backup",
	"triliovault.trilio.io/": "Trilio",
	"stash.appscode.com/":    "Stash/KubeStash",
}

// ClassifySnapshotInventory classifies already-loaded inventory rows with no pool.
func ClassifySnapshotInventory(inventory []InventoryRow, orgID, clusterUUID string, settings SnapshotSettings, now time.Time) []SnapshotRec {
	if len(inventory) == 0 {
		return nil
	}

	pvcGroups := make(map[string]*pvcGroup)
	for i, snap := range inventory {
		if snap.SourcePVCName == "" {
			continue
		}
		key := snap.Namespace + "/" + snap.SourcePVCName
		g, ok := pvcGroups[key]
		if !ok {
			g = &pvcGroup{}
			pvcGroups[key] = g
		}
		g.snapshots = append(g.snapshots, i)
	}

	recs := make([]SnapshotRec, 0, len(inventory))

	for i, snap := range inventory {
		ageDays := int(math.Floor(now.Sub(snap.CreationTimestamp).Hours() / 24))
		if ageDays < 0 {
			ageDays = 0
		}

		rec := SnapshotRec{
			OrgID:               orgID,
			ClusterUUID:         clusterUUID,
			Namespace:           snap.Namespace,
			SnapshotName:        snap.SnapshotName,
			SourcePVCName:       snap.SourcePVCName,
			VolumeSnapshotClass: snap.VolumeSnapshotClass,
			StorageClass:        snap.StorageClass,
			CreationTimestamp:   snap.CreationTimestamp,
			RestoreSizeBytes:    snap.RestoreSizeBytes,
			AgeDays:             ageDays,
			SourcePVCExists:     snap.SourcePVCExists,
			RestoredPVCCount:    snap.RestoredPVCCount,
		}

		gib := float64(snap.RestoreSizeBytes) / (1024 * 1024 * 1024)
		cents := usdToCents(gib * settings.CostPerGiBMonth)
		rec.EstimatedCostCents = &cents

		managedBy := detectManagedTool(snap.Labels)
		rec.ManagedBy = managedBy

		classification, codes, expl := classifySnapshotWithExplanation(snap, i, ageDays, managedBy, settings, pvcGroups, inventory)
		rec.RecommendationType = classification
		rec.NotificationCodes = codes
		rec.Expl = expl

		recs = append(recs, rec)
	}

	return recs
}

func classifySnapshotWithExplanation(
	snap InventoryRow,
	idx, ageDays int,
	managedBy string,
	settings SnapshotSettings,
	pvcGroups map[string]*pvcGroup,
	inventory []InventoryRow,
) (string, []int16, types.SnapshotExplanationFactors) {
	classification, codes := classifySnapshot(snap, idx, ageDays, managedBy, settings, pvcGroups, inventory)
	var expl types.SnapshotExplanationFactors
	switch classification {
	case "orphaned":
		expl = types.SnapshotExplanationFactors{
			ThresholdUsed:      settings.OrphanAgeDays,
			ThresholdName:      "orphan_age_days",
			ClassificationRule: "source PVC deleted AND age > orphan threshold",
		}
	case "managed":
		expl = types.SnapshotExplanationFactors{
			ThresholdName:      "managed_by",
			ClassificationRule: "backup tool detected: " + managedBy,
		}
	case "redundant":
		expl = types.SnapshotExplanationFactors{
			ThresholdUsed:      settings.RedundantThreshold,
			ThresholdName:      "redundancy_max",
			ClassificationRule: "age > stale threshold AND not among newest snapshots for source PVC",
		}
	case "stale":
		expl = types.SnapshotExplanationFactors{
			ThresholdUsed:      settings.StaleDays,
			ThresholdName:      "stale_age_days",
			ClassificationRule: "age > stale threshold AND never restored",
		}
	case "never_restored":
		expl = types.SnapshotExplanationFactors{
			ThresholdUsed:      settings.NeverRestoredDays,
			ThresholdName:      "never_restored_days",
			ClassificationRule: "age > never-restored threshold AND no restores",
		}
	default:
		expl = types.SnapshotExplanationFactors{
			ClassificationRule: "recent snapshot or has restores",
		}
	}
	return classification, codes, expl
}

func classifySnapshot(
	snap InventoryRow,
	idx, ageDays int,
	managedBy string,
	settings SnapshotSettings,
	pvcGroups map[string]*pvcGroup,
	inventory []InventoryRow,
) (string, []int16) {
	if snap.SourcePVCName != "" && !snap.SourcePVCExists && ageDays > settings.OrphanAgeDays {
		return "orphaned", []int16{types.NotifSnapshotOrphaned}
	}

	if managedBy != "" {
		return "managed", []int16{types.NotifSnapshotManaged}
	}

	if snap.SourcePVCName != "" {
		key := snap.Namespace + "/" + snap.SourcePVCName
		if g, ok := pvcGroups[key]; ok && len(g.snapshots) > settings.RedundantThreshold {
			if ageDays > settings.StaleDays && !isAmongNewest(idx, g.snapshots, settings.RedundantThreshold, inventory) {
				return "redundant", []int16{types.NotifSnapshotRedundant}
			}
		}
	}

	if ageDays > settings.StaleDays && snap.RestoredPVCCount == 0 {
		return "stale", []int16{types.NotifSnapshotStale}
	}

	if ageDays > settings.NeverRestoredDays && snap.RestoredPVCCount == 0 {
		return "never_restored", []int16{types.NotifSnapshotNeverUsed}
	}

	return "active", []int16{}
}

func isAmongNewest(idx int, groupIdxs []int, n int, inventory []InventoryRow) bool {
	if n >= len(groupIdxs) {
		return true
	}

	target := inventory[idx].CreationTimestamp
	newerCount := 0
	for _, gi := range groupIdxs {
		if gi == idx {
			continue
		}
		if inventory[gi].CreationTimestamp.After(target) {
			newerCount++
		}
	}
	return newerCount < n
}

func detectManagedTool(labels map[string]string) string {
	for key := range labels {
		for prefix, tool := range managedToolPrefixes {
			if strings.HasPrefix(key, prefix) {
				return tool
			}
		}
	}
	return ""
}
