package csv

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
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
