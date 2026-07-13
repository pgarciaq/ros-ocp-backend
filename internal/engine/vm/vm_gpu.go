package vm

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

const (
	vmGPUActionRemoveGPU         = "remove_gpu"
	vmGPUActionSmallerMIGProfile = "smaller_mig_profile"
	vmGPUActionUseMIGProfile     = "use_mig_profile"
	vmGPUActionConsiderVGPUOrMIG = "consider_vgpu_or_mig"
	vmGPUActionEnableTimeSlicing = "enable_time_slicing"
	vmGPUActionLargerGPU         = "larger_gpu"
	vmGPUActionMorePowerfulGPU   = "more_powerful_gpu"
	vmGPUActionNoChange          = "no_change"
)

// vmGPUAnalysis holds GPU classification output for a VM recommendation window.
type vmGPUAnalysis struct {
	Classification            string
	Action                    string
	Profile                   string
	UtilizationAvgBP          int32
	GPUCount                  int32
	ActiveGPUCount            int32
	GPUModel                  string
	MIGProfile                string
	RecommendedTimeSliceCount int32
	GPUTimeSliceConfidence    string
	GPUTimeSliceRationale     string
	RecommendedVGPUProfile    string
	GPUDevices                []model.GPUDeviceDigest
	NotificationCodes         []int16
	RequireGPUInstance        bool
	MinGPUMemoryGiB           int32
}

type vmDeviceClassification struct {
	classification      string
	action              string
	profile             string
	timeSlices          int32
	timeSliceConfidence string
	timeSliceRationale  string
	vgpuProfile         string
	fbTimeSliceUnsafe   bool
	severity            int // higher = worse (more resource-hungry)
}

// AnalyzeVMGPU runs GPU classification for API detail enrichment.
func AnalyzeVMGPU(digests []model.DailyVMDigest, cfg VMRecConfig) vmGPUAnalysis {
	return analyzeVMGPU(digests, cfg)
}

func analyzeVMGPU(digests []model.DailyVMDigest, cfg VMRecConfig) vmGPUAnalysis {
	var out vmGPUAnalysis
	if !vmWindowHasGPU(digests) {
		return out
	}

	devices := vmAggregateGPUDevices(digests)
	out.GPUDevices = devices
	out.GPUCount = int32(len(devices))
	if out.GPUCount == 0 {
		out.GPUCount = vmLatestGPUCount(digests)
	}
	out.GPUModel = vmLatestGPUModel(digests)
	out.MIGProfile = vmLatestMIGProfile(digests)

	obsDays := vmGPUObservationDays(digests)

	var (
		worst       vmDeviceClassification
		idleCount   int
		activeCount int
		maxFBUsed   float64
		sumUtilBP   int64
	)
	for i, dev := range devices {
		cls := classifyGPUDevice(dev, cfg, obsDays)
		if cls.classification == "idle" {
			idleCount++
		} else {
			activeCount++
		}
		if dev.FBUsedMaxMiB > maxFBUsed {
			maxFBUsed = dev.FBUsedMaxMiB
		}
		sumUtilBP += int64(dev.UtilAvgBP)
		if i == 0 || cls.severity > worst.severity ||
			(cls.severity == worst.severity && vmGPUActionPriority(cls.action) > vmGPUActionPriority(worst.action)) {
			worst = cls
		}
	}
	out.ActiveGPUCount = int32(activeCount)
	if len(devices) > 0 {
		out.UtilizationAvgBP = int32(sumUtilBP / int64(len(devices)))
	} else {
		out.UtilizationAvgBP = vmAverageBP(digests, func(d model.DailyVMDigest) int32 { return d.GPUUtilAvgBP })
		maxFBUsed = vmMaxFloat(digests, func(d model.DailyVMDigest) float64 { return d.GPUFBUsedMaxMiB })
		worst = classifyGPULegacyAggregate(digests, cfg, maxFBUsed, out.UtilizationAvgBP, obsDays)
	}

	out.Classification = worst.classification
	out.Action = worst.action
	out.Profile = worst.profile
	out.RecommendedTimeSliceCount = worst.timeSlices
	out.GPUTimeSliceConfidence = worst.timeSliceConfidence
	out.GPUTimeSliceRationale = worst.timeSliceRationale
	out.RecommendedVGPUProfile = worst.vgpuProfile
	if out.RecommendedVGPUProfile == "" && len(devices) > 0 && worst.classification == "underutilized" {
		ts := RecommendVMTimeSlicing(devices, obsDays, cfg)
		out.RecommendedVGPUProfile = ts.RecommendedVGPUProfile
		if out.GPUTimeSliceConfidence == "" {
			out.GPUTimeSliceConfidence = ts.Confidence
		}
		if out.GPUTimeSliceRationale == "" {
			out.GPUTimeSliceRationale = ts.Rationale
		}
	}

	if idleCount > 0 && activeCount > 0 {
		out.NotificationCodes = append(out.NotificationCodes, NotifVMGPUMixedIdle)
	}
	for _, code := range vmGPUClassificationNotificationCodes(worst.classification) {
		out.NotificationCodes = append(out.NotificationCodes, code)
	}
	if out.RecommendedVGPUProfile != "" &&
		(worst.action == vmGPUActionEnableTimeSlicing || worst.action == vmGPUActionConsiderVGPUOrMIG) {
		out.NotificationCodes = append(out.NotificationCodes, NotifVMVGPUProfileRecommended)
	}
	if worst.fbTimeSliceUnsafe {
		out.NotificationCodes = append(out.NotificationCodes, NotifVMGPUTimeSliceUnsafeFB)
	}

	out.RequireGPUInstance = activeCount > 0 || out.GPUCount > 0
	out.MinGPUMemoryGiB = int32(math.Ceil(maxFBUsed / 1024.0))
	if out.MinGPUMemoryGiB < 1 && maxFBUsed > 0 {
		out.MinGPUMemoryGiB = 1
	}
	if activeCount > 0 {
		out.GPUCount = int32(activeCount)
	}
	return out
}

