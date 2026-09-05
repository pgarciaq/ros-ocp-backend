package db_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
)

const testCACertA = "-----BEGIN CERTIFICATE-----\nca-cert-a\n-----END CERTIFICATE-----\n"
const testCACertB = "-----BEGIN CERTIFICATE-----\nca-cert-b\n-----END CERTIFICATE-----\n"

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
