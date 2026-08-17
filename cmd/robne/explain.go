package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/spf13/cobra"
)

const (
	scheduleAllHours      = "all_hours"
	scheduleBusinessHours = "business_hours"
	gpuKindMIG            = "mig"
	gpuKindTimeslicing    = "timeslicing"
)

type explainFlags struct {
	commonFlags
	namespace          string
	workload           string
	workloadType       string
	container          string
	term               string
	engine             string
	schedule           string
	node               string
	gpuModel           string
	pvc                string
	vmName             string
	quotaName          string
	clusterQuotaName   string
	snapshotName       string
	recommendationType string
}

func newExplainCmd() *cobra.Command {
	var f explainFlags
	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Show explanation factors for one recommendation",
		Long: `Re-run the engine from the same --input as recommend and print why one
recommendation is that number.

recommend JSON is the list (what to apply) and does not include explanation
factors. explain is the detail. It does not read a recommend envelope.

One entity type per run. --plugins is exactly one name (omit for container).
Two or more names is an error. YAML plugins: does not select the type.

Identity flags (inapplicable flags are errors):
  container (default): --namespace --workload --container --term --engine
  namespace:           --namespace --term --engine
  node:                --node --term --engine
  gpu MIG:             --container plus --namespace --workload --term
  gpu timeslicing:     --node --gpu-model --term
  pvc:                 --namespace --pvc --term
  vm:                  --namespace --vm-name --term --engine
  quota:               --namespace --quota-name
  cluster_quota:       --cluster-quota-name
  snapshot:            --namespace --snapshot-name

GPU: --container selects MIG; --node without --container selects timeslicing.
--schedule business_hours is container and namespace only.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runExplain(cmd.OutOrStdout(), f)
		},
	}
	bindCommonFlags(cmd, &f.commonFlags, true, false)
	if p := cmd.Flags().Lookup("plugins"); p != nil {
		p.Usage = "exactly one entity type (default: container). YAML plugins: does not select the type"
	}
	cmd.Flags().StringVar(&f.pgURLFile, "pg-url-file", "", "file containing a postgres URL (password off argv)")
	cmd.Flags().StringVar(&f.namespace, "namespace", "", "namespace")
	cmd.Flags().StringVar(&f.workload, "workload", "", "workload name")
	cmd.Flags().StringVar(&f.workloadType, "workload-type", "", "workload type (required when the container match is not unique)")
	cmd.Flags().StringVar(&f.container, "container", "", "container name (container explain; GPU MIG)")
	cmd.Flags().StringVar(&f.term, "term", "", "term (short, medium, long; VM uses short_term / medium_term / long_term)")
	cmd.Flags().StringVar(&f.engine, "engine", "", "engine (cost, performance)")
	cmd.Flags().StringVar(&f.schedule, "schedule", "", "all_hours (default) or business_hours (container and namespace only)")
	cmd.Flags().StringVar(&f.node, "node", "", "node name (node explain; GPU timeslicing)")
	cmd.Flags().StringVar(&f.gpuModel, "gpu-model", "", "GPU model name")
	cmd.Flags().StringVar(&f.pvc, "pvc", "", "PVC name")
	cmd.Flags().StringVar(&f.vmName, "vm-name", "", "VM name")
	cmd.Flags().StringVar(&f.quotaName, "quota-name", "", "namespace ResourceQuota name")
	cmd.Flags().StringVar(&f.clusterQuotaName, "cluster-quota-name", "", "ClusterResourceQuota name")
	cmd.Flags().StringVar(&f.snapshotName, "snapshot-name", "", "VolumeSnapshot name")
	cmd.Flags().StringVar(&f.recommendationType, "recommendation-type", "", "quota / cluster_quota recommendation type when the match is not unique")
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
	plugin, err := resolveExplainPlugin(f.plugins)
	if err != nil {
		return err
	}
	if err := validatePlugins(cfg, plugin); err != nil {
		return err
	}
	gpuKind, err := gpuExplainKind(plugin, f)
	if err != nil {
		return err
	}
	if err := validateExplainIdentity(plugin, gpuKind, f); err != nil {
		return err
	}
	schedule, err := resolveExplainSchedule(f.schedule)
	if err != nil {
		return err
	}
	if schedule == scheduleBusinessHours {
		if plugin != "container" && plugin != "namespace" {
			return fmt.Errorf("--schedule business_hours is container and namespace only (got %s); node/GPU/VM BH is #483", plugin)
		}
		if !businessHoursEnabled(cfg) {
			return fmt.Errorf("--schedule business_hours requires YAML business_hours.enabled")
		}
	}
	cf := f.commonFlags
	cf.plugins = plugin
	cf.format = "json"
	result, err := executeRecommend(cf)
	if err != nil {
		return err
	}
	out, err := explainJSON(result, plugin, gpuKind, schedule, f)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func resolveExplainPlugin(flag string) (string, error) {
	s := strings.TrimSpace(flag)
	if s == "" {
		return "container", nil
	}
	var list []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		list = append(list, p)
	}
	if len(list) == 0 {
		return "container", nil
	}
	if len(list) > 1 {
		return "", fmt.Errorf("explain one entity type at a time (got %s)", strings.Join(list, ", "))
	}
	if _, ok := enabledPlugins[list[0]]; !ok {
		return "", fmt.Errorf("unknown plugin %q", list[0])
	}
	return list[0], nil
}

func gpuExplainKind(plugin string, f explainFlags) (string, error) {
	if plugin != "gpu" {
		return "", nil
	}
	hasContainer := strings.TrimSpace(f.container) != ""
	hasNode := strings.TrimSpace(f.node) != ""
	switch {
	case hasContainer && hasNode:
		return "", fmt.Errorf("gpu explain: pass --container (MIG) or --node (timeslicing), not both")
	case hasContainer:
		return gpuKindMIG, nil
	case hasNode:
		return gpuKindTimeslicing, nil
	default:
		return "", fmt.Errorf("gpu explain needs --container (MIG) or --node (timeslicing)")
	}
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

func validateExplainIdentity(plugin, gpuKind string, f explainFlags) error {
	required, allowed, err := explainIdentitySets(plugin, gpuKind)
	if err != nil {
		return err
	}
	set := explainFlagsSet(f)
	var missing []string
	for _, name := range required {
		if strings.TrimSpace(explainFlagValue(f, name)) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s explain requires %s", plugin, strings.Join(missing, ", "))
	}
	var extra []string
	for _, name := range set {
		if !allowed[name] {
			extra = append(extra, name)
		}
	}
	if len(extra) > 0 {
		return fmt.Errorf("%s explain does not take %s (explain one entity type at a time)", plugin, strings.Join(extra, ", "))
	}
	return nil
}

func explainIdentitySets(plugin, gpuKind string) (required []string, allowed map[string]bool, err error) {
	allow := func(names ...string) map[string]bool {
		m := make(map[string]bool, len(names))
		for _, n := range names {
			m[n] = true
		}
		return m
	}
	switch plugin {
	case "container":
		return []string{"--namespace", "--workload", "--container", "--term", "--engine"},
			allow("--namespace", "--workload", "--workload-type", "--container", "--term", "--engine"), nil
	case "namespace":
		return []string{"--namespace", "--term", "--engine"},
			allow("--namespace", "--term", "--engine"), nil
	case "node":
		return []string{"--node", "--term", "--engine"},
			allow("--node", "--term", "--engine"), nil
	case "gpu":
		switch gpuKind {
		case gpuKindMIG:
			return []string{"--namespace", "--workload", "--container", "--term"},
				allow("--namespace", "--workload", "--container", "--term", "--gpu-model"), nil
		case gpuKindTimeslicing:
			return []string{"--node", "--gpu-model", "--term"},
				allow("--node", "--gpu-model", "--term"), nil
		default:
			return nil, nil, fmt.Errorf("gpu explain needs --container (MIG) or --node (timeslicing)")
		}
	case "pvc":
		return []string{"--namespace", "--pvc", "--term"},
			allow("--namespace", "--pvc", "--term"), nil
	case "vm":
		return []string{"--namespace", "--vm-name", "--term", "--engine"},
			allow("--namespace", "--vm-name", "--term", "--engine"), nil
	case "quota":
		return []string{"--namespace", "--quota-name"},
			allow("--namespace", "--quota-name", "--recommendation-type"), nil
	case "cluster_quota":
		return []string{"--cluster-quota-name"},
			allow("--cluster-quota-name", "--recommendation-type"), nil
	case "snapshot":
		return []string{"--namespace", "--snapshot-name"},
			allow("--namespace", "--snapshot-name"), nil
	default:
		return nil, nil, fmt.Errorf("unknown plugin %q", plugin)
	}
}

func explainFlagValue(f explainFlags, name string) string {
	switch name {
	case "--namespace":
		return f.namespace
	case "--workload":
		return f.workload
	case "--workload-type":
		return f.workloadType
	case "--container":
		return f.container
	case "--term":
		return f.term
	case "--engine":
		return f.engine
	case "--node":
		return f.node
	case "--gpu-model":
		return f.gpuModel
	case "--pvc":
		return f.pvc
	case "--vm-name":
		return f.vmName
	case "--quota-name":
		return f.quotaName
	case "--cluster-quota-name":
		return f.clusterQuotaName
	case "--snapshot-name":
		return f.snapshotName
	case "--recommendation-type":
		return f.recommendationType
	default:
		return ""
	}
}

func explainFlagsSet(f explainFlags) []string {
	names := []string{
		"--namespace", "--workload", "--workload-type", "--container", "--term", "--engine",
		"--node", "--gpu-model", "--pvc", "--vm-name", "--quota-name",
		"--cluster-quota-name", "--snapshot-name", "--recommendation-type",
	}
	var set []string
	for _, name := range names {
		if strings.TrimSpace(explainFlagValue(f, name)) != "" {
			set = append(set, name)
		}
	}
	return set
}

func explainJSON(result recommendResult, plugin, gpuKind, schedule string, f explainFlags) (any, error) {
	switch plugin {
	case "container":
		recs := result.Recs
		if schedule == scheduleBusinessHours {
			recs = result.BHRecs
		}
		rec, err := selectContainerRec(recs, f)
		if err != nil {
			return nil, err
		}
		return toContainerExplainOut(rec, schedule), nil
	case "namespace":
		recs := result.NamespaceRecs
		if schedule == scheduleBusinessHours {
			recs = result.BHNamespaceRecs
		}
		rec, err := selectNamespaceRec(recs, f)
		if err != nil {
			return nil, err
		}
		return toNamespaceExplainOut(rec, schedule), nil
	case "node":
		rec, err := selectNodeRec(result.NodeRecs, f)
		if err != nil {
			return nil, err
		}
		return toNodeExplainOut(rec), nil
	case "gpu":
		if gpuKind == gpuKindTimeslicing {
			rec, err := selectGPUTimeslicingRec(result.GPUTimeslicing, f)
			if err != nil {
				return nil, err
			}
			return toGPUTimeslicingExplainOut(rec), nil
		}
		rec, err := selectGPURec(result.GPURecs, f)
		if err != nil {
			return nil, err
		}
		return toGPUExplainOut(rec), nil
	case "pvc":
		rec, err := selectPVCRec(result.PVCRecs, f)
		if err != nil {
			return nil, err
		}
		return toPVCExplainOut(rec), nil
	case "vm":
		rec, err := selectVMRec(result.VMRecs, f)
		if err != nil {
			return nil, err
		}
		return toVMExplainOut(rec), nil
	case "quota":
		rec, err := selectQuotaRec(result.QuotaRecs, f)
		if err != nil {
			return nil, err
		}
		return toQuotaExplainOut(rec), nil
	case "cluster_quota":
		rec, err := selectClusterQuotaRec(result.ClusterQuotaRecs, f)
		if err != nil {
			return nil, err
		}
		return toClusterQuotaExplainOut(rec), nil
	case "snapshot":
		rec, err := selectSnapshotRec(result.SnapshotRecs, f)
		if err != nil {
			return nil, err
		}
		return toSnapshotExplainOut(rec), nil
	default:
		return nil, fmt.Errorf("unknown plugin %q", plugin)
	}
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

func gpuExplFromRec(rec gpu.GPURec) types.GPUExplanationFactors {
	return types.GPUExplanationFactors{
		SMActiveAvgBP:      int32(rec.SMActiveAvg * float32(gpu.BasisPointsScale)),
		TensorActiveAvgBP:  int32(rec.TensorPipeActiveAvg * float32(gpu.BasisPointsScale)),
		DRAMActiveAvgBP:    int32(rec.DRAMActiveAvg * float32(gpu.BasisPointsScale)),
		FBUsageMaxMiB:      int32(rec.FBUsageMaxMiB),
		FBP98MiB:           rec.FBP98MiB,
		RecommendedProfile: rec.RecommendedGPUProfile,
		CurrentProfile:     rec.CurrentGPUProfile,
		HasProfilingData:   rec.HasProfilingData,
		MemoryBound:        rec.MemoryBoundDetected,
	}
}
