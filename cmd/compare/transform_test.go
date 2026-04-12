package main

import (
	"encoding/csv"
	"os"
	"strings"
	"testing"
)

func TestTransformNiseCSV_WithOOMCount(t *testing.T) {
	header := "interval_start,interval_end,namespace,workload,workload_type,container_name," +
		"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
		"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg," +
		"memory_rss_usage_container_avg,oom_count"
	row := "2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC," +
		"myns,mydeploy,deployment,main,0.1,0.15,0.08,0.001,134217728,134217728,104857600,100000000,3"

	f := writeTempCSV(t, header+"\n"+row+"\n")
	result, err := transformNiseCSV(f)
	if err != nil {
		t.Fatalf("transformNiseCSV: %v", err)
	}

	reader := csv.NewReader(strings.NewReader(string(result)))
	outHeader, err := reader.Read()
	if err != nil {
		t.Fatal(err)
	}

	// oom_count must be present in output header
	found := false
	for _, col := range outHeader {
		if col == "oom_count" {
			found = true
		}
	}
	if !found {
		t.Errorf("output header should contain oom_count, got: %v", outHeader)
	}

	// Read the data row and check oom_count value
	dataRow, err := reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	oomIdx := -1
	for i, col := range outHeader {
		if col == "oom_count" {
			oomIdx = i
		}
	}
	if oomIdx < 0 || dataRow[oomIdx] != "3" {
		t.Errorf("oom_count should be '3', got '%s' (idx=%d)", dataRow[oomIdx], oomIdx)
	}
}

func TestTransformNiseCSV_WithoutOOMCount(t *testing.T) {
	header := "interval_start,interval_end,namespace,workload,workload_type,container_name," +
		"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
		"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg," +
		"memory_rss_usage_container_avg"
	row := "2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC," +
		"myns,mydeploy,deployment,main,0.1,0.15,0.08,0.001,134217728,134217728,104857600,100000000"

	f := writeTempCSV(t, header+"\n"+row+"\n")
	result, err := transformNiseCSV(f)
	if err != nil {
		t.Fatalf("transformNiseCSV: %v", err)
	}

	reader := csv.NewReader(strings.NewReader(string(result)))
	outHeader, err := reader.Read()
	if err != nil {
		t.Fatal(err)
	}

	// oom_count should NOT be in output when absent from input
	for _, col := range outHeader {
		if col == "oom_count" {
			t.Error("output header should not contain oom_count when input lacks it")
		}
	}

	// Data should still parse successfully
	dataRow, err := reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(dataRow) != len(outHeader) {
		t.Errorf("row length %d != header length %d", len(dataRow), len(outHeader))
	}
}

func TestTransformNiseCSV_MissingRequiredColumn(t *testing.T) {
	header := "interval_start,interval_end,namespace,workload,container_name"
	row := "2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,ns,deploy,main"

	f := writeTempCSV(t, header+"\n"+row+"\n")
	_, err := transformNiseCSV(f)
	if err == nil {
		t.Fatal("expected error for missing required columns")
	}
	if !strings.Contains(err.Error(), "missing required column") {
		t.Errorf("expected 'missing required column' error, got: %v", err)
	}
}

func TestTransformNiseCSV_ColumnPassthrough(t *testing.T) {
	header := "interval_start,interval_end,namespace,workload,workload_type,container_name," +
		"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
		"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg," +
		"memory_rss_usage_container_avg,oom_count"
	row := "2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC," +
		"myns,mydeploy,deployment,main,0.1,0.15,0.08,0.001,134217728,134217728,104857600,100000000,5"

	f := writeTempCSV(t, header+"\n"+row+"\n")
	result, err := transformNiseCSV(f)
	if err != nil {
		t.Fatalf("transformNiseCSV: %v", err)
	}

	reader := csv.NewReader(strings.NewReader(string(result)))
	outHeader, _ := reader.Read()

	// Columns should use operator names (no renaming to abbreviated forms)
	for _, col := range outHeader {
		if col == "cpu_request" || col == "mem_request" || col == "workload_name" {
			t.Errorf("found old abbreviated column name %q; expected operator column names", col)
		}
	}

	// Verify the expected operator column names are present
	expectedCols := []string{
		"interval_start", "namespace", "workload", "container_name",
		"cpu_request_container_avg", "memory_request_container_avg",
	}
	headerSet := map[string]bool{}
	for _, col := range outHeader {
		headerSet[col] = true
	}
	for _, want := range expectedCols {
		if !headerSet[want] {
			t.Errorf("missing expected operator column %q in output", want)
		}
	}
}

func writeTempCSV(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "nise-test-*.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}
