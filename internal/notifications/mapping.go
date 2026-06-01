package notifications

import "fmt"

// NotificationEntry is the Kruize-compatible notification object shape
// expected by koku-ui. Keys are string representations of the code.
type NotificationEntry struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    int16  `json:"code"`
}

type notifDef struct {
	Severity string
	Message  string
}

// Definitions mirrors the seed data from migration 025. If new codes are
// added to notification_code_definitions, this table must be updated in
// tandem — see TestNotificationDefinitionsComplete.
var Definitions = map[int16]notifDef{
	1:  {"WARNING", "Less than 4 days of data available for this workload"},
	2:  {"WARNING", "No new metrics data received for more than 48 hours"},
	3:  {"CRITICAL", "OOM kill events detected within the analysis window"},
	4:  {"WARNING", "PodDisruptionBudgets affect workloads on this MachineSet — review before scaling"},
	5:  {"INFO", "Workload uses less than 1% of requested resources"},
	6:  {"INFO", "Resource change detected matching a previous recommendation"},
	7:  {"INFO", "Less than 24 hours of data — recommendation may be unstable"},
	8:  {"WARNING", "Workload has zero usage for more than 72 hours"},
	9:  {"WARNING", "Memory usage trend suggests capacity risk within 30 days"},
	10: {"INFO", "GPU utilization below threshold — consider MIG or smaller profile"},
	11: {"INFO", "Node resources underutilized — consider consolidation"},
	12: {"WARNING", "Node request overcommit ratio exceeds threshold"},
	13: {"INFO", "Imbalanced CPU/memory utilization — consider different instance family"},
	14: {"WARNING", "MachineAutoscaler at maxReplicas sustained — consider increasing"},
	15: {"INFO", "MachineAutoscaler at minReplicas sustained — consider decreasing"},
	16: {"WARNING", "Frequent scale events — widen stabilization window"},
	17: {"INFO", "MachineSet has variable load but no autoscaler configured"},
	18: {"WARNING", "VM is idle: CPU and memory usage are consistently below thresholds"},
	19: {"WARNING", "VM is oversized: recommended resources are significantly below current allocation"},
	20: {"WARNING", "PVC has zero usage across all intervals"},
	21: {"WARNING", "HPA at maxReplicas sustained — scaling is bottlenecked"},
	22: {"INFO", "Workload is managed by an HPA — replica count recommendations suppressed"},
	23: {"INFO", "Current instance type is not in the cloud catalog — no resizing needed"},
	24: {"INFO", "Current instance type is deprecated — consider migrating to the recommended type"},
	25: {"INFO", "No cost data available — savings estimate not computed"},
	26: {"INFO", "GPU utilization below idle threshold — consider removing GPU request"},
	27: {"INFO", "GPU memory-bound — consider MIG profile with more HBM"},
	28: {"INFO", "No GPU profiling data available — classification limited to frame buffer"},
	29: {"INFO", "PVC capacity significantly exceeds sustained usage — consider shrinking"},
	30: {"WARNING", "PVC usage approaching capacity — consider expanding or investigate growth"},
	31: {"WARNING", "Source PVC was deleted; snapshot may no longer be needed"},
	32: {"INFO", "Snapshot has never been used to restore a volume"},
	33: {"INFO", "Newer snapshot exists for the same PVC"},
	34: {"INFO", "Snapshot older than retention threshold with no known usage"},
	35: {"INFO", "Snapshot is managed by backup tool — review retention policy for cost optimization"},
	36: {"INFO", "GPU time-slicing candidate — workload may benefit from shared GPU scheduling"},
	37: {"WARNING", "Virtual machine disk allocation is growing but guest-agent capacity data is unavailable"},
	38: {"INFO", "QEMU guest agent not installed: recommendations based on hypervisor metrics only (moderate confidence)"},
	39: {"WARNING", "High disk I/O detected: consider storage-optimized instance type or faster storage class"},
	40: {"WARNING", "Filesystem usage growing toward capacity at current growth rate"},
	41: {"INFO", "Recommended instance type available for virtual machine sizing"},
	42: {"CRITICAL", "Filesystem critically full: immediate expansion recommended"},
	43: {"CRITICAL", "VM has zero CPU and memory usage — likely abandoned"},
	44: {"INFO", "Guest agent data interrupted — recommendations use hypervisor metrics"},
	45: {"INFO", "Insufficient metrics — less than one full day of data available"},
	46: {"INFO", "Guest OS not detected — using Linux defaults. Install qemu-guest-agent for OS-specific thresholds."},
	47: {"INFO", "Periodic usage spikes detected (possibly OS updates); P95 sizing accounts for this"},
	48: {"WARNING", "VM restarted multiple times in the observation window — possible instability or crash loop"},
	49: {"INFO", "Downsize recommendation suppressed: usage not consistently below threshold"},
	50: {"WARNING", "GPU is idle — consider removing GPU passthrough/vGPU assignment"},
	51: {"WARNING", "GPU underutilized — see recommended_gpu_action and recommended_gpu_profile"},
	52: {"WARNING", "GPU memory saturated — consider a larger GPU or additional GPU"},
	53: {"WARNING", "GPU compute saturated — workload may benefit from a more powerful GPU"},
	54: {"WARNING", "One or more GPUs are idle while others are active"},
	55: {"WARNING", "Network-saturated workload: recommend n1 network-optimized instance type"},
	56: {"INFO", "vGPU profile recommended — see recommended_vgpu_profile in GPU details"},
	57: {"WARNING", "GPU time-slicing not safe — frame-buffer usage too high for shared vGPU"},
	58: {"INFO", "Sequential disk I/O pattern detected — consider storage optimized for throughput"},
	59: {"INFO", "Random disk I/O pattern detected — consider storage optimized for IOPS"},
}

// MapToKruizeFormat converts native int16 codes into the Kruize-compatible
// notification map expected by koku-ui. Output shape:
// {"<code>": {"type": "...", "message": "...", "code": N}}.
func MapToKruizeFormat(codes []int16) map[string]NotificationEntry {
	if len(codes) == 0 {
		return nil
	}
	result := make(map[string]NotificationEntry, len(codes))
	for _, code := range codes {
		def, ok := Definitions[code]
		if !ok {
			continue
		}
		key := fmt.Sprintf("%d", code)
		result[key] = NotificationEntry{
			Type:    def.Severity,
			Message: def.Message,
			Code:    code,
		}
	}
	return result
}
