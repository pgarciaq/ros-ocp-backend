package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLintMigrations_FlagsNonConcurrentIndexOnLargeTable(t *testing.T) {
	root, err := filepath.Abs(filepath.Join(".."))
	require.NoError(t, err)
	script := filepath.Join(root, "scripts", "lint-migrations.sh")
	require.NoError(t, os.Chmod(script, 0o755))

	fixture := filepath.Join(t.TempDir(), "bad.up.sql")
	require.NoError(t, os.WriteFile(fixture, []byte(`
CREATE INDEX IF NOT EXISTS idx_bad ON node_recommendations (org_id, cluster_uuid);
`), 0o644))

	cmd := exec.Command(script, fixture)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.Error(t, err, "expected lint failure, output: %s", out)
	require.Contains(t, string(out), "node_recommendations")
}

func TestLintMigrations_AllowsConcurrentIndex(t *testing.T) {
	root, err := filepath.Abs(filepath.Join(".."))
	require.NoError(t, err)
	script := filepath.Join(root, "scripts", "lint-migrations.sh")

	fixture := filepath.Join(t.TempDir(), "good.up.sql")
	require.NoError(t, os.WriteFile(fixture, []byte(`
-- documented for K8s Job; migration uses IF NOT EXISTS no-op
-- CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ok ON node_recommendations (org_id);
SELECT 1;
`), 0o644))

	cmd := exec.Command(script, fixture)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "output: %s", out)
}
