package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

var migrationFileRE = regexp.MustCompile(`^(\d+)_(.+)\.(up|down)\.sql$`)

// TestMigrationSequence_NoGapsAndPairedFiles ensures golang-migrate files are numbered
// contiguously from 1..latest and each version has matching up/down SQL with the same stem.
func TestMigrationSequence_NoGapsAndPairedFiles(t *testing.T) {
	dir := testutil.MigrationsPath()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	type stemPair struct {
		up, down bool
		stem     string
	}
	byVersion := make(map[int]stemPair)

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migrationFileRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		var ver int
		_, scanErr := fmt.Sscanf(m[1], "%d", &ver)
		require.NoError(t, scanErr, "version in %s", e.Name())
		p := byVersion[ver]
		if p.stem == "" {
			p.stem = m[2]
		} else {
			assert.Equal(t, p.stem, m[2], "version %06d up/down stem mismatch", ver)
		}
		switch m[3] {
		case "up":
			p.up = true
		case "down":
			p.down = true
		}
		byVersion[ver] = p
	}

	require.NotEmpty(t, byVersion, "no migration SQL files found under %s", dir)

	versions := make([]int, 0, len(byVersion))
	for v := range byVersion {
		versions = append(versions, v)
	}
	sort.Ints(versions)

	assert.Equal(t, 1, versions[0], "migrations must start at version 1")
	for i := 1; i < len(versions); i++ {
		assert.Equal(t, versions[i-1]+1, versions[i],
			"gap in migration sequence after %06d (next file is %06d)", versions[i-1], versions[i])
	}

	maxVer := versions[len(versions)-1]
	assert.Equal(t, int(latestMigrationVersion), maxVer,
		"latestMigrationVersion constant must match highest migration file number")

	for _, ver := range versions {
		p := byVersion[ver]
		assert.True(t, p.up, "missing .up.sql for version %06d (%s)", ver, p.stem)
		assert.True(t, p.down, "missing .down.sql for version %06d (%s)", ver, p.stem)
		assert.NotEmpty(t, p.stem)
	}

	// Sanity: VM feature migrations present (059 node-term scope through 109 storage tiering).
	for _, want := range []string{
		"000059_scope_partition_function.up.sql",
		"000089_vm_recommendations.up.sql",
		"000109_vm_storage_tiering.up.sql",
	} {
		_, statErr := os.Stat(filepath.Join(dir, want))
		assert.NoError(t, statErr, "expected migration file %s", want)
	}
}
