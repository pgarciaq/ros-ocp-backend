package engine

// ClassifyNodeIdleState classifies a node as active, idle, or zombie from utilization
// metrics computed in classifyNode. Zombie is stricter than underutilized (30%):
// near-zero CPU with only system pods. Idle requires low CPU and memory utilization
// with a bounded pod count.
func ClassifyNodeIdleState(class nodeClassification, settings NodeThresholdSettings) IdleState {
	if class.validDays == 0 {
		return IdleStateActive
	}

	if class.maxCPUUsageP95MC < settings.ZombieCPUP95MC && class.PodCount <= settings.ZombieMaxPods {
		return IdleStateZombie
	}

	idleCPUPct := float32(settings.IdleCPUUtilPct) / 100.0
	idleMemPct := float32(settings.IdleMemUtilPct) / 100.0
	if class.CPUUtilP95 < idleCPUPct && class.MemUtilP95 < idleMemPct && class.PodCount <= settings.IdleMaxPods {
		return IdleStateIdle
	}

	return IdleStateActive
}
