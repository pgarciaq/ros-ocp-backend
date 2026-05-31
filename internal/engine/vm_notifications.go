package engine

import (
	"encoding/json"
	"fmt"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// VMNotification is stored in vm_recommendations.notifications (JSONB).
// Type values are lowercase for the VM recommendations API contract.
type VMNotification struct {
	Code    int16  `json:"code"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

const (
	vmNotifTypeInfo     = "info"
	vmNotifTypeWarning  = "warning"
	vmNotifTypeCritical = "critical"
)

// VM notification codes (notification_code_definitions). Codes 20–25 are used by
// other plugins; VM-specific codes continue at 37+.
const (
	// NotifVMIdle / NotifVMOversized are 18/19 in notifications.go.
	NotifVMDiskGrowingNoCapacity int16 = 37
	NotifVMNoGuestAgent            int16 = 38
	NotifVMHighIO            int16 = 39
	NotifVMDiskFillingGuest  int16 = 40
	NotifVMInstanceTypeRec   int16 = 41
	NotifVMDiskCritical      int16 = 42
)

type vmNotificationParams struct {
	IsIdle                   bool
	IsOversized              bool
	GuestAgentDetected       bool
	IOHint                   *string
	DiskDaysUntilFull        *int32
	DiskGrowthGiBPerDay      *float64
	HypervisorDiskGrowth     bool
	RecommendedInstanceType  *string
	RecommendedSeries        *string
	FilesystemUsedPct        *float64
}

func vmBuildNotifications(p vmNotificationParams) []byte {
	var out []VMNotification

	if p.IsIdle {
		out = append(out, VMNotification{
			Code:    NotifVMIdle,
			Type:    vmNotifTypeWarning,
			Message: "VM is idle: CPU and memory usage are consistently below thresholds",
		})
	}
	if p.IsOversized {
		out = append(out, VMNotification{
			Code:    NotifVMOversized,
			Type:    vmNotifTypeWarning,
			Message: "VM is oversized: recommended resources are significantly below current allocation",
		})
	}
	if !p.GuestAgentDetected {
		out = append(out, VMNotification{
			Code:    NotifVMNoGuestAgent,
			Type:    vmNotifTypeInfo,
			Message: "QEMU guest agent not installed: recommendations based on hypervisor metrics only (moderate confidence)",
		})
	}
	if p.IOHint != nil && *p.IOHint == vmIOHintHigh {
		out = append(out, VMNotification{
			Code:    NotifVMHighIO,
			Type:    vmNotifTypeWarning,
			Message: "High disk I/O detected: consider storage-optimized instance type or faster storage class",
		})
	}
	if p.DiskDaysUntilFull != nil && *p.DiskDaysUntilFull < 90 && p.GuestAgentDetected {
		out = append(out, VMNotification{
			Code: NotifVMDiskFillingGuest,
			Type: vmNotifTypeWarning,
			Message: fmt.Sprintf(
				"Filesystem usage growing: estimated %d days until full at current growth rate",
				*p.DiskDaysUntilFull,
			),
		})
	}
	if p.HypervisorDiskGrowth && !p.GuestAgentDetected && p.DiskGrowthGiBPerDay != nil {
		out = append(out, VMNotification{
			Code: NotifVMDiskGrowingNoCapacity,
			Type: vmNotifTypeInfo,
			Message: fmt.Sprintf(
				"Disk allocation growing at %.2f GiB/day: consider proactive expansion",
				*p.DiskGrowthGiBPerDay,
			),
		})
	}
	if p.RecommendedInstanceType != nil {
		series := ""
		if p.RecommendedSeries != nil {
			series = *p.RecommendedSeries
		}
		out = append(out, VMNotification{
			Code: NotifVMInstanceTypeRec,
			Type: vmNotifTypeInfo,
			Message: fmt.Sprintf(
				"Recommended instance type: %s (%s series)",
				*p.RecommendedInstanceType,
				series,
			),
		})
	}
	if p.FilesystemUsedPct != nil && *p.FilesystemUsedPct > 90 {
		out = append(out, VMNotification{
			Code: NotifVMDiskCritical,
			Type: vmNotifTypeCritical,
			Message: fmt.Sprintf(
				"Filesystem is %.0f%% full: immediate expansion recommended",
				*p.FilesystemUsedPct,
			),
		})
	}

	b, err := json.Marshal(out)
	if err != nil {
		return []byte("[]")
	}
	return b
}

// vmLatestFilesystemUsedPct returns used/capacity * 100 from the newest digest with filesystem metrics.
func vmLatestFilesystemUsedPct(days []model.DailyVMDigest) *float64 {
	var best *model.DailyVMDigest
	for i := range days {
		d := &days[i]
		if d.FilesystemUsedMaxBytes == nil || d.FilesystemCapacityBytes == nil {
			continue
		}
		if *d.FilesystemCapacityBytes <= 0 {
			continue
		}
		if best == nil || d.BucketDate.After(best.BucketDate) {
			best = d
		}
	}
	if best == nil {
		return nil
	}
	pct := 100.0 * float64(*best.FilesystemUsedMaxBytes) / float64(*best.FilesystemCapacityBytes)
	return &pct
}
