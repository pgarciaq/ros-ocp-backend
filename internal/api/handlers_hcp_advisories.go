package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
)

// HCP advisories serving API (#717). Contract: ADR-0349 — typed per-verdict
// details (names derived from the repo's field conventions, additive-only
// evolution), one advisory per (org, HC, window) by predicate partition,
// nullable hosted_cluster_id for management-cluster-scoped verdicts
// (api-tax writes hc_cluster_id=''), no pagination in v1 (documented cap).

// hcpAdvisoriesMaxRows is the documented response cap. Per-org advisory
// volume is a handful of rows; the cap exists to bound worst cases, not to
// paginate (ADR-0349 §4).
const hcpAdvisoriesMaxRows = 500

// Verdict values — public constants (ADR-0349 §4).
const (
	advisoryVerdictHeadroom = "do_not_add_workers_first"
	advisoryVerdictAPITax   = "tune_noisy_webhook"
	advisoryVerdictZombie   = "review_unused_hosted_cluster"
	advisoryVerdictSleep    = "schedule_nonprod_sleep"
)

// HCPAdvisory is one served advisory row. Details carries the per-verdict
// typed fields; Evidence carries the correlator's raw n_signals for
// provenance and for verdicts not yet mapped.
type HCPAdvisory struct {
	HostedClusterID       *string        `json:"hosted_cluster_id"`
	ManagementClusterUUID string         `json:"management_cluster_uuid"`
	Verdict               string         `json:"verdict"`
	Confidence            string         `json:"confidence"`
	WindowStart           time.Time      `json:"window_start"`
	WindowEnd             time.Time      `json:"window_end"`
	ExpiresAt             time.Time      `json:"expires_at"`
	Details               map[string]any `json:"details"`
	Evidence              []string       `json:"evidence"`
}

// HCPAdvisoriesMeta is the response meta block.
type HCPAdvisoriesMeta struct {
	Count int `json:"count"`
}

// HCPAdvisoriesResponse is the JSON payload for
// GET /recommendations/openshift/hcp/advisories.
type HCPAdvisoriesResponse struct {
	Meta HCPAdvisoriesMeta `json:"meta"`
	Data []HCPAdvisory     `json:"data"`
}

// advisorySignalValue returns the value after "key:" in the correlator's
// n_signals entries ("cp_req_mc:3980"). Values may carry trailing context
// ("api_max_daily:13870175 (context, no gate)") — the caller trims further.
func advisorySignalValue(signals []string, key string) (string, bool) {
	prefix := key + ":"
	for _, s := range signals {
		if strings.HasPrefix(s, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(s, prefix)), true
		}
	}
	return "", false
}