func vmWindowHasGPU(digests []model.DailyVMDigest) bool {
	for _, d := range digests {
		if d.HasGPU && d.GPUCount > 0 {
			return true
		}
		if len(d.Devices) > 0 {
			return true
		}
	}
	return false
}

func vmAggregateGPUDevices(digests []model.DailyVMDigest) []model.GPUDeviceDigest {
	type acc struct {
		model.GPUDeviceDigest
		utilAvg   []int32
		smAvg     []int32
		tensorAvg []int32
		dramAvg   []int32
	}
	byUUID := make(map[string]*acc)

	for _, d := range digests {
		devs := vmParseGPUDevices(d)
		for _, dev := range devs {
			uuid := strings.TrimSpace(dev.UUID)
			if uuid == "" {
				continue
			}
			a, ok := byUUID[uuid]
			if !ok {
				a = &acc{GPUDeviceDigest: dev}
				byUUID[uuid] = a
			}
			if dev.Model != "" {
				a.Model = dev.Model
			}
			if dev.MIGProfile != "" {
				a.MIGProfile = dev.MIGProfile
			}
			if dev.MaxSlices > a.MaxSlices {
				a.MaxSlices = dev.MaxSlices
			}
			a.utilAvg = append(a.utilAvg, dev.UtilAvgBP)
			a.smAvg = append(a.smAvg, dev.SMActiveAvgBP)
			a.tensorAvg = append(a.tensorAvg, dev.TensorAvgBP)
			a.dramAvg = append(a.dramAvg, dev.DRAMAvgBP)
			if dev.UtilMaxBP > a.UtilMaxBP {
				a.UtilMaxBP = dev.UtilMaxBP
			}
			if dev.FBUsedMaxMiB > a.FBUsedMaxMiB {
				a.FBUsedMaxMiB = dev.FBUsedMaxMiB
			}
			if dev.FBUsedAvgMiB > 0 {
				a.FBUsedAvgMiB = dev.FBUsedAvgMiB
			}
		}
	}

	out := make([]model.GPUDeviceDigest, 0, len(byUUID))
	for _, a := range byUUID {
		if len(a.utilAvg) > 0 {
			var sum int64
			for _, v := range a.utilAvg {
				sum += int64(v)
			}
			a.UtilAvgBP = int32(sum / int64(len(a.utilAvg)))
		}
		if len(a.smAvg) > 0 {
			var sum int64
			for _, v := range a.smAvg {
				sum += int64(v)
			}
			a.SMActiveAvgBP = int32(sum / int64(len(a.smAvg)))
		}
		if len(a.tensorAvg) > 0 {
			var sum int64
			for _, v := range a.tensorAvg {
				sum += int64(v)
			}
			a.TensorAvgBP = int32(sum / int64(len(a.tensorAvg)))
		}
		if len(a.dramAvg) > 0 {
			var sum int64
			for _, v := range a.dramAvg {
				sum += int64(v)
			}
			a.DRAMAvgBP = int32(sum / int64(len(a.dramAvg)))
		}
		out = append(out, a.GPUDeviceDigest)
	}
	return out
}

