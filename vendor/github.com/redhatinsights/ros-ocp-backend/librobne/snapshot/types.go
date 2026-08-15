package snapshot

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// SnapshotSettings holds resolved snapshot classification thresholds.
// CostPerGiBMonth stays float64 — do not change the deposited cost shape.
type SnapshotSettings struct {
	OrphanAgeDays       int
	NeverRestoredDays   int
	StaleDays           int
	RedundantThreshold  int
	CostPerGiBMonth     float64
	InventoryFreshHours int
}

// SnapshotRec is a classified snapshot recommendation.
type SnapshotRec struct {
	OrgID               string
	ClusterUUID         string
	Namespace           string
	SnapshotName        string
	SourcePVCName       string
	VolumeSnapshotClass string
	StorageClass        string
	CreationTimestamp   time.Time
	RestoreSizeBytes    int64
	AgeDays             int
	SourcePVCExists     bool
	RestoredPVCCount    int
	ManagedBy           string
	RecommendationType  string
	EstimatedCostCents  *int64
	NotificationCodes   []int16
	Expl                types.SnapshotExplanationFactors
}

// InventoryRow is the inventory shape consumed by ClassifySnapshotInventory.
type InventoryRow struct {
	Namespace           string
	SnapshotName        string
	SourcePVCName       string
	VolumeSnapshotClass string
	StorageClass        string
	CreationTimestamp   time.Time
	RestoreSizeBytes    int64
	SourcePVCExists     bool
	RestoredPVCCount    int
	Labels              map[string]string
}

// DefaultSnapshotSettings matches the product compiled-in snapshot defaults.
func DefaultSnapshotSettings() SnapshotSettings {
	return SnapshotSettings{
		OrphanAgeDays:       7,
		NeverRestoredDays:   30,
		StaleDays:           90,
		RedundantThreshold:  3,
		CostPerGiBMonth:     0.05,
		InventoryFreshHours: 6,
	}
}
