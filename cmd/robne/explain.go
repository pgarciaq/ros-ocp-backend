package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/spf13/cobra"
)

const (
	scheduleAllHours      = "all_hours"
	scheduleBusinessHours = "business_hours"
)

type explainFlags struct {
	commonFlags
	namespace    string
	workload     string
	workloadType string
	container    string
	term         string
	engine       string
	schedule     string
}

func newExplainCmd() *cobra.Command {
	var f explainFlags
	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Show explanation factors for one container recommendation",
		Long: `Re-run the engine from the same --input as recommend and print why one
container recommendation is that number.

recommend JSON is the list (what to apply) and does not include explanation
factors. explain is the detail. It does not read a recommend envelope.

Container-only in this release. Other entity types: issue #490.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runExplain(cmd.OutOrStdout(), f)
		},
	}
	bindCommonFlags(cmd, &f.commonFlags, true, false)
	cmd.Flags().StringVar(&f.pgURLFile, "pg-url-file", "", "file containing a postgres URL (password off argv)")
	cmd.Flags().StringVar(&f.namespace, "namespace", "", "namespace")
	cmd.Flags().StringVar(&f.workload, "workload", "", "workload name")
	cmd.Flags().StringVar(&f.workloadType, "workload-type", "", "workload type (required when the match is not unique)")
	cmd.Flags().StringVar(&f.container, "container", "", "container name")
	cmd.Flags().StringVar(&f.term, "term", "", "term (short, medium, long)")
	cmd.Flags().StringVar(&f.engine, "engine", "", "engine (cost, performance)")
	cmd.Flags().StringVar(&f.schedule, "schedule", "", "all_hours (default) or business_hours")
	_ = cmd.MarkFlagRequired("namespace")
	_ = cmd.MarkFlagRequired("workload")
	_ = cmd.MarkFlagRequired("container")
	_ = cmd.MarkFlagRequired("term")
	_ = cmd.MarkFlagRequired("engine")
	return cmd
}

func runExplain(stdout io.Writer, f explainFlags) error {
	if err := rejectRecommendEnvelopeInput(f.input); err != nil {
		return err
	}
	env, err := overlayEnvFromOS(f.noUserConfig)
	if err != nil {
		return err
	}
	cfg, err := loadFileConfig(env, f.configPath)
	if err != nil {
		return err
	}
	if err := validatePlugins(cfg, f.plugins); err != nil {
		return err
	}
	if pluginsExplicit(cfg, f.plugins) {
		for _, p := range resolvedPlugins(cfg, f.plugins) {
			if p != "container" {
				return fmt.Errorf("explain is container-only (#480); other entity types are #490 (got %q)", p)
			}
		}
	}
	schedule, err := resolveExplainSchedule(f.schedule)
	if err != nil {
		return err
	}
	if schedule == scheduleBusinessHours && !businessHoursEnabled(cfg) {
		return fmt.Errorf("--schedule business_hours requires YAML business_hours.enabled")
	}
	cf := f.commonFlags
	cf.plugins = "container"
	cf.format = "json"
	result, err := executeRecommend(cf)
	if err != nil {
		return err
	}
	recs := result.Recs
	if schedule == scheduleBusinessHours {
		recs = result.BHRecs
	}
	rec, err := selectContainerRec(recs, f)
	if err != nil {
		return err
	}
	out := toContainerExplainOut(rec, schedule)
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func resolveExplainSchedule(flag string) (string, error) {
	s := strings.TrimSpace(flag)
	if s == "" {
		return scheduleAllHours, nil
	}
	switch s {
	case scheduleAllHours, scheduleBusinessHours:
		return s, nil
	default:
		return "", fmt.Errorf("unknown --schedule %q (all_hours or business_hours)", flag)
	}
}

func rejectRecommendEnvelopeInput(path string) error {
	if path == "" || isPostgresURL(path) {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return nil
	}
	f, err := os.Open(path) //nolint:gosec // G304: operator-supplied CLI path
	if err != nil {
		return nil
	}
	defer f.Close()
	var probe struct {
		Version         *int            `json:"version"`
		Recommendations json.RawMessage `json:"recommendations"`
	}
	if err := json.NewDecoder(f).Decode(&probe); err != nil {
		return nil
	}
	if probe.Version != nil || len(probe.Recommendations) > 0 {
		return fmt.Errorf("explain does not read recommend JSON envelopes; pass the same --input as recommend (CSV, directory, tarball, or postgres://)")
	}
	return nil
}

func selectContainerRec(recs []types.ContainerRec, f explainFlags) (types.ContainerRec, error) {
	var matches []types.ContainerRec
	for _, rec := range recs {
		if rec.Namespace != f.namespace || rec.Workload != f.workload || rec.ContainerName != f.container {
			continue
		}
		if rec.Term != f.term || rec.Engine != f.engine {
			continue
		}
		if f.workloadType != "" && rec.WorkloadType != f.workloadType {
			continue
		}
		matches = append(matches, rec)
	}
	if len(matches) == 0 {
		return types.ContainerRec{}, fmt.Errorf(
			"no matching container recommendation for namespace=%s workload=%s container=%s term=%s engine=%s",
			f.namespace, f.workload, f.container, f.term, f.engine,
		)
	}
	if len(matches) > 1 {
		typesSeen := make([]string, 0, len(matches))
		for _, rec := range matches {
			typesSeen = append(typesSeen, rec.WorkloadType)
		}
		return types.ContainerRec{}, fmt.Errorf("match is not unique; pass --workload-type (got %s)", strings.Join(typesSeen, ", "))
	}
	return matches[0], nil
}

// containerExplainOut is the snake_case explain DTO. Do not add json tags on
// types.ContainerExplanationFactors. Do not add these fields to containerOut.
type containerExplainOut struct {
	Namespace             string  `json:"namespace"`
	Workload              string  `json:"workload"`
	WorkloadType          string  `json:"workload_type"`
	ContainerName         string  `json:"container_name"`
	Term                  string  `json:"term"`
	Engine                string  `json:"engine"`
	Schedule              string  `json:"schedule"`
	RecCPURequestMC       int64   `json:"rec_cpu_request_mc"`
	RecCPULimitMC         int64   `json:"rec_cpu_limit_mc"`
	RecMemRequestKiB      int64   `json:"rec_mem_request_kib"`
	RecMemLimitKiB        int64   `json:"rec_mem_limit_kib"`
	CurrentCPURequestMC   int64   `json:"current_cpu_request_mc"`
	CurrentMemRequestKiB  int64   `json:"current_mem_request_kib"`
	EstimatedSavingsCents *int64  `json:"estimated_savings_cents"`
	Stale                 bool    `json:"stale"`
	IdleState             string  `json:"idle_state"`
	Category              string  `json:"category"`
	DataDays              int     `json:"data_days"`
	DecayHalfLifeHours    float64 `json:"decay_half_life_hours"`
	CPUCostPctMC          int64   `json:"cpu_cost_pct_mc"`
	CPUPerfPctMC          int64   `json:"cpu_perf_pct_mc"`
	CPUUsageP95MC         int64   `json:"cpu_usage_p95_mc"`
	CPUUsageP50MC         int64   `json:"cpu_usage_p50_mc"`
	CPUUsageMeanMC        int64   `json:"cpu_usage_mean_mc"`
	CPUAdaptiveMarginBP   int32   `json:"cpu_adaptive_margin_bp"`
	CPUTrendSlope         float64 `json:"cpu_trend_slope"`
	MemCostPctKiB         int64   `json:"mem_cost_pct_kib"`
	MemPerfPctKiB         int64   `json:"mem_perf_pct_kib"`
	MemUsageP95KiB        int64   `json:"mem_usage_p95_kib"`
	MemUsageP50KiB        int64   `json:"mem_usage_p50_kib"`
	MemUsageMeanKiB       int64   `json:"mem_usage_mean_kib"`
	MemAdaptiveMarginBP   int32   `json:"mem_adaptive_margin_bp"`
	MemTrendSlope         float64 `json:"mem_trend_slope"`
	OOMCountSum           int64   `json:"oom_count_sum"`
	OOMBumpApplied        bool    `json:"oom_bump_applied"`
	CPUFloorApplied       bool    `json:"cpu_floor_applied"`
	MemFloorApplied       bool    `json:"mem_floor_applied"`
	IsIdle                bool    `json:"is_idle"`
}

func toContainerExplainOut(r types.ContainerRec, schedule string) containerExplainOut {
	row := toContainerOut(r)
	e := r.Expl
	return containerExplainOut{
		Namespace:             row.Namespace,
		Workload:              row.Workload,
		WorkloadType:          row.WorkloadType,
		ContainerName:         row.ContainerName,
		Term:                  row.Term,
		Engine:                row.Engine,
		Schedule:              schedule,
		RecCPURequestMC:       row.RecCPURequestMC,
		RecCPULimitMC:         row.RecCPULimitMC,
		RecMemRequestKiB:      row.RecMemRequestKiB,
		RecMemLimitKiB:        row.RecMemLimitKiB,
		CurrentCPURequestMC:   row.CurrentCPURequestMC,
		CurrentMemRequestKiB:  row.CurrentMemRequestKiB,
		EstimatedSavingsCents: row.EstimatedSavingsCents,
		Stale:                 row.Stale,
		IdleState:             row.IdleState,
		Category:              row.Category,
		DataDays:              e.DataDays,
		DecayHalfLifeHours:    e.DecayHalfLifeHours,
		CPUCostPctMC:          e.CPUCostPctMC,
		CPUPerfPctMC:          e.CPUPerfPctMC,
		CPUUsageP95MC:         e.CPUUsageP95MC,
		CPUUsageP50MC:         e.CPUUsageP50MC,
		CPUUsageMeanMC:        e.CPUUsageMeanMC,
		CPUAdaptiveMarginBP:   e.CPUAdaptiveMarginBP,
		CPUTrendSlope:         e.CPUTrendSlope,
		MemCostPctKiB:         e.MemCostPctKiB,
		MemPerfPctKiB:         e.MemPerfPctKiB,
		MemUsageP95KiB:        e.MemUsageP95KiB,
		MemUsageP50KiB:        e.MemUsageP50KiB,
		MemUsageMeanKiB:       e.MemUsageMeanKiB,
		MemAdaptiveMarginBP:   e.MemAdaptiveMarginBP,
		MemTrendSlope:         e.MemTrendSlope,
		OOMCountSum:           e.OOMCountSum,
		OOMBumpApplied:        e.OOMBumpApplied,
		CPUFloorApplied:       e.CPUFloorApplied,
		MemFloorApplied:       e.MemFloorApplied,
		IsIdle:                e.IsIdle,
	}
}
