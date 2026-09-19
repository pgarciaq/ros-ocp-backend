// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHCPNamespacesFromManifest(t *testing.T) {
	// Nil manifest (single CSV, no payload dir): guardrails stay off,
	// never error — pre-#406 payloads keep working.
	assert.Nil(t, hcpNamespacesFromManifest(nil))
	// Empty facts: off, not empty-but-present.
	assert.Empty(t, hcpNamespacesFromManifest(&csv.Manifest{}))
	// Populated facts: exact list, no reshaping.
	m := &csv.Manifest{Topology: topology.TopologyFacts{
		ControlPlaneTopology:         "HighlyAvailable",
		HostedClusterCount:           1,
		HostedControlPlaneNamespaces: []string{"hc01-infra-hc01"},
	}}
	assert.Equal(t, []string{"hc01-infra-hc01"}, hcpNamespacesFromManifest(m))
}

// End-to-end Path 1 (#584): payload dir with manifest.json listing the HCP
// namespace routes container recs to the guardrail profile; the same
// payload without the manifest stays generic. oneDayCSV rows request
// 200m/100MiB at 50m/50MiB usage (recs in KiB: 102400 request).
// CPU relative leg governs (140m > 100m absolute); memory absolute leg
// governs (0.7*102400 = 71680KiB < 128MiB absolute). The memory relative
// leg is covered in hcp.TestEffectiveFloor.
func TestRecommend_HCPGuardrailFromPayloadManifest(t *testing.T) {
	setup := func(t *testing.T, withManifest bool) string {
		cwd := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(cwd, "ocp_ros_usage.csv"),
			[]byte(oneDayCSV("hc01-infra-hc01", "etcd", "cluster-a")), 0o600))
		if withManifest {
			require.NoError(t, os.WriteFile(filepath.Join(cwd, "manifest.json"), []byte(
				`{"cluster_id":"cluster-a","cr_status":{"topology":{"controlPlaneTopology":"HighlyAvailable","hostedClusterCount":1,"hostedControlPlaneNamespaces":["hc01-infra-hc01"]}}}`), 0o600))
		}
		return cwd
	}
	run := func(t *testing.T, cwd string) shortCostRec {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
		t.Setenv("ROBNE_NO_USER_CONFIG", "1")
		t.Chdir(cwd)
		result, err := computeRecommendations(commonFlags{
			input:        cwd,
			noUserConfig: true,
			now:          "2026-08-01T02:00:00Z",
			format:       "json",
		})
		require.NoError(t, err)
		for _, r := range result.Recs {
			if r.Namespace == "hc01-infra-hc01" && r.Term == "short" && r.Engine == "cost" {
				return shortCostRec{cpu: r.RecCPURequestMC, mem: r.RecMemRequestKiB, n: len(result.Recs)}
			}
		}
		t.Fatal("expected a short/cost rec for the HCP namespace")
		return shortCostRec{}
	}

	with := run(t, setup(t, true))
	without := run(t, setup(t, false))

	// Detect-not-exclude: manifest changes values, never row counts.
	assert.Equal(t, without.n, with.n, "manifest must not change rec counts")
	// Guardrail floors govern the manifest run (CPU relative leg: 140m;
	// memory absolute leg: 128MiB, since 70% of the 100MiB request is smaller).
	assert.GreaterOrEqual(t, with.cpu, int64(140), "HCP row must take the guardrail CPU floor")
	assert.GreaterOrEqual(t, with.mem, int64(131072), "HCP row must take the guardrail memory floor")
	// Floors only raise: identical rows score at least generic without it.
	assert.GreaterOrEqual(t, with.cpu, without.cpu, "guardrail must not lower CPU vs generic")
	assert.GreaterOrEqual(t, with.mem, without.mem, "guardrail must not lower memory vs generic")
}

type shortCostRec struct {
	cpu int64
	mem int64
	n   int
}
