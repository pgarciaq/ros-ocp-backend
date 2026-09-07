package db_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
)

const testCACertA = "-----BEGIN CERTIFICATE-----\nca-cert-a\n-----END CERTIFICATE-----\n"
const testCACertB = "-----BEGIN CERTIFICATE-----\nca-cert-b\n-----END CERTIFICATE-----\n"

// Unique contents: deterministic paths live in shared /tmp, so new tests
// must never reuse testCACertA/B paths.
const testCACertSymlink = "-----BEGIN CERTIFICATE-----\nca-cert-symlink-case\n-----END CERTIFICATE-----\n"
const testCACertStale = "-----BEGIN CERTIFICATE-----\nca-cert-stale-case\n-----END CERTIFICATE-----\n"

// Same content must reuse one deterministic path with correct content (#533).
func TestCreateCACertFile_ReusesPathAndWritesContent(t *testing.T) {
	p1 := database.CreateCACertFile(testCACertA)
	t.Cleanup(func() { _ = os.Remove(p1) })
	p2 := database.CreateCACertFile(testCACertA)
	assert.Equal(t, p1, p2, "same cert content must reuse the same file")

	data, err := os.ReadFile(p1)
	require.NoError(t, err)
	assert.Equal(t, testCACertA, string(data))

	// Distinct content must not clobber: co-located processes may carry
	// different CA bundles.
	p3 := database.CreateCACertFile(testCACertB)
	t.Cleanup(func() { _ = os.Remove(p3) })
	assert.NotEqual(t, p1, p3)
}

// 0600 must hold even if the file pre-existed with wider perms (#533).
func TestCreateCACertFile_Enforces0600(t *testing.T) {
	p := database.CreateCACertFile(testCACertA)
	t.Cleanup(func() { _ = os.Remove(p) })

	require.NoError(t, os.Chmod(p, 0o644))
	p2 := database.CreateCACertFile(testCACertA)
	assert.Equal(t, p, p2)

	fi, err := os.Stat(p2)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

// A pre-planted symlink at the deterministic path must be replaced, never
// followed: pre-fix O_TRUNC truncated the victim through the link (#544).
func TestCreateCACertFile_ReplacesSymlinkWithoutTouchingVictim(t *testing.T) {
	// caCertFilePath is unexported: learn the path with a first write.
	probe := database.CreateCACertFile(testCACertSymlink)
	require.NoError(t, os.Remove(probe))
	t.Cleanup(func() { _ = os.Remove(probe) })

	const sentinel = "victim-sentinel-must-survive"
	victimPath := filepath.Join(t.TempDir(), "victim.txt")
	require.NoError(t, os.WriteFile(victimPath, []byte(sentinel), 0o600))
	require.NoError(t, os.Symlink(victimPath, probe))

	p := database.CreateCACertFile(testCACertSymlink)
	assert.Equal(t, probe, p, "deterministic reuse must hold")

	// Victim byte-identical: names the truncate-follow mechanism on failure.
	data, err := os.ReadFile(victimPath)
	require.NoError(t, err)
	assert.Equal(t, sentinel, string(data))

	// Served bytes are the cert and the installed path is a real file now.
	served, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, testCACertSymlink, string(served))
	fi, err := os.Lstat(p)
	require.NoError(t, err)
	assert.False(t, fi.Mode()&os.ModeSymlink != 0, "rename must replace the symlink itself")
}

// A stale regular file at the deterministic path is replaced wholesale,
// which also heals wide permissions without a separate chmod path.
func TestCreateCACertFile_ReplacesStaleFile(t *testing.T) {
	probe := database.CreateCACertFile(testCACertStale)
	require.NoError(t, os.WriteFile(probe, []byte("stale-garbage"), 0o644))
	t.Cleanup(func() { _ = os.Remove(probe) })

	p := database.CreateCACertFile(testCACertStale)
	assert.Equal(t, probe, p)

	served, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, testCACertStale, string(served))
	fi, err := os.Stat(p)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}
