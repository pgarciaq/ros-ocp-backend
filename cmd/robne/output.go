package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
	"github.com/redhatinsights/ros-ocp-backend/librobne/namespace"
	"github.com/redhatinsights/ros-ocp-backend/librobne/node"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pvc"
	"github.com/redhatinsights/ros-ocp-backend/librobne/quota"
	"github.com/redhatinsights/ros-ocp-backend/librobne/snapshot"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/redhatinsights/ros-ocp-backend/librobne/vm"
)

const recommendJSONVersion = 1
const recommendJSONVersionWithNamespace = 2
const recommendJSONVersionWithNode = 3
const recommendJSONVersionWithGPU = 4
const recommendJSONVersionWithPVC = 5
const recommendJSONVersionWithVM = 6
const recommendJSONVersionWithQuota = 7
const recommendJSONVersionWithClusterQuota = 8
const recommendJSONVersionWithSnapshot = 9
const recommendJSONVersionWithBusinessHours = 10

var stdoutEntityPlugins = []string{"container", "namespace", "node", "gpu", "pvc", "vm", "quota", "cluster_quota", "snapshot"}

// recommendResult is the CLI-owned stdout payload (engine recs plus run metadata).
type recommendResult struct {
	Recs                []types.ContainerRec
	BHRecs              []types.ContainerRec
	NamespaceRecs       []namespace.NamespaceRec
	BHNamespaceRecs     []namespace.NamespaceRec
	NodeRecs            []node.Rec
	GPURecs             []gpuRecRow
	GPUTimeslicing      []gpu.TimeslicingRec
	PVCRecs             []pvc.PVCRec
	VMRecs              []vm.VMRecommendation
	QuotaRecs           []quota.QuotaRec
	ClusterQuotaRecs    []quota.ClusterQuotaRec
	SnapshotRecs        []snapshot.SnapshotRec
	Digests             []types.KeyedDigest
	BHDigests           []types.KeyedDigest
	NamespaceDigests    map[namespace.NamespaceKey][]types.DigestRow
	BHNamespaceDigests  map[namespace.NamespaceKey][]types.DigestRow
	NodeDigests         []node.DigestRow
	GPUDigests          map[gpu.GPUContainerKey][]gpu.GPUDigestRow
	PVCDigests          map[pvc.PVCKey][]pvc.PVCDigestRow
	VMDigests           []vm.DailyVMDigest
	QuotaDigests        []quota.NamespaceQuotaSnapshot
	ClusterQuotaDigests []quota.ClusterQuotaSnapshot
	ClusterID           string
	OrgID               string
	Now                 time.Time
	SkippedRows         int
	ValidTerms          []string
	GPUNodeLastSeen     map[string]time.Time
	plugins             []string
	businessHours       bool
}

// recommendJSON is the versioned --format json envelope. Phase 3 diff consumes this.
type recommendJSON struct {
	Version                               int                  `json:"version"`
	ClusterID                             string               `json:"cluster_id"`
	Now                                   string               `json:"now"`
	SkippedRows                           int                  `json:"skipped_rows"`
	Recommendations                       []containerOut       `json:"recommendations"`
	BusinessHoursRecommendations          *[]containerOut      `json:"business_hours_recommendations,omitempty"`
	NamespaceRecommendations              *[]namespaceOut      `json:"namespace_recommendations,omitempty"`
	BusinessHoursNamespaceRecommendations *[]namespaceOut      `json:"business_hours_namespace_recommendations,omitempty"`
	NodeRecommendations                   *[]nodeOut           `json:"node_recommendations,omitempty"`
	GPURecommendations                    *[]gpuOut            `json:"gpu_recommendations,omitempty"`
	GPUTimeslicingRecommendations         *[]gpuTimeslicingOut `json:"gpu_timeslicing_recommendations,omitempty"`
	PVCRecommendations                    *[]pvcOut            `json:"pvc_recommendations,omitempty"`
	VMRecommendations                     *[]vmOut             `json:"vm_recommendations,omitempty"`
	QuotaRecommendations                  *[]quotaOut          `json:"quota_recommendations,omitempty"`
	ClusterQuotaRecommendations           *[]clusterQuotaOut   `json:"cluster_quota_recommendations,omitempty"`
	SnapshotRecommendations               *[]snapshotOut       `json:"snapshot_recommendations,omitempty"`
}

// containerOut is the snake_case row DTO. Fields match containerOutCSVHeader.
// Do not add json tags on types.ContainerRec.
type containerOut struct {
	Namespace             string `json:"namespace"`
	Workload              string `json:"workload"`
	WorkloadType          string `json:"workload_type"`
	ContainerName         string `json:"container_name"`
	Term                  string `json:"term"`
	Engine                string `json:"engine"`
	RecCPURequestMC       int64  `json:"rec_cpu_request_mc"`
	RecCPULimitMC         int64  `json:"rec_cpu_limit_mc"`
	RecMemRequestKiB      int64  `json:"rec_mem_request_kib"`
	RecMemLimitKiB        int64  `json:"rec_mem_limit_kib"`
	CurrentCPURequestMC   int64  `json:"current_cpu_request_mc"`
	CurrentMemRequestKiB  int64  `json:"current_mem_request_kib"`
	EstimatedSavingsCents *int64 `json:"estimated_savings_cents"`
	Stale                 bool   `json:"stale"`
	IdleState             string `json:"idle_state"`
	Category              string `json:"category"`
}

