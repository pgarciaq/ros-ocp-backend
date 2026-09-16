// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package csv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mgmtManifestJSON = `{"uuid":"a7111871-ff0b-41b1-9b6e-c5231beaedd0","cluster_id":"24894339-cd50-47e5-83a6-1766135fdb03","files":[],"resource_optimization_files":[],"cr_status":{"topology":{"controlPlaneTopology":"HighlyAvailable","hostedClusterCount":1,"hostedControlPlaneNamespaces":["hc01-infra-hc01"]}}}`

const hostedManifestJSON = `{"uuid":"596cabe4-7b96-4ffb-a784-49f2390c3066","cluster_id":"08a33802-9f04-4232-afc7-25152f749adf","files":[],"resource_optimization_files":[],"cr_status":{"topology":{"controlPlaneTopology":"External","managedByHypershift":true}}}`

const legacyManifestJSON = `{"uuid":"a7111871-ff0b-41b1-9b6e-c5231beaedd0","cluster_id":"24894339-cd50-47e5-83a6-1766135fdb03","files":[]}`

func rosCSVBody() string {
	return niseHeader() + "\n" +
		niseRow("app", "api", "2026-08-01 00:00:00 +0000 UTC", "2026-08-01 01:00:00 +0000 UTC", "0.1", "0.05") + "\n"
}

func TestLoad_TarGzCapturesManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "pkg.tar.gz")
	writeGzipTar(t, tarPath, map[string]string{
		"./manifest.json":     mgmtManifestJSON,
		"./ocp_ros_usage.csv": rosCSVBody(),
	})
	got, err := Load(tarPath)
	require.NoError(t, err)
	require.NotNil(t, got.Manifest, "tarball manifest.json must be captured")
	assert.Equal(t, "24894339-cd50-47e5-83a6-1766135fdb03", got.Manifest.ClusterUUID)
	assert.Equal(t, "HighlyAvailable", got.Manifest.Topology.ControlPlaneTopology)
	assert.Equal(t, 1, got.Manifest.Topology.HostedClusterCount)
	assert.Equal(t, []string{"hc01-infra-hc01"}, got.Manifest.Topology.HostedControlPlaneNamespaces)
	assert.False(t, got.Manifest.Topology.ManagedByHypershift)
}

func TestLoad_TarGzWithoutManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "pkg.tar.gz")
	writeGzipTar(t, tarPath, map[string]string{
		"./ocp_ros_usage.csv": rosCSVBody(),
	})
	got, err := Load(tarPath)
	require.NoError(t, err)
	assert.Nil(t, got.Manifest, "absent manifest must stay nil (backward compatible)")
}

func TestLoad_TarGzMalformedManifestIgnored(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "pkg.tar.gz")
	writeGzipTar(t, tarPath, map[string]string{
		"./manifest.json":     "{not json",
		"./ocp_ros_usage.csv": rosCSVBody(),
	})
	got, err := Load(tarPath)
	require.NoError(t, err, "corrupt manifest must not fail previously-working loads")
	assert.Nil(t, got.Manifest)
	require.Len(t, got.Rows, 1)
}

func TestLoad_DirectoryCapturesManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(hostedManifestJSON), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ocp_ros_usage.csv"), []byte(rosCSVBody()), 0o600))
	got, err := Load(dir)
	require.NoError(t, err)
	require.NotNil(t, got.Manifest)
	assert.Equal(t, "08a33802-9f04-4232-afc7-25152f749adf", got.Manifest.ClusterUUID)
	assert.Equal(t, "External", got.Manifest.Topology.ControlPlaneTopology)
	assert.True(t, got.Manifest.Topology.ManagedByHypershift)
}

func TestLoad_DirectoryLegacyManifestYieldsEmptyFacts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(legacyManifestJSON), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ocp_ros_usage.csv"), []byte(rosCSVBody()), 0o600))
	got, err := Load(dir)
	require.NoError(t, err)
	require.NotNil(t, got.Manifest, "pre-#406 manifest still parses (no topology section)")
	assert.Equal(t, "24894339-cd50-47e5-83a6-1766135fdb03", got.Manifest.ClusterUUID)
	assert.Empty(t, got.Manifest.Topology.ControlPlaneTopology)
}
