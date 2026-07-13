package vm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func TestEvaluateNetworkQoS_SRIOV_Drops(t *testing.T) {
	cfg := DefaultVMRecConfig()
	digest := model.DailyVMDigest{
		NetThroughputP95BPS: 1_000_000_000,
		NetPPSP95:           10_000,
		NetDropRatioMaxBP:   200, // 2%
	}
	notifs := EvaluateNetworkQoS(digest, true, cfg)
	require.Len(t, notifs, 1)
	assert.Equal(t, NotifVMNetworkQoSSRIOV, notifs[0].Code)
}

func TestEvaluateNetworkQoS_SRIOV_HighThroughputNoDrops(t *testing.T) {
	cfg := DefaultVMRecConfig()
	digest := model.DailyVMDigest{
		NetThroughputP95BPS: 6_000_000_000,
		NetPPSP95:           50_000,
		NetDropRatioMaxBP:   0,
	}
	notifs := EvaluateNetworkQoS(digest, true, cfg)
	require.Len(t, notifs, 1)
	assert.Equal(t, NotifVMNetworkQoSSRIOV, notifs[0].Code)
}

func TestEvaluateNetworkQoS_DPDK_SmallPackets(t *testing.T) {
	cfg := DefaultVMRecConfig()
	digest := model.DailyVMDigest{
		NetThroughputP95BPS: 800_000 * 128,
		NetPPSP95:           800_000,
		NetDropRatioMaxBP:   0,
	}
	notifs := EvaluateNetworkQoS(digest, true, cfg)
	require.NotEmpty(t, notifs)
	assert.True(t, vmNotifsContainCode(notifs, NotifVMNetworkQoSDPDK))
}

func TestEvaluateNetworkQoS_DPDK_LargePacketsNoHint(t *testing.T) {
	cfg := DefaultVMRecConfig()
	digest := model.DailyVMDigest{
		NetThroughputP95BPS: 800_000 * 1500,
		NetPPSP95:           800_000,
		NetDropRatioMaxBP:   0,
	}
	notifs := EvaluateNetworkQoS(digest, true, cfg)
	for _, n := range notifs {
		assert.NotEqual(t, NotifVMNetworkQoSDPDK, n.Code)
	}
}

func TestEvaluateNetworkQoS_NotNetworkBound_NoNotifications(t *testing.T) {
	cfg := DefaultVMRecConfig()
	digest := model.DailyVMDigest{
		NetThroughputP95BPS: 6_000_000_000,
		NetPPSP95:           800_000,
		NetDropRatioMaxBP:   500,
	}
	notifs := EvaluateNetworkQoS(digest, false, cfg)
	assert.Empty(t, notifs)
}

func TestEvaluateNetworkQoS_Disabled(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.NetworkQoSEnabled = false
	digest := model.DailyVMDigest{
		NetThroughputP95BPS: 6_000_000_000,
		NetDropRatioMaxBP:   500,
	}
	notifs := EvaluateNetworkQoS(digest, true, cfg)
	assert.Empty(t, notifs)
}

func vmNotifsContainCode(notifs []VMNotification, code int16) bool {
	for _, n := range notifs {
		if n.Code == code {
			return true
		}
	}
	return false
}
