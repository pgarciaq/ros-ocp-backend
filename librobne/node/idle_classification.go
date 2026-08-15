package node

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

// ClassifyNodeIdleState classifies a node as active, idle, or zombie from utilization
// metrics computed in classifyNode. Zombie is stricter than underutilized (30%):
// near-zero CPU with only system pods. Idle requires low CPU and memory utilization
// with a bounded pod count.
func ClassifyNodeIdleState(class nodeClassification, settings ThresholdSettings) types.IdleState {
	if class.validDays == 0 {
		return types.IdleStateActive
	}

	if class.maxCPUUsageP95MC < settings.ZombieCPUP95MC && class.PodCount <= settings.ZombieMaxPods {
		return types.IdleStateZombie
	}

	idleCPUPct := float32(settings.IdleCPUUtilPct) / 100.0
	idleMemPct := float32(settings.IdleMemUtilPct) / 100.0
	if class.CPUUtilP95 < idleCPUPct && class.MemUtilP95 < idleMemPct && class.PodCount <= settings.IdleMaxPods {
		return types.IdleStateIdle
	}

	return types.IdleStateActive
}