func vmParseGPUDevices(d model.DailyVMDigest) []model.GPUDeviceDigest {
	if len(d.Devices) > 0 {
		return d.Devices
	}
	if !d.HasGPU || d.GPUCount <= 0 {
		return nil
	}
	return []model.GPUDeviceDigest{{
		UUID:          "gpu-0",
		Model:         d.GPUModel,
		UtilAvgBP:     d.GPUUtilAvgBP,
		UtilMaxBP:     d.GPUUtilMaxBP,
		FBUsedAvgMiB:  d.GPUFBUsedAvgMiB,
		FBUsedMaxMiB:  d.GPUFBUsedMaxMiB,
		SMActiveAvgBP: d.GPUSMActiveAvgBP,
		TensorAvgBP:   d.GPUTensorAvgBP,
		DRAMAvgBP:     d.GPUDRAMAvgBP,
		MIGProfile:    d.GPUMIGProfile,
		MaxSlices:     d.GPUMaxSlices,
	}}
}

func vmCanonicalGPUModel(modelName string) string {
	if spec := engine.MatchGPUModel(modelName); spec != nil {
		return spec.Name
	}
	return modelName
}

func classifyGPUDevice(dev model.GPUDeviceDigest, cfg VMRecConfig, observationDays int) vmDeviceClassification {
	dev.Model = vmCanonicalGPUModel(dev.Model)
	idleThresholdBP := int32(cfg.GPUIdleThreshold * 10000)
	underutilBP := int32(cfg.GPUUnderutilThreshold * 10000)
	computeSatBP := int32(cfg.GPUComputeSaturationThreshold * 10000)

	fbSatMiB := cfg.GPUFBSaturationMiB
	if fbSatMiB <= 0 {
		if spec := engine.MatchGPUModel(dev.Model); spec != nil {
			fbSatMiB = float64(spec.TotalFBMiB) * 0.90
		}
	}

	migProfile := strings.TrimSpace(dev.MIGProfile)
	isMIG := migProfile != ""

	switch {
	case dev.SMActiveAvgBP < idleThresholdBP && dev.TensorAvgBP < idleThresholdBP:
		return vmDeviceClassification{classification: "idle", action: vmGPUActionRemoveGPU, severity: 1}
	case dev.SMActiveAvgBP < underutilBP:
		spec := engine.MatchGPUModel(dev.Model)
		migCapable := isMIG || dev.MaxSlices > 0 || (spec != nil && spec.MIGSupported)
		if migCapable {
			profile := OptimalMIGProfile(dev.Model, migProfile, dev.FBUsedMaxMiB, dev.UtilAvgBP)
			if profile == "" && dev.MaxSlices > 0 {
				profile = migProfileForSliceCount(dev.Model, recommendedMIGSliceCount(dev.UtilAvgBP, dev.MaxSlices))
			}
			return vmDeviceClassification{
				classification: "underutilized",
				action:         vmGPUActionUseMIGProfile,
				profile:        profile,
				severity:       2,
			}
		}
		ts := RecommendVMTimeSlicingForDevice(dev, observationDays, cfg)
		if ts.EnableTimeSlicing && ts.RecommendedSliceCount >= cfg.GPUTimeSliceMinReplicas {
			return vmDeviceClassification{
				classification:      "underutilized",
				action:              vmGPUActionEnableTimeSlicing,
				timeSlices:          ts.RecommendedSliceCount,
				timeSliceConfidence: ts.Confidence,
				timeSliceRationale:  ts.Rationale,
				vgpuProfile:         ts.RecommendedVGPUProfile,
				severity:            2,
			}
		}
		action := vmGPUActionConsiderVGPUOrMIG
		if ts.FBUnsafe {
			action = vmGPUActionConsiderVGPUOrMIG
		}
		return vmDeviceClassification{
			classification:      "underutilized",
			action:              action,
			profile:             ts.RecommendedVGPUProfile,
			timeSliceConfidence: ts.Confidence,
			timeSliceRationale:  ts.Rationale,
			vgpuProfile:         ts.RecommendedVGPUProfile,
			fbTimeSliceUnsafe:   ts.FBUnsafe,
			severity:            2,
		}
	case fbSatMiB > 0 && dev.FBUsedMaxMiB >= fbSatMiB:
		return vmDeviceClassification{classification: "memory_saturated", action: vmGPUActionLargerGPU, severity: 4}
	case dev.UtilAvgBP > computeSatBP:
		return vmDeviceClassification{classification: "compute_saturated", action: vmGPUActionMorePowerfulGPU, severity: 3}
	default:
		return vmDeviceClassification{classification: "well_utilized", action: vmGPUActionNoChange, severity: 0}
	}
}