// advisorySignalInt parses the leading integer of a signal value; returns
// false when absent or non-numeric (e.g. "unknown (context...)").
func advisorySignalInt(signals []string, key string) (int64, bool) {
	raw, ok := advisorySignalValue(signals, key)
	if !ok || raw == "" {
		return 0, false
	}
	numericPrefix := raw
	if idx := strings.IndexAny(raw, " \t"); idx >= 0 {
		numericPrefix = raw[:idx]
	}
	n, err := strconv.ParseInt(numericPrefix, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// hcpAdvisoryDetails maps the table's per-verdict overloaded columns
// (apitax precedent) into typed fields per ADR-0349's field table.
func hcpAdvisoryDetails(verdict string, p99, threshold float64, cRatio *float64, signals []string) map[string]any {
	details := map[string]any{}
	switch verdict {
	case advisoryVerdictZombie:
		// Zombie columns: h_p99_s carries peak daily user CPU (mC),
		// h_threshold_s the idle floor (mC), c_ratio NULL (ADR-0339).
		details["peak_user_cpu_mc"] = int64(p99)
		details["idle_floor_cpu_mc"] = int64(threshold)
		if v, ok := advisorySignalInt(signals, "cp_req_mc"); ok {
			details["control_plane_cpu_request_mc"] = v
		}
		if v, ok := advisorySignalInt(signals, "api_max_daily"); ok {
			details["api_max_daily_requests"] = v
		}
	case advisoryVerdictHeadroom:
		// Headroom columns carry their own semantics: hosted API latency
		// p99 and threshold in seconds, CP capacity ratio.
		details["hosted_api_p99_seconds"] = p99
		details["hosted_api_threshold_seconds"] = threshold
		if cRatio != nil {
			details["control_plane_capacity_ratio"] = *cRatio
		}
	case advisoryVerdictAPITax:
		// Api-tax columns: h_p99_s the webhook p99, h_threshold_s the
		// p99 threshold, c_ratio the rejected rate (writeAPITaxAdvisory).
		details["webhook_p99_seconds"] = p99
		details["webhook_p99_threshold_seconds"] = threshold
		if cRatio != nil {
			details["webhook_rejected_rate"] = *cRatio
		}
		if v, ok := advisorySignalValue(signals, "webhook"); ok {
			details["webhook"] = v
		}
	case advisoryVerdictSleep:
		// Sleep columns: h_p99_s carries the BH-activity peak (mC),
		// h_threshold_s the outside-BH peak (mC) (writeSleepAdvisory);
		// the reclaim estimate parses from signals (ADR-0349 field table).
		details["bh_active_peak_cpu_mc"] = int64(p99)
		details["outside_bh_peak_cpu_mc"] = int64(threshold)
		if v, ok := advisorySignalValue(signals, "declared_tier"); ok {
			details["declared_tier"] = v
		}
		if v, ok := advisorySignalInt(signals, "worker_nodes"); ok {
			details["worker_node_count"] = v
		}
		if raw, ok := advisorySignalValue(signals, "reclaim_node_hours_per_week"); ok {
			if f, err := strconv.ParseFloat(raw, 64); err == nil {
				details["reclaim_node_hours_per_week"] = f
			}
		}
	default:
		// Unmapped (future) verdict: evidence carries everything until a
		// typed mapping is added — additive by design (ADR-0349 §1).
	}
	return details
}

// GetHCPAdvisories returns the org's live HCP advisories (all verdicts).
func GetHCPAdvisories(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	rows, err := pool.Query(c.Request().Context(), `
		SELECT hc_cluster_id, management_cluster_uuid::text, verdict, confidence,
			window_start, window_end, expires_at,
			h_p99_s::float8, h_threshold_s::float8, c_ratio::float8, n_signals
		FROM hcp_correlation_advisories
		WHERE org_id = $1 AND expires_at > now()
		ORDER BY window_start DESC, hc_cluster_id
		LIMIT $2`,
		orgID, hcpAdvisoriesMaxRows)
	if err != nil {
		hlog.Errorf("hcp advisories: query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch advisories",
		})
	}
	defer rows.Close()

	resp := HCPAdvisoriesResponse{Data: []HCPAdvisory{}}
	for rows.Next() {
		var a HCPAdvisory
		var hcID string
		var p99, threshold float64
		var cRatio *float64
		var signals []string
		if err := rows.Scan(&hcID, &a.ManagementClusterUUID, &a.Verdict, &a.Confidence,
			&a.WindowStart, &a.WindowEnd, &a.ExpiresAt,
			&p99, &threshold, &cRatio, &signals); err != nil {
			hlog.Errorf("hcp advisories: scan failed: %v", err)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to fetch advisories",
			})
		}
		if hcID != "" {
			a.HostedClusterID = &hcID
		}
		a.Details = hcpAdvisoryDetails(a.Verdict, p99, threshold, cRatio, signals)
		a.Evidence = signals
		if a.Evidence == nil {
			a.Evidence = []string{}
		}
		resp.Data = append(resp.Data, a)
	}
	if err := rows.Err(); err != nil {
		hlog.Errorf("hcp advisories: rows error: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch advisories",
		})
	}
	resp.Meta.Count = len(resp.Data)
	return c.JSON(http.StatusOK, resp)
}
