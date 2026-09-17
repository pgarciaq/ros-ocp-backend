// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package hcp

import (
	"bytes"
	"os"
	"testing"

	roscsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustParseFixture parses the synthetic multi-namespace CSV through the real
// ROS parser: the test exercises header mapping plus filtering, not filtering
// alone, so a header regression also goes red here.
func mustParseFixture(t *testing.T) []roscsv.Row {
	t.Helper()
	body, err := os.ReadFile("testdata/hcp-multi-ns.csv")
	require.NoError(t, err)
	rows, skipped, err := roscsv.ParseRows(bytes.NewReader(body))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, rows, 9)
	return rows
}

func TestIncludeRowMultiNamespace(t *testing.T) {
	known := NewNamespaceSet([]string{"hc01-infra-hc01"})
	rows := mustParseFixture(t)

	// keyed by row index: each assertion names the pre-lock behavior it rules out.
	cases := []struct {
		row  int
		want bool
		why  string
	}{
		// HCP-namespace control-plane rows are the W1 input.
		{0, true, "etcd in the HCP namespace must be included"},
		{1, true, "kube-apiserver in the HCP namespace must be included"},
		// Catalog pods are CP-adjacent, not tenants: pinning them in rules out
		// a "strict CP components only" reading of the lock.
		{2, true, "in-namespace OLM catalog pods must be included"},
		// Blank workload (live CSV join gap, registry-operator token minter)
		// still classifies: namespace decides, owner joins do not.
		{3, true, "blank-workload rows in the HCP namespace must be included"},
		// Same pod name on both sides of the boundary: a name-matching
		// implementation includes row 4 and the test goes red.
		{4, false, "etcd-0 outside the HCP namespace must be excluded despite the identical pod name"},
		// Same workload name as an HCP inventory entry, wrong namespace.
		{5, false, "konnectivity-agent in kube-system must be excluded"},
		// Non-HCP virt-launcher is excluded by namespace. KubeVirt-platform
		// virt-launcher *inside* an HCP namespace is out of scope: the lock
		// covers Agent only, so no such row exists in this fixture.
		{6, false, "virt-launcher outside the HCP namespace must be excluded"},
		// Namespace pattern alone must not decide: hc02 matches the
		// {hc.namespace}-{hc.name} shape but is not in the known set.
		{7, false, "HCP-shaped namespace outside the known set must be excluded"},
		// Degenerate rows never match.
		{8, false, "empty namespace must be excluded"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, IncludeRow(rows[tc.row], known), "row %d (%s/%s): %s",
			tc.row, rows[tc.row].Namespace, rows[tc.row].Pod, tc.why)
	}
}

func TestIncludeRowNilSetExcludes(t *testing.T) {
	// No known namespaces means no input: rules out a nil-map panic and a
	// permissive default in one assertion.
	assert.False(t, IncludeRow(roscsv.Row{Namespace: "hc01-infra-hc01"}, nil))
}