func classifyGPULegacyAggregate(digests []model.DailyVMDigest, cfg VMRecConfig, maxFB float64, avgUtil int32, observationDays int) vmDeviceClassification {
	dev := model.GPUDeviceDigest{
		UUID:          "gpu-0",
		Model:         vmLatestGPUModel(digests),
		UtilAvgBP:     avgUtil,
		FBUsedMaxMiB:  maxFB,
		SMActiveAvgBP: vmAverageBP(digests, func(d model.DailyVMDigest) int32 { return d.GPUSMActiveAvgBP }),
		TensorAvgBP:   vmAverageBP(digests, func(d model.DailyVMDigest) int32 { return d.GPUTensorAvgBP }),
		DRAMAvgBP:     vmAverageBP(digests, func(d model.DailyVMDigest) int32 { return d.GPUDRAMAvgBP }),
		MIGProfile:    vmLatestMIGProfile(digests),
		MaxSlices:     vmLatestMaxSlices(digests),
	}
	return classifyGPUDevice(dev, cfg, observationDays)
}

func migProfileForSliceCount(modelName string, slices int32) string {
	spec := engine.MatchGPUModel(modelName)
	if spec == nil {
		return ""
	}
	for _, p := range spec.Profiles {
		if int32(p.Slices) == slices {
			return p.Name
		}
	}
	if len(spec.Profiles) > 0 && slices > 0 {
		best := spec.Profiles[0]
		for _, p := range spec.Profiles {
			if int32(p.Slices) <= slices && p.Slices > best.Slices {
				best = p
			}
		}
		return best.Name
	}
	return ""
}

func vmGPUActionPriority(action string) int {
	switch action {
	case vmGPUActionMorePowerfulGPU, vmGPUActionLargerGPU:
		return 4
	case vmGPUActionUseMIGProfile, vmGPUActionSmallerMIGProfile:
		return 3
	case vmGPUActionEnableTimeSlicing, vmGPUActionConsiderVGPUOrMIG:
		return 2
	case vmGPUActionRemoveGPU:
		return 1
	default:
		return 0
	}
}

func vmGPUClassificationNotificationCodes(classification string) []int16 {
	switch classification {
	case "idle":
		return []int16{NotifVMGPUIdle}
	case "underutilized":
		return []int16{NotifVMGPUUnderutilized}
	case "memory_saturated":
		return []int16{NotifVMGPUMemorySaturated}
	case "compute_saturated":
		return []int16{NotifVMGPUComputeSaturated}
	default:
		return nil
	}
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

func vmLatestMaxSlices(digests []model.DailyVMDigest) int32 {
	latest := latestVMDigest(digests)
	return latest.GPUMaxSlices
}

func appendVMGPUNotifications(existing []byte, codes []int16) []byte {
	if len(codes) == 0 {
		return existing
	}
	var notifs []VMNotification
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &notifs)
	}
	seen := make(map[int16]struct{}, len(notifs))
	for _, n := range notifs {
		seen[n.Code] = struct{}{}
	}
	for _, code := range codes {
		if _, ok := seen[code]; ok {
			continue
		}
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
			Message: "GPU underutilized — see recommended_gpu_action and recommended_gpu_profile for MIG or time-slicing guidance",
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
	case NotifVMGPUMixedIdle:
		return VMNotification{
			Code:    NotifVMGPUMixedIdle,
			Type:    vmNotifTypeWarning,
			Message: "One or more GPUs are idle while others are active — consider reducing GPU count",
		}
	case NotifVMVGPUProfileRecommended:
		return VMNotification{
			Code:    NotifVMVGPUProfileRecommended,
			Type:    vmNotifTypeInfo,
			Message: "vGPU profile recommended — see recommended_vgpu_profile in GPU details",
		}
	case NotifVMGPUTimeSliceUnsafeFB:
		return VMNotification{
			Code:    NotifVMGPUTimeSliceUnsafeFB,
			Type:    vmNotifTypeWarning,
			Message: "GPU time-slicing not safe — frame-buffer usage too high for shared vGPU",
		}
	default:
		return VMNotification{Code: code, Type: vmNotifTypeInfo, Message: "GPU recommendation"}
	}
}
