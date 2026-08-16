package csv

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_TarGzStripsDotSlash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "pkg.tar.gz")
	writeGzipTar(t, tarPath, map[string]string{
		"./ocp_ros_usage.csv": niseHeader() + "\n" +
			niseRow("app", "api", "2026-08-01 00:00:00 +0000 UTC", "2026-08-01 01:00:00 +0000 UTC", "0.1", "0.05") + "\n",
		"./ocp_pod_usage.csv": "report_period_start,namespace\n2026-08-01,app\n",
	})
	got, err := Load(tarPath)
	require.NoError(t, err)
	require.Len(t, got.Rows, 1)
	assert.Equal(t, []string{"ocp_ros_usage.csv"}, got.Files)
	assert.Equal(t, []string{"ocp_pod_usage.csv"}, got.CostOnlySkipped)
}

func TestLoad_CostOnlyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ocp_pod_usage.csv")
	require.NoError(t, os.WriteFile(path, []byte("interval_start,namespace\n"), 0o600))
	_, err := Load(path)
	var cost *ErrCostOnlyInput
	require.ErrorAs(t, err, &cost)
}

func TestLoad_DirectorySkipsCostOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ocp_pod_usage.csv"), []byte("x\n"), 0o600))
	body := niseHeader() + "\n" +
		niseRow("app", "api", "2026-08-01 00:00:00 +0000 UTC", "2026-08-01 01:00:00 +0000 UTC", "0.1", "0.05") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ocp_ros_usage.csv"), []byte(body), 0o600))
	got, err := Load(dir)
	require.NoError(t, err)
	require.Len(t, got.Rows, 1)
}

func TestLoad_NamespaceOnlyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ocp_ros_namespace_usage.csv")
	require.NoError(t, os.WriteFile(path, []byte(namespaceCSV()), 0o600))
	got, err := Load(path)
	require.NoError(t, err)
	require.Len(t, got.NamespaceRows, 1)
	assert.Empty(t, got.Rows)
	assert.Equal(t, []string{"ocp_ros_namespace_usage.csv"}, got.Files)
	assert.Equal(t, "kube-system", got.NamespaceRows[0].Namespace)
}

func TestLoad_DirectoryLoadsNamespaceAndContainer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := niseHeader() + "\n" +
		niseRow("app", "api", "2026-08-01 00:00:00 +0000 UTC", "2026-08-01 01:00:00 +0000 UTC", "0.1", "0.05") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ocp_ros_usage.csv"), []byte(body), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ocp_ros_namespace_usage.csv"), []byte(namespaceCSV()), 0o600))
	got, err := Load(dir)
	require.NoError(t, err)
	require.Len(t, got.Rows, 1)
	require.Len(t, got.NamespaceRows, 1)
	assert.ElementsMatch(t, []string{"ocp_ros_usage.csv", "ocp_ros_namespace_usage.csv"}, got.Files)
}

func TestLoad_TarGzNamespace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "pkg.tar.gz")
	writeGzipTar(t, tarPath, map[string]string{
		"./ocp_ros_namespace_usage.csv": namespaceCSV(),
		"./ocp_pod_usage.csv":           "report_period_start,namespace\n2026-08-01,app\n",
	})
	got, err := Load(tarPath)
	require.NoError(t, err)
	require.Len(t, got.NamespaceRows, 1)
	assert.Empty(t, got.Rows)
	assert.Equal(t, []string{"ocp_ros_namespace_usage.csv"}, got.Files)
	assert.Equal(t, []string{"ocp_pod_usage.csv"}, got.CostOnlySkipped)
}

func TestLoad_StorageOnlyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ocp_storage_usage.csv")
	require.NoError(t, os.WriteFile(path, []byte(storageCSV()), 0o600))
	got, err := Load(path)
	require.NoError(t, err)
	require.Len(t, got.PVCRows, 1)
	assert.Empty(t, got.Rows)
	assert.Empty(t, got.NamespaceRows)
	assert.Equal(t, []string{"ocp_storage_usage.csv"}, got.Files)
	assert.Equal(t, "data-pvc", got.PVCRows[0].PersistentVolumeClaim)
}

func TestLoad_DirectoryLoadsStorageAndContainer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := niseHeader() + "\n" +
		niseRow("app", "api", "2026-08-01 00:00:00 +0000 UTC", "2026-08-01 01:00:00 +0000 UTC", "0.1", "0.05") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ocp_ros_usage.csv"), []byte(body), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ocp_storage_usage.csv"), []byte(storageCSV()), 0o600))
	got, err := Load(dir)
	require.NoError(t, err)
	require.Len(t, got.Rows, 1)
	require.Len(t, got.PVCRows, 1)
}

func TestLoad_CmOpenShiftStorageIsNotCostOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cm-openshift-storage-usage-202606.4.csv")
	require.NoError(t, os.WriteFile(path, []byte(storageCSV()), 0o600))
	got, err := Load(path)
	require.NoError(t, err)
	require.Len(t, got.PVCRows, 1)
}

func TestLoad_NamespaceAllRowsUnparseable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := strings.Join([]string{
		"interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg",
		"bad-date,2026-03-20 01:00:00 +0000 UTC,ns1,0.500,0.250,1073741824,536870912",
	}, "\n")
	path := filepath.Join(dir, "ocp_ros_namespace_usage.csv")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unparseable")
}

func TestLoad_AllRowsUnparseable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := niseHeader() + "\n" +
		niseRow("app", "api", "2026-08-01 00:00:00 +0000 UTC", "2026-08-01 01:00:00 +0000 UTC", "not-a-number", "0.05") + "\n"
	path := filepath.Join(dir, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unparseable")
}

func writeGzipTar(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	gz := gzip.NewWriter(f)
	defer func() { _ = gz.Close() }()
	tw := tar.NewWriter(gz)
	defer func() { _ = tw.Close() }()
	for name, body := range files {
		hdr := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(body))}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
}

func namespaceCSV() string {
	return strings.Join([]string{
		"interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,kube-system,0.500,0.250,1073741824,536870912",
	}, "\n")
}

func storageCSV() string {
	return strings.Join([]string{
		"interval_start,interval_end,namespace,pod,persistentvolumeclaim,persistentvolume,storageclass,persistentvolumeclaim_capacity_bytes,persistentvolumeclaim_usage_byte_seconds",
		"2026-05-01 00:00:00+00:00,2026-05-01 01:00:00+00:00,production,app-pod-1,data-pvc,pv-data,gp3,10737418240,18000000000000",
	}, "\n")
}
