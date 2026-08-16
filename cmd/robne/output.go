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

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

const recommendJSONVersion = 1

// recommendResult is the CLI-owned stdout payload (engine recs plus run metadata).
type recommendResult struct {
	Recs        []types.ContainerRec
	ClusterID   string
	OrgID       string
	Now         time.Time
	SkippedRows int
}

// recommendJSON is the versioned --format json envelope. Phase 3 diff consumes this.
type recommendJSON struct {
	Version         int            `json:"version"`
	ClusterID       string         `json:"cluster_id"`
	Now             string         `json:"now"`
	SkippedRows     int            `json:"skipped_rows"`
	Recommendations []containerOut `json:"recommendations"`
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

var containerOutCSVHeader = []string{
	"namespace", "workload", "workload_type", "container_name", "term", "engine",
	"rec_cpu_request_mc", "rec_cpu_limit_mc", "rec_mem_request_kib", "rec_mem_limit_kib",
	"current_cpu_request_mc", "current_mem_request_kib", "estimated_savings_cents",
	"stale", "idle_state", "category",
}

func writeRecs(w io.Writer, result recommendResult, format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json", "":
		return writeJSON(w, result)
	case "csv":
		return writeCSV(w, result.Recs)
	case "table":
		return writeTable(w, result.Recs)
	default:
		return fmt.Errorf("unknown --format %q (json, csv, table)", format)
	}
}

func writeJSON(w io.Writer, result recommendResult) error {
	env := recommendJSON{
		Version:         recommendJSONVersion,
		ClusterID:       result.ClusterID,
		Now:             result.Now.UTC().Format(time.RFC3339),
		SkippedRows:     result.SkippedRows,
		Recommendations: make([]containerOut, len(result.Recs)),
	}
	for i, rec := range result.Recs {
		env.Recommendations[i] = toContainerOut(rec)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
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