// namespaceOut is the snake_case namespace row DTO. Do not add json tags on NamespaceRec.
type namespaceOut struct {
	Namespace             string `json:"namespace"`
	Term                  string `json:"term"`
	Engine                string `json:"engine"`
	RecCPURequestMC       int64  `json:"rec_cpu_request_mc"`
	RecCPULimitMC         int64  `json:"rec_cpu_limit_mc"`
	RecMemRequestKiB      int64  `json:"rec_mem_request_kib"`
	RecMemLimitKiB        int64  `json:"rec_mem_limit_kib"`
	CurrentCPURequestMC   int64  `json:"current_cpu_request_mc"`
	CurrentMemRequestKiB  int64  `json:"current_mem_request_kib"`
	EstimatedSavingsCents *int64 `json:"estimated_savings_cents"`
	Stale                 bool   `json:"stale"`
	Category              string `json:"category"`
}

var namespaceOutCSVHeader = []string{
	"namespace", "term", "engine",
	"rec_cpu_request_mc", "rec_cpu_limit_mc", "rec_mem_request_kib", "rec_mem_limit_kib",
	"current_cpu_request_mc", "current_mem_request_kib", "estimated_savings_cents",
	"stale", "category",
}

type nodeOut struct {
	Node                  string `json:"node"`
	Term                  string `json:"term"`
	Engine                string `json:"engine"`
	Category              string `json:"category"`
	IdleState             string `json:"idle_state"`
	RecommendedCPUMC      int64  `json:"recommended_cpu_mc"`
	RecommendedMemKiB     int64  `json:"recommended_mem_kib"`
	CurrentCPUMC          int64  `json:"current_cpu_mc"`
	CurrentMemKiB         int64  `json:"current_mem_kib"`
	NodeCountReduction    int    `json:"node_count_reduction"`
	EstimatedSavingsCents *int64 `json:"estimated_savings_cents"`
	InstanceType          string `json:"instance_type"`
	SuggestedInstanceType string `json:"suggested_instance_type"`
}

var nodeOutCSVHeader = []string{
	"node", "term", "engine", "category", "idle_state",
	"recommended_cpu_mc", "recommended_mem_kib", "current_cpu_mc", "current_mem_kib",
	"node_count_reduction", "estimated_savings_cents", "instance_type", "suggested_instance_type",
}

type gpuOut struct {
	Namespace                string `json:"namespace"`
	Workload                 string `json:"workload"`
	ContainerName            string `json:"container_name"`
	Term                     string `json:"term"`
	GPUModelName             string `json:"gpu_model_name"`
	CurrentGPUProfile        string `json:"current_gpu_profile"`
	RecommendedGPUProfile    string `json:"recommended_gpu_profile"`
	Classification           string `json:"classification"`
	GPUCount                 int    `json:"gpu_count"`
	EstimatedGPUSavingsCents *int64 `json:"estimated_gpu_savings_cents"`
}

var gpuOutCSVHeader = []string{
	"namespace", "workload", "container_name", "term",
	"gpu_model_name", "current_gpu_profile", "recommended_gpu_profile",
	"classification", "gpu_count", "estimated_gpu_savings_cents",
}

type gpuTimeslicingOut struct {
	Node                string `json:"node"`
	GPUModel            string `json:"gpu_model"`
	Term                string `json:"term"`
	RecommendedReplicas int    `json:"recommended_replicas"`
}

type pvcOut struct {
	Namespace             string `json:"namespace"`
	PVC                   string `json:"pvc"`
	Term                  string `json:"term"`
	RecommendationType    string `json:"recommendation_type"`
	CapacityBytes         int64  `json:"capacity_bytes"`
	RequestBytes          int64  `json:"request_bytes"`
	UsageBytesMax         int64  `json:"usage_bytes_max"`
	RecommendedBytes      *int64 `json:"recommended_bytes"`
	DaysToFull            *int   `json:"days_to_full"`
	EstimatedSavingsCents *int64 `json:"estimated_savings_cents"`
	StorageClass          string `json:"storage_class"`
	VMName                string `json:"vm_name"`
}

var pvcOutCSVHeader = []string{
	"namespace", "pvc", "term", "recommendation_type",
	"capacity_bytes", "request_bytes", "usage_bytes_max",
	"recommended_bytes", "days_to_full", "estimated_savings_cents",
	"storage_class", "vm_name",
}

type vmOut struct {
	Namespace                 string `json:"namespace"`
	VMName                    string `json:"vm_name"`
	Term                      string `json:"term"`
	Engine                    string `json:"engine"`
	Category                  string `json:"category"`
	CurrentVCPU               int32  `json:"current_vcpu"`
	CurrentMemoryGiB          int32  `json:"current_memory_gib"`
	RecommendedVCPU           int32  `json:"recommended_vcpu"`
	RecommendedMemoryGiB      int32  `json:"recommended_memory_gib"`
	RecommendedInstanceType   string `json:"recommended_instance_type"`
	GuestOS                   string `json:"guest_os"`
	EstimatedSavingsCents     *int64 `json:"estimated_savings_cents"`
	RecommendedTimeSliceCount int32  `json:"recommended_time_slice_count"`
}

var vmOutCSVHeader = []string{
	"namespace", "vm_name", "term", "engine", "category",
	"current_vcpu", "current_memory_gib", "recommended_vcpu", "recommended_memory_gib",
	"recommended_instance_type", "guest_os", "estimated_savings_cents",
	"recommended_time_slice_count",
}

// quotaOut is the snake_case quota row DTO. Do not add json tags on quota.QuotaRec.
type quotaOut struct {
	Namespace                      string `json:"namespace"`
	QuotaName                      string `json:"quota_name"`
	RecommendationType             string `json:"recommendation_type"`
	RiskLevel                      string `json:"risk_level"`
	CPURequestHardMC               int64  `json:"cpu_request_hard_mc"`
	CPULimitHardMC                 int64  `json:"cpu_limit_hard_mc"`
	MemoryRequestHardBytes         int64  `json:"memory_request_hard_bytes"`
	MemoryLimitHardBytes           int64  `json:"memory_limit_hard_bytes"`
	CPURequestRecommendedMC        int64  `json:"cpu_request_recommended_mc"`
	CPULimitRecommendedMC          int64  `json:"cpu_limit_recommended_mc"`
	MemoryRequestRecommendedBytes  int64  `json:"memory_request_recommended_bytes"`
	MemoryLimitRecommendedBytes    int64  `json:"memory_limit_recommended_bytes"`
	StorageRequestHardBytes        int64  `json:"storage_request_hard_bytes"`
	StorageRequestRecommendedBytes int64  `json:"storage_request_recommended_bytes"`
	PodsHard                       int64  `json:"pods_hard"`
	PodsRecommended                int64  `json:"pods_recommended"`
	EstimatedSavingsCents          *int64 `json:"estimated_savings_cents"`
}

