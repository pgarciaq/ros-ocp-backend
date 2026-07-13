package vm

import (
	"fmt"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// VM network QoS notification codes (simplified SR-IOV / DPDK hints).
const (
	NotifVMNetworkQoSSRIOV int16 = 65
	NotifVMNetworkQoSDPDK  int16 = 66
)

// EvaluateNetworkQoS emits actionable network QoS hints for network-bound VMs.
// Full NIC-type recommendations require operator interface metadata (future work).
func EvaluateNetworkQoS(digest model.DailyVMDigest, isNetworkBound bool, cfg VMRecConfig) []VMNotification {
	if !cfg.NetworkQoSEnabled || !isNetworkBound {
		return nil
	}

	var notifications []VMNotification

	totalBPS := float64(digest.NetThroughputP95BPS)
	totalPPS := float64(digest.NetPPSP95)

	var avgPacketBytes float64
	if totalPPS > 0 {
		avgPacketBytes = totalBPS / totalPPS
	}

	dropThreshold := cfg.NetworkQoSSRIOVDropThreshold
	if dropThreshold <= 0 {
		dropThreshold = 0.01
	}
	throughputThreshold := float64(cfg.NetworkQoSSRIOVThroughputBPS)
	if throughputThreshold <= 0 {
		throughputThreshold = 5_000_000_000
	}
	dropRatio := float64(digest.NetDropRatioMaxBP) / 10000.0

	if dropRatio >= dropThreshold || totalBPS >= throughputThreshold {
		msg := fmt.Sprintf(
			"VM has sustained high throughput (%.1f Gbps) with %.1f%% packet drops — SR-IOV may reduce drops and improve performance",
			totalBPS/1e9, dropRatio*100,
		)
		notifications = append(notifications, VMNotification{
			Code:    NotifVMNetworkQoSSRIOV,
			Type:    vmNotifTypeInfo,
			Message: msg,
		})
	}

	ppsThreshold := float64(cfg.NetworkQoSDPDKPPSThreshold)
	if ppsThreshold <= 0 {
		ppsThreshold = 500_000
	}
	if totalPPS >= ppsThreshold && avgPacketBytes > 0 && avgPacketBytes < 256 {
		msg := fmt.Sprintf(
			"VM processes %dk packets/sec with average size %.0f bytes — DPDK userspace networking may reduce latency",
			int(totalPPS/1000), avgPacketBytes,
		)
		notifications = append(notifications, VMNotification{
			Code:    NotifVMNetworkQoSDPDK,
			Type:    vmNotifTypeInfo,
			Message: msg,
		})
	}

	return notifications
}
