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
	NotifVMDiskCritical int16 = 42
	NotifVMAbandoned              int16 = 43
	NotifVMGuestAgentInterrupted  int16 = 44
	NotifVMInsufficientData       int16 = 45
	NotifVMUnknownOS              int16 = 46
	NotifVMWindowsUpdateSpike     int16 = 47
	NotifVMCrashLoop              int16 = 48
	NotifVMDownsizeHeld           int16 = 49
	NotifVMGPUIdle                int16 = 50
	NotifVMGPUUnderutilized       int16 = 51
	NotifVMGPUMemorySaturated     int16 = 52
	NotifVMGPUComputeSaturated    int16 = 53
	NotifVMGPUMixedIdle           int16 = 54
	NotifVMVGPUProfileRecommended int16 = 56
	NotifVMGPUTimeSliceUnsafeFB   int16 = 57
	NotifVMNetworkSaturated       int16 = 55
	NotifVMIOSequential           int16 = 58
	NotifVMIORandom               int16 = 59
)

type vmNotificationParams struct {
	IsIdle                   bool
	IsAbandoned              bool
	AbandonedDays            int
	IsOversized              bool
	GuestAgentDetected       bool
	AgentInterrupted         bool
	LowConfidence            bool
	IOHint                   *string
	IOPattern                string
	DiskDaysUntilFull        *int32
	DiskGrowthGiBPerDay      *float64
	HypervisorDiskGrowth     bool
	RecommendedInstanceType  *string
	RecommendedSeries        *string
	FilesystemUsedPct        *float64
	UnknownOS                bool
	WindowsUpdateSpike       bool
	CrashLoopRestarts        int
	DownsizeHeld             bool
	DownsizeStabilityDays    int
	IsNetworkBound           bool
}

func vmBuildNotifications(p vmNotificationParams) []byte {
	var out []VMNotification

	if p.IsAbandoned {
		days := p.AbandonedDays
		if days < 1 {
			days = 1
		}
		out = append(out, VMNotification{
			Code: NotifVMAbandoned,
			Type: vmNotifTypeCritical,
			Message: fmt.Sprintf(
				"VM appears abandoned: zero CPU and memory usage for %d days. Consider deleting or powering off to recover resources.",
				days,
			),
		})
	} else if p.IsIdle {
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
	if p.LowConfidence {
		out = append(out, VMNotification{
			Code:    NotifVMInsufficientData,
			Type:    vmNotifTypeInfo,
			Message: "Insufficient data: less than one full day of metrics available",
		})
	}
	if p.AgentInterrupted {
		out = append(out, VMNotification{
			Code:    NotifVMGuestAgentInterrupted,
			Type:    vmNotifTypeInfo,
			Message: "Guest agent data interrupted: recommendations using hypervisor metrics (moderate confidence)",
		})
	} else if !p.GuestAgentDetected {
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
	switch p.IOPattern {
	case VMIOPatternSequential:
		out = append(out, VMNotification{
			Code:    NotifVMIOSequential,
			Type:    vmNotifTypeInfo,
			Message: "Sequential I/O pattern detected — consider storage optimized for throughput",
		})
	case VMIOPatternRandom:
		out = append(out, VMNotification{
			Code:    NotifVMIORandom,
			Type:    vmNotifTypeInfo,
			Message: "Random I/O pattern detected — consider storage optimized for IOPS",
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
	if p.UnknownOS {
		out = append(out, VMNotification{
			Code:    NotifVMUnknownOS,
			Type:    vmNotifTypeInfo,
			Message: "Guest OS not detected — using Linux defaults. Install qemu-guest-agent for OS-specific thresholds.",
		})
	}
	if p.WindowsUpdateSpike {
		out = append(out, VMNotification{
			Code:    NotifVMWindowsUpdateSpike,
			Type:    vmNotifTypeInfo,
			Message: "Periodic usage spikes detected (possibly OS updates); P95 sizing accounts for this",
		})
	}
	if p.CrashLoopRestarts > 0 {
		out = append(out, VMNotification{
			Code: NotifVMCrashLoop,
			Type: vmNotifTypeWarning,
			Message: fmt.Sprintf(
				"VM restarted %d times in the observation window — possible instability or crash loop",
				p.CrashLoopRestarts,
			),
		})
	}
	if p.DownsizeHeld {
		days := p.DownsizeStabilityDays
		if days < 1 {
			days = 3
		}
		out = append(out, VMNotification{
			Code: NotifVMDownsizeHeld,
			Type: vmNotifTypeInfo,
			Message: fmt.Sprintf(
				"Downsize recommendation suppressed: usage not consistently below threshold for %d days",
				days,
			),
		})
	}
	if p.IsNetworkBound {
		out = append(out, VMNotification{
			Code:    NotifVMNetworkSaturated,
			Type:    vmNotifTypeWarning,
			Message: "Network-saturated workload: recommend n1 network-optimized instance type",
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
