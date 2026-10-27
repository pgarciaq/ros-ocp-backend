// Package hcp implements the HCP (HyperShift hosted control plane) plugin (#630).
//
// HCP control-plane pods run as ordinary pods in per-hosted-cluster namespaces
// on a management cluster. This plugin observes container CSV ingestion and
// counts HCP-namespace rows plus workload-inventory tripwire misses; it writes
// nothing. Guardrail floors live in librobne/hcp (pure helpers) and are
// applied at recommend time by librobne/engine (CLI) and the server container
// path (#631 owns threading the HCP set server-side).
//
// # Traits Implemented
//
//   - [plugin.IngestHook] — observes "container" rows after ingestion
//   - [plugin.CSVIngestor] — claims "hcp-capacity" CSV type
//     (ros-openshift-hcp-capacity-): W4 fleet-capacity snapshots (#392),
//     parsed and persisted as history rows by internal/ingestion
//   - [plugin.TermProvider] — configurable short/medium/long terms (max 90 days,
//     same 1/7/15 defaults as other fast-moving plugins, independent rows)
//
// No RetentionProvider: the core history sweep owns hcp_capacity_history
// (the RetentionProvider trait carries only the month cutoff — #701; a
// plugin sweep would silently deliver 6 months instead of 90 days).
package hcp

import (
	"context"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	rosapi "github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
	hcplib "github.com/redhatinsights/ros-ocp-backend/librobne/hcp"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// HCPPlugin observes HCP-namespace container ingestion for tripwire telemetry.
type HCPPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&HCPPlugin{})
}

// Name returns the stable plugin identifier used for registry lookups,
// ROS_ENABLED_PLUGINS filtering, capabilities, and log fields.
func (p *HCPPlugin) Name() string { return "hcp" }

// Requires declares the container dependency: the hook reads container CSV
// rows. Validated fail-fast at startup; never auto-enabled.
func (p *HCPPlugin) Requires() []string { return []string{"container"} }

func (p *HCPPlugin) Enabled() bool { return plugin.EnabledFor(p.Name()) }

func (p *HCPPlugin) HookAfterCSVTypes() []string {
	return []string{"container"}
}

// SupportedCSVTypes claims the W4 fleet-capacity snapshot (#392). The
// registry's collision check (validateCSVTypeClaims) guarantees no other
// plugin claims this type.
func (p *HCPPlugin) SupportedCSVTypes() []string {
	return []string{string(types.PayloadTypeHCPCapacity)}
}

// IngestCSV parses the W4 fleet-capacity snapshot and persists history
// rows; no hooks consume the data, so no MetricRow output.
func (p *HCPPlugin) IngestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]ingestion.MetricRow, error) {
	if err := ingestion.ProcessHCPCapacityCSV(ctx, pool, r, orgID, clusterUUID); err != nil {
		return nil, err
	}
	return nil, nil
}

// RegisterRoutes serves the dedicated HCP surface (#638). The no-route guard
// test from #630 retired with this method: routes exist exactly when enabled.
func (p *HCPPlugin) RegisterRoutes(g *echo.Group) {
	if plugin.EnabledFor(plugin.KruizePluginName) {
		return
	}
	g.GET("/recommendations/openshift/hcp", rosapi.GetHCPRecommendationSetList)
	// W4 fleet-capacity advisory (#392). Static segments win over
	// :recommendation-id in echo's router; registering first keeps that
	// explicit.
	g.GET("/recommendations/openshift/hcp/fleet-capacity", rosapi.GetHCPCapacityLatest)
	g.GET("/recommendations/openshift/hcp/fleet-capacity/history", rosapi.GetHCPCapacityHistory)
	g.GET("/recommendations/openshift/hcp/:recommendation-id", rosapi.GetHCPRecommendationSet)
}

// observeRows is the pure counting core of AfterIngest: how many rows fall in
// known HCP namespaces, and which distinct HCP workloads sit outside the
// pinned inventory (tripwire). Blank workloads classify by namespace and never
// trip (see hcplib.UnknownWorkloads).
func observeRows(rows []ingestion.MetricRow, hcpNamespaces map[string]bool) (hcpRows int, unknown []string) {
	var hcpSubset []ingestion.MetricRow
	for _, row := range rows {
		if !hcplib.GuardrailFor(row.Namespace, hcpNamespaces) {
			continue
		}
		hcpRows++
		hcpSubset = append(hcpSubset, row)
	}
	workloadNames := make([]string, 0, len(hcpSubset))
	for _, row := range hcpSubset {
		workloadNames = append(workloadNames, row.WorkloadName)
	}
	return hcpRows, hcplib.UnknownWorkloads(workloadNames)
}

// AfterIngest resolves the persisted HCP namespace set once per ingest (one
// SELECT, never per-row) and records tripwire telemetry. It never fails the
// run: missing rows, unreadable state, and empty sets degrade to no
// observation. The latest-row limitation is documented: #621 upgrades the
// source to a report-scoped snapshot.
func (p *HCPPlugin) AfterIngest(ctx context.Context, pool *pgxpool.Pool, rows []ingestion.MetricRow, orgID, clusterUUID string) error {
	if pool == nil || len(rows) == 0 {
		return nil
	}
	maxLookbackDays := config.GetConfig().MaxLookbackDays
	if maxLookbackDays <= 0 {
		maxLookbackDays = 14
	}
	namespaces, err := pgrec.ReadHCPNamespaces(ctx, pool, orgID, clusterUUID, time.Now().UTC().AddDate(0, 0, -maxLookbackDays))
	if err != nil {
		logging.ForOrg(orgID, clusterUUID).Warnf("hcp: unable to read hcp namespaces (tripwire off): %v", err)
		return nil
	}
	set := hcplib.NewNamespaceSet(namespaces)
	if len(set) == 0 {
		return nil
	}
	hcpRows, unknown := observeRows(rows, set)
	metrics.HCPNamespaceRowsTotal.Add(float64(hcpRows))
	metrics.HCPPinMissTotal.Add(float64(len(unknown)))
	if len(unknown) > 0 {
		sample := unknown
		if len(sample) > 5 {
			sample = sample[:5]
		}
		logging.ForOrg(orgID, clusterUUID).Warnf("hcp: %d workload(s) in HCP namespaces outside pinned inventory (sample: %v)", len(unknown), sample)
	}
	return nil
}

func (p *HCPPlugin) DefaultTerms() []plugin.TermConfig {
	return []plugin.TermConfig{
		{Name: "short", WindowDays: 1, MinDataDays: 1, DecayHalfLifeHours: 0},
		{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168},
		{Name: "long", WindowDays: 15, MinDataDays: 7, DecayHalfLifeHours: 360},
	}
}

func (p *HCPPlugin) MaxWindowDays() int { return 90 }
