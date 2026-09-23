package kruize

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// SynthInput is one typed sibling row feeding SynthesizeKruizeJSON.
// Amounts are native units (millicores, KiB); nil means absent (the
// section is omitted, never fabricated).
type SynthInput struct {
	Term   string
	Engine string

	CurrentCPURequestMC  *int64
	CurrentMemRequestKiB *int64
	CurrentCPULimitMC    *int64
	CurrentMemLimitKiB   *int64

	RecCPURequestMC  *int64
	RecMemRequestKiB *int64
	RecCPULimitMC    *int64
	RecMemLimitKiB   *int64

	MonitoringStartTime time.Time
	MonitoringEndTime   time.Time

	// NotificationCodes renders as Kruize-shaped engine notifications
	// (Phase 2). Nil omits the section.
	NotificationCodes []int16
}

// kruizeTimeFormat matches the legacy blob timestamps (millis, Zulu).
const kruizeTimeFormat = "2006-01-02T15:04:05.000Z"

// SynthesizeKruizeJSON builds a legacy-shaped recommendation blob from
// typed sibling rows (#599 option 2, Phase 1: current + config + variation
// + times; no plots, no notifications).
//
// Variation amounts are ABSOLUTE deltas (rec − current) in matching units:
// UpdateRecommendationJSON recomputes exact percentages from them. Wiring
// MUST pass empty StoredVariationPcts for synthesized blobs — the
// skipRequests path would leave absolute request amounts unconverted.
//
// Missing terms/engines/nil sections are omitted, never fabricated. Empty
// input yields an empty map (same contract as the reader).
func SynthesizeKruizeJSON(rows []SynthInput) map[string]interface{} {
	if len(rows) == 0 {
		return map[string]interface{}{}
	}

	byTerm := map[string]map[string]SynthInput{}
	for _, r := range rows {
		if r.Term == "" || r.Engine == "" {
			continue
		}
		if byTerm[r.Term] == nil {
			byTerm[r.Term] = map[string]SynthInput{}
		}
		byTerm[r.Term][r.Engine] = r
	}

	// Current state comes from the short-term cost row (legacy parity),
	// else the first row carrying current amounts.
	current := pickCurrentRow(rows)

	data := map[string]interface{}{}
	if end := current.MonitoringEndTime; !end.IsZero() {
		data["monitoring_end_time"] = end.UTC().Format(kruizeTimeFormat)
	}
	if cur := synthResourcePair(current.CurrentCPURequestMC, current.CurrentMemRequestKiB, current.CurrentCPULimitMC, current.CurrentMemLimitKiB); cur != nil {
		data["current"] = cur
	}

	terms := map[string]interface{}{}
	for term, engines := range byTerm {
		termObj := map[string]interface{}{}
		var start, end time.Time
		engObj := map[string]interface{}{}
		for engine, r := range engines {
			e := map[string]interface{}{}
			if cfg := synthResourcePair(r.RecCPURequestMC, r.RecMemRequestKiB, r.RecCPULimitMC, r.RecMemLimitKiB); cfg != nil {
				e["config"] = cfg
			}
			if vr := synthVariation(r); vr != nil {
				e["variation"] = vr
			}
			if notifs := notifications.MapToKruizeFormat(r.NotificationCodes); notifs != nil {
				e["notifications"] = notifs
			}
			engObj[engine] = e
			if !r.MonitoringStartTime.IsZero() && (start.IsZero() || r.MonitoringStartTime.Before(start)) {
				start = r.MonitoringStartTime
			}
			if !r.MonitoringEndTime.IsZero() && r.MonitoringEndTime.After(end) {
				end = r.MonitoringEndTime
			}
		}
		if !start.IsZero() {
			termObj["monitoring_start_time"] = start.UTC().Format(kruizeTimeFormat)
		}
		if !start.IsZero() && !end.IsZero() {
			termObj["duration_in_hours"] = end.Sub(start).Hours()
		}
		termObj["recommendation_engines"] = engObj
		terms[term] = termObj
	}
	data["recommendation_terms"] = terms
	return data
}

func pickCurrentRow(rows []SynthInput) SynthInput {
	for _, r := range rows {
		if r.Term == ShortTerm && r.Engine == EngineCost {
			return r
		}
	}
	for _, r := range rows {
		if r.CurrentCPURequestMC != nil || r.CurrentMemRequestKiB != nil {
			return r
		}
	}
	return rows[0]
}

func synthCores(mc *int64) *float64 {
	if mc == nil {
		return nil
	}
	v := float64(*mc) / 1000.0
	return &v
}

func synthBytes(kib *int64) *float64 {
	if kib == nil {
		return nil
	}
	v := float64(*kib) * 1024.0
	return &v
}

func synthAmountObj(amount *float64, format string) map[string]interface{} {
	return map[string]interface{}{"amount": *amount, "format": format}
}

// synthResourcePair builds {requests,limits}×{cpu,memory} with base-unit
// amounts (cores/bytes); either side omitted when fully absent.
func synthResourcePair(reqCPUmc, reqMemKib, limCPUmc, limMemKib *int64) map[string]interface{} {
	mkSide := func(cpuMC, memKiB *int64) map[string]interface{} {
		side := map[string]interface{}{}
		if cpu := synthCores(cpuMC); cpu != nil {
			side["cpu"] = synthAmountObj(cpu, "cores")
		}
		if mem := synthBytes(memKiB); mem != nil {
			side["memory"] = synthAmountObj(mem, "bytes")
		}
		if len(side) == 0 {
			return nil
		}
		return side
	}
	obj := map[string]interface{}{}
	if req := mkSide(reqCPUmc, reqMemKib); req != nil {
		obj["requests"] = req
	}
	if lim := mkSide(limCPUmc, limMemKib); lim != nil {
		obj["limits"] = lim
	}
	if len(obj) == 0 {
		return nil
	}
	return obj
}

// synthVariation emits absolute deltas (rec − current) wherever both ends
// exist, so the reader's percentage recompute stays exact. Subtraction
// happens on int64 before conversion — no float error enters.
func synthVariation(r SynthInput) map[string]interface{} {
	delta := func(rec, cur *int64) *int64 {
		if rec == nil || cur == nil {
			return nil
		}
		v := *rec - *cur
		return &v
	}
	return synthResourcePair(
		delta(r.RecCPURequestMC, r.CurrentCPURequestMC),
		delta(r.RecMemRequestKiB, r.CurrentMemRequestKiB),
		delta(r.RecCPULimitMC, r.CurrentCPULimitMC),
		delta(r.RecMemLimitKiB, r.CurrentMemLimitKiB),
	)
}
