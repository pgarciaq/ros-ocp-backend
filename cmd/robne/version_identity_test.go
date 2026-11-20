package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVersion_IdentityLines pins #908's Alternative A: identity lines follow
// the version line, and line one stays byte-identical to the historical shape
// so every first-line consumer (the release smoke's assertion, `head -1`
// scripts) keeps working. No runtime banner exists anywhere else — stdout
// purity (#904) must not regress.
func TestVersion_IdentityLines(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, writeVersion(&b))
	lines := strings.Split(b.String(), "\n")

	require.NotEmpty(t, lines)
	assert.Equal(t, "robne devel", lines[0], "line one must stay byte-identical (release smoke, head -1 scripts)")
	assert.Equal(t, "Resource Optimization for OpenShift CLI", lines[1])
	assert.Equal(t, "(c) Copyright 2026 Red Hat", lines[2], "static year string (#908 owner decision)")
	assert.Equal(t, "docs: https://pgarciaq.github.io/ros-ocp-backend/features/robne-cli/", lines[3])
	// The envelope block still follows, separated by a blank line.
	require.Greater(t, len(lines), 5)
	assert.Equal(t, "", lines[4])
	assert.Contains(t, b.String(), "json_envelope_max")
}