var quotaOutCSVHeader = []string{
	"namespace", "quota_name", "recommendation_type", "risk_level",
	"cpu_request_hard_mc", "cpu_limit_hard_mc",
	"memory_request_hard_bytes", "memory_limit_hard_bytes",
	"cpu_request_recommended_mc", "cpu_limit_recommended_mc",
	"memory_request_recommended_bytes", "memory_limit_recommended_bytes",
	"storage_request_hard_bytes", "storage_request_recommended_bytes",
	"pods_hard", "pods_recommended", "estimated_savings_cents",
}

// clusterQuotaOut is the snake_case CRQ row DTO. Do not add json tags on quota.ClusterQuotaRec.
type clusterQuotaOut struct {
	ClusterQuotaName               string `json:"cluster_quota_name"`
	Namespaces                     string `json:"namespaces"`
	RecommendationType             string `json:"recommendation_type"`
	RiskLevel                      string `json:"risk_level"`
	CPURequestHardMC               int64  `json:"cpu_request_hard_mc"`
	CPULimitHardMC                 int64  `json:"cpu_limit_hard_mc"`
	MemoryRequestHardBytes         int64  `json:"memory_request_hard_bytes"`
	MemoryLimitHardBytes           int64  `json:"memory_limit_hard_bytes"`
	CPURequestRecommendedMC        int64  `json:"cpu_request_recommended_mc"`
	CPULimitRecommendedMC          int64  `json:"cpu_limit_recommended_mc"`
	MemoryRequestRecommendedBytes  int64  `json:"memory_request_recommended_bytes"`
	MemoryLimitRecommendedBytes    int64  `json:"memory_limit_recommended_bytes"`
	StorageRequestHardBytes        int64  `json:"storage_request_hard_bytes"`
	StorageRequestRecommendedBytes int64  `json:"storage_request_recommended_bytes"`
	PodsHard                       int64  `json:"pods_hard"`
	PodsRecommended                int64  `json:"pods_recommended"`
	EstimatedSavingsCents          *int64 `json:"estimated_savings_cents"`
}

var clusterQuotaOutCSVHeader = []string{
	"cluster_quota_name", "namespaces", "recommendation_type", "risk_level",
	"cpu_request_hard_mc", "cpu_limit_hard_mc",
	"memory_request_hard_bytes", "memory_limit_hard_bytes",
	"cpu_request_recommended_mc", "cpu_limit_recommended_mc",
	"memory_request_recommended_bytes", "memory_limit_recommended_bytes",
	"storage_request_hard_bytes", "storage_request_recommended_bytes",
	"pods_hard", "pods_recommended", "estimated_savings_cents",
}

// snapshotOut is the snake_case snapshot row DTO. Do not add json tags on snapshot.SnapshotRec.
type snapshotOut struct {
	Namespace           string  `json:"namespace"`
	SnapshotName        string  `json:"snapshot_name"`
	SourcePVCName       string  `json:"source_pvc_name"`
	VolumeSnapshotClass string  `json:"volume_snapshot_class"`
	StorageClass        string  `json:"storage_class"`
	CreationTimestamp   string  `json:"creation_timestamp"`
	RestoreSizeBytes    int64   `json:"restore_size_bytes"`
	AgeDays             int     `json:"age_days"`
	SourcePVCExists     bool    `json:"source_pvc_exists"`
	RestoredPVCCount    int     `json:"restored_pvc_count"`
	ManagedBy           string  `json:"managed_by"`
	RecommendationType  string  `json:"recommendation_type"`
	EstimatedCostCents  *int64  `json:"estimated_cost_cents"`
	NotificationCodes   []int16 `json:"notification_codes"`
}

var snapshotOutCSVHeader = []string{
	"namespace", "snapshot_name", "source_pvc_name", "volume_snapshot_class", "storage_class",
	"creation_timestamp", "restore_size_bytes", "age_days", "source_pvc_exists", "restored_pvc_count",
	"managed_by", "recommendation_type", "estimated_cost_cents", "notification_codes",
}

// gpuRecRow pairs container identity with a GPURec. GPURec has no namespace fields.
type gpuRecRow struct {
	Namespace     string
	Workload      string
	ContainerName string
	NodeName      string
	Rec           gpu.GPURec
}

