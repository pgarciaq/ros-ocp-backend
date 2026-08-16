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
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

const recommendJSONVersion = 1
const recommendJSONVersionWithNamespace = 2
const recommendJSONVersionWithNode = 3
const recommendJSONVersionWithGPU = 4

var stdoutEntityPlugins = []string{"container", "namespace", "node", "gpu"}

// recommendResult is the CLI-owned stdout payload (engine recs plus run metadata).
type recommendResult struct {
	Recs           []types.ContainerRec
	NamespaceRecs  []namespace.NamespaceRec
	NodeRecs       []node.Rec
	GPURecs        []gpuRecRow
	GPUTimeslicing []gpu.TimeslicingRec
	Digests        []types.KeyedDigest
	ClusterID      string
	OrgID          string
	Now            time.Time
	SkippedRows    int
	plugins        []string
}

// recommendJSON is the versioned --format json envelope. Phase 3 diff consumes this.
type recommendJSON struct {
	Version                       int                  `json:"version"`
	ClusterID                     string               `json:"cluster_id"`
	Now                           string               `json:"now"`
	SkippedRows                   int                  `json:"skipped_rows"`
	Recommendations               []containerOut       `json:"recommendations"`
	NamespaceRecommendations      *[]namespaceOut      `json:"namespace_recommendations,omitempty"`
	NodeRecommendations           *[]nodeOut           `json:"node_recommendations,omitempty"`
	GPURecommendations            *[]gpuOut            `json:"gpu_recommendations,omitempty"`
	GPUTimeslicingRecommendations *[]gpuTimeslicingOut `json:"gpu_timeslicing_recommendations,omitempty"`
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

// gpuRecRow pairs container identity with a GPURec. GPURec has no namespace fields.
type gpuRecRow struct {
	Namespace     string
	Workload      string
	ContainerName string
	Rec           gpu.GPURec
}

func writeRecs(w io.Writer, result recommendResult, format string) error {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "json"
	}
	if stdoutEntityCount(result.plugins) > 1 && (format == "csv" || format == "table") {
		return fmt.Errorf("--format %s is one entity per stream; use json when --plugins includes more than one of container, namespace, node, gpu", format)
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

func envelopeVersion(plugins []string) int {
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
	return v
}

func writeJSON(w io.Writer, result recommendResult) error {
	env := recommendJSON{
		Version:         envelopeVersion(result.plugins),
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
