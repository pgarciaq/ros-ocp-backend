// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package hcp

import (
	"bytes"
	"encoding/csv"
	"os"
	"testing"

	roscsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKnownWorkloadsPin(t *testing.T) {
	// The pin is the named-workload evidence from #583 (live Agent lab package
	// 20260914T153624): 38 names. The CSVs' 39th distinct workload value is
	// the blank join-gap name, which UnknownWorkloads skips by design — so the
	// pin holds names only, and length-pinning rules out silent pin edits:
	// adding or dropping a name without updating the evidence must go red here.
	assert.Len(t, KnownWorkloads, 38)
	for _, name := range []string{"etcd", "kube-apiserver", "kube-scheduler", "control-plane-operator"} {
		assert.Contains(t, KnownWorkloads, name)
	}
	// KubeVirt-only noise must never enter the pin: its presence would mean
	// the Agent-platform evidence was contaminated.
	assert.NotContains(t, KnownWorkloads, "virt-launcher")
}

func TestUnknownWorkloadsFindsPlantedName(t *testing.T) {
	rows := []roscsv.Row{
		{Namespace: "hc01-infra-hc01", WorkloadName: "etcd"},
		{Namespace: "hc01-infra-hc01", WorkloadName: "weekend-operator"},
		{Namespace: "hc01-infra-hc01", WorkloadName: ""},
	}
	// Exactly the planted name: rules out prefix/substring matching (which
	// would also flag nothing here) and rules out reporting blanks.
	assert.Equal(t, []string{"weekend-operator"}, UnknownWorkloads(rows))
}

func TestWorkloadInventoryTripwire(t *testing.T) {
	// Automated tripwire for risk 3: the pinned inventory is diffed against
	// CSV data and any unpinned workload name fails loudly. In CI the
	// committed synthetic fixture is the input; against a fresh lab pull, set
	// HCP_LIVE_CSV to the ROS container CSV path and the same assertion runs
	// on live data (e.g. HCP_LIVE_CSV=/tmp/opencode/hcp-data/mgmt3/*container*.csv).
	paths := []string{"testdata/hcp-multi-ns.csv"}
	if live := os.Getenv("HCP_LIVE_CSV"); live != "" {
		paths = append(paths, live)
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		require.NoError(t, err, path)
		rows, skipped, err := roscsv.ParseRows(bytes.NewReader(body))
		require.NoError(t, err, path)
		if path == "testdata/hcp-multi-ns.csv" {
			// Fixture quality is controlled: every row must parse, so a
			// malformed fixture row goes red here instead of silently
			// dropping out of the assertions below.
			require.Zero(t, skipped, path)
		} else {
			// Live pulls legitimately skip request-less rows (25 in the
			// 20260914 package: registry-operator token minter + one
			// catalog row). Skips are parser behavior, not tripwire
			// signal — but they hide workloads from UnknownWorkloads,
			// which is why the raw scan below exists.
			t.Logf("%s: parser skipped %d request-less rows", path, skipped)
		}
		assert.Empty(t, UnknownWorkloads(rows), "unpinned workloads in %s: review before W1 inclusion", path)
		assert.Empty(t, rawUnknownWorkloads(t, body), "unpinned workloads (raw scan) in %s: review before W1 inclusion", path)
	}
}

// rawWorkloads extracts distinct non-blank workload names straight from CSV
// text, bypassing the ROS parser: request-less rows never become roscsv.Row,
// so a tripwire built only on parsed rows is blind to them.
func rawWorkloads(t *testing.T, body []byte) []string {
	t.Helper()
	reader := csv.NewReader(bytes.NewReader(body))
	header, err := reader.Read()
	require.NoError(t, err)
	workloadIdx := -1
	for i, col := range header {
		if col == "workload" {
			workloadIdx = i
		}
	}
	require.NotEqual(t, -1, workloadIdx, "fixture must carry a workload column")
	seen := map[string]bool{}
	var names []string
	for {
		record, err := reader.Read()
		if err != nil {
			break
		}
		if name := record[workloadIdx]; name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

func TestTripwireSeesThroughParserSkips(t *testing.T) {
	// A request-less row with an unpinned workload mirrors the live
	// registry-operator shape (usage without requests): the parser skips it,
	// so the parsed-rows scan reports nothing. The raw scan must still flag
	// the name — otherwise a request-less newcomer enters scope unnoticed.
	body := "namespace,workload,workload_type,container_name,pod,interval_start,interval_end,cpu_request_container_avg,cpu_usage_container_avg,memory_request_container_avg,memory_usage_container_avg\n" +
		"hc01-infra-hc01,ghost-operator,ReplicaSet,ghost,ghost-0,2026-09-15 00:00:01 +0000 UTC,2026-09-15 00:15:00 +0000 UTC,,0.05,,52428800\n"
	parsed, _, err := roscsv.ParseRows(bytes.NewReader([]byte(body)))
	require.NoError(t, err)
	assert.Empty(t, UnknownWorkloads(parsed), "documents the blind spot: skipped rows never reach the pin check")
	assert.Equal(t, []string{"ghost-operator"}, rawUnknownWorkloads(t, []byte(body)))
}

// rawUnknownWorkloads diffs raw-scan names against the pin: the tripwire's
// second layer, immune to parser skips.
func rawUnknownWorkloads(t *testing.T, body []byte) []string {
	t.Helper()
	var unknown []string
	for _, name := range rawWorkloads(t, body) {
		if _, ok := KnownWorkloads[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	return unknown
}