func writeRecs(w io.Writer, result recommendResult, format string) error {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "json"
	}
	if result.businessHours && (format == "csv" || format == "table") {
		return fmt.Errorf("--format %s is one entity and one schedule stream; use json when business_hours.enabled is true", format)
	}
	if stdoutEntityCount(result.plugins) > 1 && (format == "csv" || format == "table") {
		return fmt.Errorf("--format %s is one entity per stream; use json when --plugins includes more than one of container, namespace, node, gpu, pvc, vm, quota, cluster_quota, snapshot", format)
	}
	switch format {
	case "json":
		return writeJSON(w, result)
	case "csv":
		switch stdoutCSVEntity(result.plugins) {
		case "namespace":
			return writeNamespaceCSV(w, result.NamespaceRecs)
		case "node":
			return writeNodeCSV(w, result.NodeRecs)
		case "gpu":
			return writeGPUCSV(w, result.GPURecs)
		case "pvc":
			return writePVCCSV(w, result.PVCRecs)
		case "vm":
			return writeVMCSV(w, result.VMRecs)
		case "quota":
			return writeQuotaCSV(w, result.QuotaRecs)
		case "cluster_quota":
			return writeClusterQuotaCSV(w, result.ClusterQuotaRecs)
		case "snapshot":
			return writeSnapshotCSV(w, result.SnapshotRecs)
		default:
			return writeCSV(w, result.Recs)
		}
	case "table":
		switch stdoutCSVEntity(result.plugins) {
		case "namespace":
			return writeNamespaceTable(w, result.NamespaceRecs)
		case "node":
			return writeNodeTable(w, result.NodeRecs)
		case "gpu":
			return writeGPUTable(w, result.GPURecs)
		case "pvc":
			return writePVCTable(w, result.PVCRecs)
		case "vm":
			return writeVMTable(w, result.VMRecs)
		case "quota":
			return writeQuotaTable(w, result.QuotaRecs)
		case "cluster_quota":
			return writeClusterQuotaTable(w, result.ClusterQuotaRecs)
		case "snapshot":
			return writeSnapshotTable(w, result.SnapshotRecs)
		default:
			return writeTable(w, result.Recs)
		}
	default:
		return fmt.Errorf("unknown --format %q (json, csv, table)", format)
	}
}

func stdoutEntityCount(plugins []string) int {
	n := 0
	for _, p := range stdoutEntityPlugins {
		if pluginEnabled(plugins, p) {
			n++
		}
	}
	return n
}

func stdoutCSVEntity(plugins []string) string {
	for _, p := range stdoutEntityPlugins {
		if pluginEnabled(plugins, p) {
			return p
		}
	}
	return "container"
}

func envelopeVersion(plugins []string, bh bool) int {
	v := recommendJSONVersion
	if pluginEnabled(plugins, "namespace") {
		v = recommendJSONVersionWithNamespace
	}
	if pluginEnabled(plugins, "node") {
		v = recommendJSONVersionWithNode
	}
	if pluginEnabled(plugins, "gpu") {
		v = recommendJSONVersionWithGPU
	}
	if pluginEnabled(plugins, "pvc") {
		v = recommendJSONVersionWithPVC
	}
	if pluginEnabled(plugins, "vm") {
		v = recommendJSONVersionWithVM
	}
	if pluginEnabled(plugins, "quota") {
		v = recommendJSONVersionWithQuota
	}
	if pluginEnabled(plugins, "cluster_quota") {
		v = recommendJSONVersionWithClusterQuota
	}
	if pluginEnabled(plugins, "snapshot") {
		v = recommendJSONVersionWithSnapshot
	}
	if bh {
		return recommendJSONVersionWithBusinessHours
	}
	return v
}

