package engine

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

const (
	vmGPUActionRemoveGPU         = "remove_gpu"
	vmGPUActionSmallerMIGProfile = "smaller_mig_profile"
	vmGPUActionConsiderVGPUOrMIG = "consider_vgpu_or_mig"
	vmGPUActionLargerGPU         = "larger_gpu"
	vmGPUActionMorePowerfulGPU   = "more_powerful_gpu"
	vmGPUActionNoChange          = "no_change"
)

// vmGPUAnalysis holds GPU classification output for a VM recommendation window.
type vmGPUAnalysis struct {
	Classification        string
	Action                string
	Profile               string
	UtilizationAvgBP      int32
	GPUCount              int32
	GPUModel              string
	MIGProfile            string
	NotificationCodes     []int16
	RequireGPUInstance    bool
	MinGPUMemoryGiB       int32
}

func analyzeVMGPU(digests []model.DailyVMDigest, cfg VMRecConfig) vmGPUAnalysis {
	var out vmGPUAnalysis
	hasGPU := false
	for _, d := range digests {
		if d.HasGPU && d.GPUCount > 0 {
			hasGPU = true
			break
		}
	}
	if !hasGPU {
		return out
	}

	avgSM := vmAverageBP(digests, func(d model.DailyVMDigest) int32 { return d.GPUSMActiveAvgBP })
	avgTensor := vmAverageBP(digests, func(d model.DailyVMDigest) int32 { return d.GPUTensorAvgBP })
	avgUtil := vmAverageBP(digests, func(d model.DailyVMDigest) int32 { return d.GPUUtilAvgBP })
	maxFBUsed := vmMaxFloat(digests, func(d model.DailyVMDigest) float64 { return d.GPUFBUsedMaxMiB })

	out.UtilizationAvgBP = avgUtil
	out.GPUCount = vmLatestGPUCount(digests)
	out.GPUModel = vmLatestGPUModel(digests)
	migProfile := vmLatestMIGProfile(digests)
	out.MIGProfile = migProfile
	isMIG := migProfile != ""

	idleThresholdBP := int32(cfg.GPUIdleThreshold * 10000)
	underutilBP := int32(cfg.GPUUnderutilThreshold * 10000)
	computeSatBP := int32(cfg.GPUComputeSaturationThreshold * 10000)

	fbSatMiB := cfg.GPUFBSaturationMiB
	if fbSatMiB <= 0 {
		if spec := MatchGPUModel(out.GPUModel); spec != nil {
			fbSatMiB = float64(spec.TotalFBMiB) * 0.90
		}
	}

	out.RequireGPUInstance = true
	out.MinGPUMemoryGiB = int32(math.Ceil(maxFBUsed / 1024.0))
	if out.MinGPUMemoryGiB < 1 && maxFBUsed > 0 {
		out.MinGPUMemoryGiB = 1
	}

	switch {
	case avgSM < idleThresholdBP && avgTensor < idleThresholdBP:
		out.Classification = "idle"
		out.Action = vmGPUActionRemoveGPU
		out.NotificationCodes = append(out.NotificationCodes, NotifVMGPUIdle)
	case avgSM < underutilBP:
		out.Classification = "underutilized"
		if isMIG {
			out.Action = vmGPUActionSmallerMIGProfile
			out.Profile = suggestSmallerMIGProfile(migProfile, avgUtil, out.GPUModel)
		} else {
			out.Action = vmGPUActionConsiderVGPUOrMIG
		}
		out.NotificationCodes = append(out.NotificationCodes, NotifVMGPUUnderutilized)
	case fbSatMiB > 0 && maxFBUsed >= fbSatMiB:
		out.Classification = "memory_saturated"
		out.Action = vmGPUActionLargerGPU
		out.NotificationCodes = append(out.NotificationCodes, NotifVMGPUMemorySaturated)
	case avgUtil > computeSatBP:
		out.Classification = "compute_saturated"
		out.Action = vmGPUActionMorePowerfulGPU
		out.NotificationCodes = append(out.NotificationCodes, NotifVMGPUComputeSaturated)
	default:
		out.Classification = "well_utilized"
		out.Action = vmGPUActionNoChange
	}

	return out
}

func vmAverageBP(digests []model.DailyVMDigest, pick func(model.DailyVMDigest) int32) int32 {
	var sum int64
	var n int
	for _, d := range digests {
		if !d.HasGPU {
			continue
		}
		sum += int64(pick(d))
		n++
	}
	if n == 0 {
		return 0
	}
	return int32(sum / int64(n))
}

func vmMaxFloat(digests []model.DailyVMDigest, pick func(model.DailyVMDigest) float64) float64 {
	var max float64
	for _, d := range digests {
		if !d.HasGPU {
			continue
		}
		if v := pick(d); v > max {
			max = v
		}
	}
	return max
}

func vmLatestMIGProfile(digests []model.DailyVMDigest) string {
	latest := latestVMDigest(digests)
	return strings.TrimSpace(latest.GPUMIGProfile)
}

func vmLatestGPUModel(digests []model.DailyVMDigest) string {
	latest := latestVMDigest(digests)
	return strings.TrimSpace(latest.GPUModel)
}

func vmLatestGPUCount(digests []model.DailyVMDigest) int32 {
	latest := latestVMDigest(digests)
	return latest.GPUCount
}

func suggestSmallerMIGProfile(currentProfile string, avgUtilBP int32, modelName string) string {
	spec := MatchGPUModel(modelName)
	if spec == nil || !spec.MIGSupported || len(spec.Profiles) == 0 {
		return ""
	}
	currentIdx := -1
	for i, p := range spec.Profiles {
		if p.Name == currentProfile {
			currentIdx = i
			break
		}
	}
	if currentIdx <= 0 {
		return spec.Profiles[0].Name
	}
	// Step down one profile when utilization is very low; otherwise keep current.
	if avgUtilBP < 1000 {
		return spec.Profiles[currentIdx-1].Name
	}
	return currentProfile
}

func appendVMGPUNotifications(existing []byte, codes []int16) []byte {
	if len(codes) == 0 {
		return existing
	}
	var notifs []VMNotification
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &notifs)
	}
	for _, code := range codes {
		notifs = append(notifs, vmNotificationForGPUCode(code))
	}
	b, err := json.Marshal(notifs)
	if err != nil {
		return existing
	}
	return b
}

func vmNotificationForGPUCode(code int16) VMNotification {
	switch code {
	case NotifVMGPUIdle:
		return VMNotification{
			Code:    NotifVMGPUIdle,
			Type:    vmNotifTypeWarning,
			Message: "GPU is idle — consider removing GPU passthrough/vGPU assignment",
		}
	case NotifVMGPUUnderutilized:
		return VMNotification{
			Code:    NotifVMGPUUnderutilized,
			Type:    vmNotifTypeWarning,
			Message: "GPU underutilized — consider a smaller vGPU profile or MIG partition",
		}
	case NotifVMGPUMemorySaturated:
		return VMNotification{
			Code:    NotifVMGPUMemorySaturated,
			Type:    vmNotifTypeWarning,
			Message: "GPU memory saturated — consider a larger GPU or additional GPU",
		}
	case NotifVMGPUComputeSaturated:
		return VMNotification{
			Code:    NotifVMGPUComputeSaturated,
			Type:    vmNotifTypeWarning,
			Message: "GPU compute saturated — workload may benefit from a more powerful GPU",
		}
	default:
		return VMNotification{Code: code, Type: vmNotifTypeInfo, Message: "GPU recommendation"}
	}
}
