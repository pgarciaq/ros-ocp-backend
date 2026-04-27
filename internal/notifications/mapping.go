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
	18: {"WARNING", "Virtual machine has near-zero utilization"},
	19: {"INFO", "Virtual machine allocated resources exceed usage by resize threshold"},
	20: {"WARNING", "PVC has zero usage across all intervals"},
	21: {"WARNING", "HPA at maxReplicas sustained — scaling is bottlenecked"},
	22: {"INFO", "Workload is managed by an HPA — replica count recommendations suppressed"},
	23: {"INFO", "Current instance type is not in the cloud catalog — no resizing needed"},
	24: {"INFO", "Current instance type is deprecated — consider migrating to the recommended type"},
	25: {"INFO", "No cost data available — savings estimate not computed"},
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