func writeJSON(w io.Writer, result recommendResult) error {
	env := recommendJSON{
		Version:         envelopeVersion(result.plugins, result.businessHours),
		ClusterID:       result.ClusterID,
		Now:             result.Now.UTC().Format(time.RFC3339),
		SkippedRows:     result.SkippedRows,
		Recommendations: make([]containerOut, len(result.Recs)),
	}
	for i, rec := range result.Recs {
		env.Recommendations[i] = toContainerOut(rec)
	}
	if pluginEnabled(result.plugins, "namespace") {
		ns := make([]namespaceOut, len(result.NamespaceRecs))
		for i, rec := range result.NamespaceRecs {
			ns[i] = toNamespaceOut(rec)
		}
		env.NamespaceRecommendations = &ns
	}
	if result.businessHours {
		c := make([]containerOut, len(result.BHRecs))
		for i, rec := range result.BHRecs {
			c[i] = toContainerOut(rec)
		}
		env.BusinessHoursRecommendations = &c
		ns := make([]namespaceOut, len(result.BHNamespaceRecs))
		for i, rec := range result.BHNamespaceRecs {
			ns[i] = toNamespaceOut(rec)
		}
		env.BusinessHoursNamespaceRecommendations = &ns
	}
	if pluginEnabled(result.plugins, "node") {
		rows := make([]nodeOut, len(result.NodeRecs))
		for i, rec := range result.NodeRecs {
			rows[i] = toNodeOut(rec)
		}
		env.NodeRecommendations = &rows
	}
	if pluginEnabled(result.plugins, "gpu") {
		rows := make([]gpuOut, len(result.GPURecs))
		for i, rec := range result.GPURecs {
			rows[i] = toGPUOut(rec)
		}
		env.GPURecommendations = &rows
		ts := make([]gpuTimeslicingOut, len(result.GPUTimeslicing))
		for i, rec := range result.GPUTimeslicing {
			ts[i] = toGPUTimeslicingOut(rec)
		}
		env.GPUTimeslicingRecommendations = &ts
	}
	if pluginEnabled(result.plugins, "pvc") {
		rows := make([]pvcOut, len(result.PVCRecs))
		for i, rec := range result.PVCRecs {
			rows[i] = toPVCOut(rec)
		}
		env.PVCRecommendations = &rows
	}
	if pluginEnabled(result.plugins, "vm") {
		rows := make([]vmOut, len(result.VMRecs))
		for i, rec := range result.VMRecs {
			rows[i] = toVMOut(rec)
		}
		env.VMRecommendations = &rows
	}
	if pluginEnabled(result.plugins, "quota") {
		rows := make([]quotaOut, len(result.QuotaRecs))
		for i, rec := range result.QuotaRecs {
			rows[i] = toQuotaOut(rec)
		}
		env.QuotaRecommendations = &rows
	}
	if pluginEnabled(result.plugins, "cluster_quota") {
		rows := make([]clusterQuotaOut, len(result.ClusterQuotaRecs))
		for i, rec := range result.ClusterQuotaRecs {
			rows[i] = toClusterQuotaOut(rec)
		}
		env.ClusterQuotaRecommendations = &rows
	}
	if pluginEnabled(result.plugins, "snapshot") {
		rows := make([]snapshotOut, len(result.SnapshotRecs))
		for i, rec := range result.SnapshotRecs {
			rows[i] = toSnapshotOut(rec)
		}
		env.SnapshotRecommendations = &rows
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

var containerOutCSVHeader = []string{
	"namespace", "workload", "workload_type", "container_name", "term", "engine",
	"rec_cpu_request_mc", "rec_cpu_limit_mc", "rec_mem_request_kib", "rec_mem_limit_kib",
	"current_cpu_request_mc", "current_mem_request_kib", "estimated_savings_cents",
	"stale", "idle_state", "category",
}

func toContainerOut(r types.ContainerRec) containerOut {
	return containerOut{
		Namespace:             r.Namespace,
		Workload:              r.Workload,
		WorkloadType:          r.WorkloadType,
		ContainerName:         r.ContainerName,
		Term:                  r.Term,
		Engine:                r.Engine,
		RecCPURequestMC:       r.RecCPURequestMC,
		RecCPULimitMC:         r.RecCPULimitMC,
		RecMemRequestKiB:      r.RecMemRequestKiB,
		RecMemLimitKiB:        r.RecMemLimitKiB,
		CurrentCPURequestMC:   r.CurrentCPURequestMC,
		CurrentMemRequestKiB:  r.CurrentMemRequestKiB,
		EstimatedSavingsCents: r.EstimatedSavingsCents,
		Stale:                 r.Stale,
		IdleState:             string(r.IdleState),
		Category:              r.Category,
	}
}

func toNamespaceOut(r namespace.NamespaceRec) namespaceOut {
	return namespaceOut{
		Namespace:             r.Namespace,
		Term:                  r.Term,
		Engine:                r.Engine,
		RecCPURequestMC:       r.RecCPURequestMC,
		RecCPULimitMC:         r.RecCPULimitMC,
		RecMemRequestKiB:      r.RecMemRequestKiB,
		RecMemLimitKiB:        r.RecMemLimitKiB,
		CurrentCPURequestMC:   r.CurrentCPURequestMC,
		CurrentMemRequestKiB:  r.CurrentMemRequestKiB,
		EstimatedSavingsCents: r.EstimatedSavingsCents,
		Stale:                 r.Stale,
		Category:              r.Category,
	}
}

func toNodeOut(r node.Rec) nodeOut {
	var savings *int64
	if r.EstimatedMonthlySavingsCents != 0 {
		v := r.EstimatedMonthlySavingsCents
		savings = &v
	}
	return nodeOut{
		Node:                  r.Node,
		Term:                  r.Term,
		Engine:                r.Engine,
		Category:              r.Category,
		IdleState:             string(r.IdleState),
		RecommendedCPUMC:      r.RecommendedCPUMC,
		RecommendedMemKiB:     r.RecommendedMemKiB,
		CurrentCPUMC:          r.CurrentCPUMC,
		CurrentMemKiB:         r.CurrentMemKiB,
		NodeCountReduction:    r.NodeCountReduction,
		EstimatedSavingsCents: savings,
		InstanceType:          r.InstanceType,
		SuggestedInstanceType: r.SuggestedInstanceType,
	}
}

func toGPUOut(r gpuRecRow) gpuOut {
	return gpuOut{
		Namespace:                r.Namespace,
		Workload:                 r.Workload,
		ContainerName:            r.ContainerName,
		Term:                     r.Rec.Term,
		GPUModelName:             r.Rec.GPUModelName,
		CurrentGPUProfile:        r.Rec.CurrentGPUProfile,
		RecommendedGPUProfile:    r.Rec.RecommendedGPUProfile,
		Classification:           string(r.Rec.Classification),
		GPUCount:                 r.Rec.GPUCount,
		EstimatedGPUSavingsCents: r.Rec.EstimatedGPUSavingsCents,
	}
}

func toGPUTimeslicingOut(r gpu.TimeslicingRec) gpuTimeslicingOut {
	return gpuTimeslicingOut{
		Node:                r.NodeName,
		GPUModel:            r.GPUModel,
		Term:                r.Term,
		RecommendedReplicas: r.RecommendedReplicas,
	}
}

func toPVCOut(r pvc.PVCRec) pvcOut {
	var savings *int64
	if r.EstimatedMonthlySavingsCents != 0 {
		v := r.EstimatedMonthlySavingsCents
		savings = &v
	}
	return pvcOut{
		Namespace:             r.Namespace,
		PVC:                   r.PVC,
		Term:                  r.Term,
		RecommendationType:    r.RecommendationType,
		CapacityBytes:         r.CapacityBytes,
		RequestBytes:          r.RequestBytes,
		UsageBytesMax:         r.UsageBytesMax,
		RecommendedBytes:      r.RecommendedBytes,
		DaysToFull:            r.DaysToFull,
		EstimatedSavingsCents: savings,
		StorageClass:          r.StorageClass,
		VMName:                r.VMName,
	}
}

func toVMOut(r vm.VMRecommendation) vmOut {
	instance := ""
	if r.RecommendedInstanceType != nil {
		instance = *r.RecommendedInstanceType
	}
	return vmOut{
		Namespace:                 r.Namespace,
		VMName:                    r.VMName,
		Term:                      r.Term,
		Engine:                    r.Engine,
		Category:                  r.Category,
		CurrentVCPU:               r.CurrentVCPU,
		CurrentMemoryGiB:          r.CurrentMemoryGiB,
		RecommendedVCPU:           r.RecommendedVCPU,
		RecommendedMemoryGiB:      r.RecommendedMemoryGiB,
		RecommendedInstanceType:   instance,
		GuestOS:                   r.GuestOS,
		EstimatedSavingsCents:     r.EstimatedSavingsCents,
		RecommendedTimeSliceCount: r.RecommendedTimeSliceCount,
	}
}

func writeCSV(w io.Writer, recs []types.ContainerRec) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(containerOutCSVHeader); err != nil {
		return err
	}
	for _, rec := range recs {
		row := toContainerOut(rec)
		savings := ""
		if row.EstimatedSavingsCents != nil {
			savings = strconv.FormatInt(*row.EstimatedSavingsCents, 10)
		}
		if err := cw.Write([]string{
			row.Namespace, row.Workload, row.WorkloadType, row.ContainerName, row.Term, row.Engine,
			strconv.FormatInt(row.RecCPURequestMC, 10),
			strconv.FormatInt(row.RecCPULimitMC, 10),
			strconv.FormatInt(row.RecMemRequestKiB, 10),
			strconv.FormatInt(row.RecMemLimitKiB, 10),
			strconv.FormatInt(row.CurrentCPURequestMC, 10),
			strconv.FormatInt(row.CurrentMemRequestKiB, 10),
			savings,
			strconv.FormatBool(row.Stale),
			row.IdleState,
			row.Category,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeTable(w io.Writer, recs []types.ContainerRec) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tWORKLOAD\tCONTAINER\tTERM\tENGINE\tCPU_REQ_MC\tMEM_REQ_KIB\tSAVINGS_CENTS\tCATEGORY")
	for _, r := range recs {
		savings := ""
		if r.EstimatedSavingsCents != nil {
			savings = strconv.FormatInt(*r.EstimatedSavingsCents, 10)
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
			r.Namespace, r.Workload, r.ContainerName, r.Term, r.Engine,
			r.RecCPURequestMC, r.RecMemRequestKiB, savings, r.Category); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func writeNamespaceCSV(w io.Writer, recs []namespace.NamespaceRec) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(namespaceOutCSVHeader); err != nil {
		return err
	}
	for _, rec := range recs {
		row := toNamespaceOut(rec)
		savings := ""
		if row.EstimatedSavingsCents != nil {
			savings = strconv.FormatInt(*row.EstimatedSavingsCents, 10)
		}
		if err := cw.Write([]string{
			row.Namespace, row.Term, row.Engine,
			strconv.FormatInt(row.RecCPURequestMC, 10),
			strconv.FormatInt(row.RecCPULimitMC, 10),
			strconv.FormatInt(row.RecMemRequestKiB, 10),
			strconv.FormatInt(row.RecMemLimitKiB, 10),
			strconv.FormatInt(row.CurrentCPURequestMC, 10),
			strconv.FormatInt(row.CurrentMemRequestKiB, 10),
			savings,
			strconv.FormatBool(row.Stale),
			row.Category,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeNamespaceTable(w io.Writer, recs []namespace.NamespaceRec) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tTERM\tENGINE\tCPU_REQ_MC\tMEM_REQ_KIB\tSAVINGS_CENTS\tCATEGORY")
	for _, r := range recs {
		savings := ""
		if r.EstimatedSavingsCents != nil {
			savings = strconv.FormatInt(*r.EstimatedSavingsCents, 10)
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
			r.Namespace, r.Term, r.Engine,
			r.RecCPURequestMC, r.RecMemRequestKiB, savings, r.Category); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func writeNodeCSV(w io.Writer, recs []node.Rec) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(nodeOutCSVHeader); err != nil {
		return err
	}
	for _, rec := range recs {
		row := toNodeOut(rec)
		savings := ""
		if row.EstimatedSavingsCents != nil {
			savings = strconv.FormatInt(*row.EstimatedSavingsCents, 10)
		}
		if err := cw.Write([]string{
			row.Node, row.Term, row.Engine, row.Category, row.IdleState,
			strconv.FormatInt(row.RecommendedCPUMC, 10),
			strconv.FormatInt(row.RecommendedMemKiB, 10),
			strconv.FormatInt(row.CurrentCPUMC, 10),
			strconv.FormatInt(row.CurrentMemKiB, 10),
			strconv.Itoa(row.NodeCountReduction),
			savings,
			row.InstanceType,
			row.SuggestedInstanceType,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeNodeTable(w io.Writer, recs []node.Rec) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NODE\tTERM\tENGINE\tCPU_MC\tMEM_KIB\tSAVINGS_CENTS\tCATEGORY")
	for _, r := range recs {
		row := toNodeOut(r)
		savings := ""
		if row.EstimatedSavingsCents != nil {
			savings = strconv.FormatInt(*row.EstimatedSavingsCents, 10)
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
			row.Node, row.Term, row.Engine, row.RecommendedCPUMC, row.RecommendedMemKiB, savings, row.Category); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func writeGPUCSV(w io.Writer, recs []gpuRecRow) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(gpuOutCSVHeader); err != nil {
		return err
	}
	for _, rec := range recs {
		row := toGPUOut(rec)
		savings := ""
		if row.EstimatedGPUSavingsCents != nil {
			savings = strconv.FormatInt(*row.EstimatedGPUSavingsCents, 10)
		}
		if err := cw.Write([]string{
			row.Namespace, row.Workload, row.ContainerName, row.Term,
			row.GPUModelName, row.CurrentGPUProfile, row.RecommendedGPUProfile,
			row.Classification, strconv.Itoa(row.GPUCount), savings,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeGPUTable(w io.Writer, recs []gpuRecRow) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tWORKLOAD\tCONTAINER\tTERM\tMODEL\tPROFILE\tCLASS")
	for _, rec := range recs {
		row := toGPUOut(rec)
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Namespace, row.Workload, row.ContainerName, row.Term,
			row.GPUModelName, row.RecommendedGPUProfile, row.Classification); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func writePVCCSV(w io.Writer, recs []pvc.PVCRec) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(pvcOutCSVHeader); err != nil {
		return err
	}
	for _, rec := range recs {
		row := toPVCOut(rec)
		savings := ""
		if row.EstimatedSavingsCents != nil {
			savings = strconv.FormatInt(*row.EstimatedSavingsCents, 10)
		}
		recBytes := ""
		if row.RecommendedBytes != nil {
			recBytes = strconv.FormatInt(*row.RecommendedBytes, 10)
		}
		days := ""
		if row.DaysToFull != nil {
			days = strconv.Itoa(*row.DaysToFull)
		}
		if err := cw.Write([]string{
			row.Namespace, row.PVC, row.Term, row.RecommendationType,
			strconv.FormatInt(row.CapacityBytes, 10),
			strconv.FormatInt(row.RequestBytes, 10),
			strconv.FormatInt(row.UsageBytesMax, 10),
			recBytes, days, savings,
			row.StorageClass, row.VMName,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writePVCTable(w io.Writer, recs []pvc.PVCRec) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tPVC\tTERM\tTYPE\tCAPACITY\tUSAGE_MAX\tSAVINGS_CENTS")
	for _, rec := range recs {
		row := toPVCOut(rec)
		savings := ""
		if row.EstimatedSavingsCents != nil {
			savings = strconv.FormatInt(*row.EstimatedSavingsCents, 10)
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%d\t%s\n",
			row.Namespace, row.PVC, row.Term, row.RecommendationType,
			row.CapacityBytes, row.UsageBytesMax, savings); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func writeVMCSV(w io.Writer, recs []vm.VMRecommendation) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(vmOutCSVHeader); err != nil {
		return err
	}
	for _, rec := range recs {
		row := toVMOut(rec)
		savings := ""
		if row.EstimatedSavingsCents != nil {
			savings = strconv.FormatInt(*row.EstimatedSavingsCents, 10)
		}
		if err := cw.Write([]string{
			row.Namespace, row.VMName, row.Term, row.Engine, row.Category,
			strconv.FormatInt(int64(row.CurrentVCPU), 10),
			strconv.FormatInt(int64(row.CurrentMemoryGiB), 10),
			strconv.FormatInt(int64(row.RecommendedVCPU), 10),
			strconv.FormatInt(int64(row.RecommendedMemoryGiB), 10),
			row.RecommendedInstanceType, row.GuestOS, savings,
			strconv.FormatInt(int64(row.RecommendedTimeSliceCount), 10),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeVMTable(w io.Writer, recs []vm.VMRecommendation) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tVM\tTERM\tENGINE\tVCPU\tMEM_GIB\tSAVINGS_CENTS\tCATEGORY")
	for _, rec := range recs {
		row := toVMOut(rec)
		savings := ""
		if row.EstimatedSavingsCents != nil {
			savings = strconv.FormatInt(*row.EstimatedSavingsCents, 10)
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
			row.Namespace, row.VMName, row.Term, row.Engine,
			row.RecommendedVCPU, row.RecommendedMemoryGiB, savings, row.Category); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func toQuotaOut(r quota.QuotaRec) quotaOut {
	return quotaOut{
		Namespace:                      r.Namespace,
		QuotaName:                      r.QuotaName,
		RecommendationType:             r.RecommendationType,
		RiskLevel:                      r.RiskLevel,
		CPURequestHardMC:               r.Snapshot.CPURequestHardMC,
		CPULimitHardMC:                 r.Snapshot.CPULimitHardMC,
		MemoryRequestHardBytes:         r.Snapshot.MemoryRequestHardBytes,
		MemoryLimitHardBytes:           r.Snapshot.MemoryLimitHardBytes,
		CPURequestRecommendedMC:        r.Recommended.CPURequestMillicores,
		CPULimitRecommendedMC:          r.Recommended.CPULimitMillicores,
		MemoryRequestRecommendedBytes:  r.Recommended.MemoryRequestBytes,
		MemoryLimitRecommendedBytes:    r.Recommended.MemoryLimitBytes,
		StorageRequestHardBytes:        r.Snapshot.StorageRequestHardBytes,
		StorageRequestRecommendedBytes: r.Recommended.StorageRequestBytes,
		PodsHard:                       r.Snapshot.PodsHard,
		PodsRecommended:                r.Recommended.Pods,
	}
}

func writeQuotaCSV(w io.Writer, recs []quota.QuotaRec) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(quotaOutCSVHeader); err != nil {
		return err
	}
	for _, rec := range recs {
		row := toQuotaOut(rec)
		if err := cw.Write([]string{
			row.Namespace, row.QuotaName, row.RecommendationType, row.RiskLevel,
			strconv.FormatInt(row.CPURequestHardMC, 10),
			strconv.FormatInt(row.CPULimitHardMC, 10),
			strconv.FormatInt(row.MemoryRequestHardBytes, 10),
			strconv.FormatInt(row.MemoryLimitHardBytes, 10),
			strconv.FormatInt(row.CPURequestRecommendedMC, 10),
			strconv.FormatInt(row.CPULimitRecommendedMC, 10),
			strconv.FormatInt(row.MemoryRequestRecommendedBytes, 10),
			strconv.FormatInt(row.MemoryLimitRecommendedBytes, 10),
			strconv.FormatInt(row.StorageRequestHardBytes, 10),
			strconv.FormatInt(row.StorageRequestRecommendedBytes, 10),
			strconv.FormatInt(row.PodsHard, 10),
			strconv.FormatInt(row.PodsRecommended, 10),
			"",
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeQuotaTable(w io.Writer, recs []quota.QuotaRec) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tQUOTA\tTYPE\tRISK\tCPU_HARD_MC\tCPU_REC_MC")
	for _, rec := range recs {
		row := toQuotaOut(rec)
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%d\n",
			row.Namespace, row.QuotaName, row.RecommendationType, row.RiskLevel,
			row.CPURequestHardMC, row.CPURequestRecommendedMC); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func toClusterQuotaOut(r quota.ClusterQuotaRec) clusterQuotaOut {
	return clusterQuotaOut{
		ClusterQuotaName:               r.ClusterQuotaName,
		Namespaces:                     r.Namespaces,
		RecommendationType:             r.RecommendationType,
		RiskLevel:                      r.RiskLevel,
		CPURequestHardMC:               r.Snapshot.CPURequestHardMC,
		CPULimitHardMC:                 r.Snapshot.CPULimitHardMC,
		MemoryRequestHardBytes:         r.Snapshot.MemoryRequestHardBytes,
		MemoryLimitHardBytes:           r.Snapshot.MemoryLimitHardBytes,
		CPURequestRecommendedMC:        r.Recommended.CPURequestMillicores,
		CPULimitRecommendedMC:          r.Recommended.CPULimitMillicores,
		MemoryRequestRecommendedBytes:  r.Recommended.MemoryRequestBytes,
		MemoryLimitRecommendedBytes:    r.Recommended.MemoryLimitBytes,
		StorageRequestHardBytes:        r.Snapshot.StorageRequestHardBytes,
		StorageRequestRecommendedBytes: r.StorageRecommendedBytes,
		PodsHard:                       r.Snapshot.PodsHard,
		PodsRecommended:                r.PodsRecommended,
	}
}

func writeClusterQuotaCSV(w io.Writer, recs []quota.ClusterQuotaRec) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(clusterQuotaOutCSVHeader); err != nil {
		return err
	}
	for _, rec := range recs {
		row := toClusterQuotaOut(rec)
		if err := cw.Write([]string{
			row.ClusterQuotaName, row.Namespaces, row.RecommendationType, row.RiskLevel,
			strconv.FormatInt(row.CPURequestHardMC, 10),
			strconv.FormatInt(row.CPULimitHardMC, 10),
			strconv.FormatInt(row.MemoryRequestHardBytes, 10),
			strconv.FormatInt(row.MemoryLimitHardBytes, 10),
			strconv.FormatInt(row.CPURequestRecommendedMC, 10),
			strconv.FormatInt(row.CPULimitRecommendedMC, 10),
			strconv.FormatInt(row.MemoryRequestRecommendedBytes, 10),
			strconv.FormatInt(row.MemoryLimitRecommendedBytes, 10),
			strconv.FormatInt(row.StorageRequestHardBytes, 10),
			strconv.FormatInt(row.StorageRequestRecommendedBytes, 10),
			strconv.FormatInt(row.PodsHard, 10),
			strconv.FormatInt(row.PodsRecommended, 10),
			"",
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeClusterQuotaTable(w io.Writer, recs []quota.ClusterQuotaRec) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CLUSTER_QUOTA\tNAMESPACES\tTYPE\tRISK\tCPU_HARD_MC\tCPU_REC_MC")
	for _, rec := range recs {
		row := toClusterQuotaOut(rec)
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%d\n",
			row.ClusterQuotaName, row.Namespaces, row.RecommendationType, row.RiskLevel,
			row.CPURequestHardMC, row.CPURequestRecommendedMC); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func toSnapshotOut(r snapshot.SnapshotRec) snapshotOut {
	codes := r.NotificationCodes
	if codes == nil {
		codes = []int16{}
	}
	created := ""
	if !r.CreationTimestamp.IsZero() {
		created = r.CreationTimestamp.UTC().Format(time.RFC3339)
	}
	return snapshotOut{
		Namespace:           r.Namespace,
		SnapshotName:        r.SnapshotName,
		SourcePVCName:       r.SourcePVCName,
		VolumeSnapshotClass: r.VolumeSnapshotClass,
		StorageClass:        r.StorageClass,
		CreationTimestamp:   created,
		RestoreSizeBytes:    r.RestoreSizeBytes,
		AgeDays:             r.AgeDays,
		SourcePVCExists:     r.SourcePVCExists,
		RestoredPVCCount:    r.RestoredPVCCount,
		ManagedBy:           r.ManagedBy,
		RecommendationType:  r.RecommendationType,
		EstimatedCostCents:  r.EstimatedCostCents,
		NotificationCodes:   codes,
	}
}

func writeSnapshotCSV(w io.Writer, recs []snapshot.SnapshotRec) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(snapshotOutCSVHeader); err != nil {
		return err
	}
	for _, rec := range recs {
		row := toSnapshotOut(rec)
		cost := ""
		if row.EstimatedCostCents != nil {
			cost = strconv.FormatInt(*row.EstimatedCostCents, 10)
		}
		codes, err := json.Marshal(row.NotificationCodes)
		if err != nil {
			return err
		}
		if err := cw.Write([]string{
			row.Namespace, row.SnapshotName, row.SourcePVCName, row.VolumeSnapshotClass, row.StorageClass,
			row.CreationTimestamp, strconv.FormatInt(row.RestoreSizeBytes, 10), strconv.Itoa(row.AgeDays),
			strconv.FormatBool(row.SourcePVCExists), strconv.Itoa(row.RestoredPVCCount),
			row.ManagedBy, row.RecommendationType, cost, string(codes),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeSnapshotTable(w io.Writer, recs []snapshot.SnapshotRec) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tSNAPSHOT\tTYPE\tAGE_DAYS\tCOST_CENTS")
	for _, rec := range recs {
		row := toSnapshotOut(rec)
		cost := ""
		if row.EstimatedCostCents != nil {
			cost = strconv.FormatInt(*row.EstimatedCostCents, 10)
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n",
			row.Namespace, row.SnapshotName, row.RecommendationType, row.AgeDays, cost); err != nil {
			return err
		}
	}
	return tw.Flush()
}
