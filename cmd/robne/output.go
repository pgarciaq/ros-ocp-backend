package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

func writeRecs(w io.Writer, recs []types.ContainerRec, format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json", "":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(recs)
	case "csv":
		return writeCSV(w, recs)
	case "table":
		return writeTable(w, recs)
	default:
		return fmt.Errorf("unknown --format %q (json, csv, table)", format)
	}
}

func writeCSV(w io.Writer, recs []types.ContainerRec) error {
	cw := csv.NewWriter(w)
	header := []string{
		"namespace", "workload", "workload_type", "container_name", "term", "engine",
		"rec_cpu_request_mc", "rec_cpu_limit_mc", "rec_mem_request_kib", "rec_mem_limit_kib",
		"current_cpu_request_mc", "current_mem_request_kib", "estimated_savings_cents",
		"stale", "idle_state", "category",
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, r := range recs {
		savings := ""
		if r.EstimatedSavingsCents != nil {
			savings = strconv.FormatInt(*r.EstimatedSavingsCents, 10)
		}
		row := []string{
			r.Namespace, r.Workload, r.WorkloadType, r.ContainerName, r.Term, r.Engine,
			strconv.FormatInt(r.RecCPURequestMC, 10),
			strconv.FormatInt(r.RecCPULimitMC, 10),
			strconv.FormatInt(r.RecMemRequestKiB, 10),
			strconv.FormatInt(r.RecMemLimitKiB, 10),
			strconv.FormatInt(r.CurrentCPURequestMC, 10),
			strconv.FormatInt(r.CurrentMemRequestKiB, 10),
			savings,
			strconv.FormatBool(r.Stale),
			string(r.IdleState),
			r.Category,
		}
		if err := cw.Write(row); err != nil {
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
