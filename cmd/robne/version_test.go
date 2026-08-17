package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersion_DefaultDevel(t *testing.T) {
	assert.Equal(t, "devel", version)

	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"version"})
	require.NoError(t, root.Execute())

	out := buf.String()
	assert.Contains(t, out, "robne devel")
	assert.Contains(t, out, fmt.Sprintf("json_envelope_max %d", jsonEnvelopeMax()))
	for _, row := range envelopeCapability() {
		assert.Regexp(t, fmt.Sprintf(`(?m)^%s\s+%d$`, row.name, row.v), out)
	}
}

func TestVersion_NoDashDashVersionFlag(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"--version"})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flag")
}

func TestEnvelopeCapability_CoversStdoutPluginsAndBH(t *testing.T) {
	names := map[string]int{}
	for _, row := range envelopeCapability() {
		names[row.name] = row.v
	}
	for _, p := range stdoutEntityPlugins {
		_, ok := names[p]
		assert.True(t, ok, "missing capability row for plugin %s", p)
	}
	assert.Equal(t, recommendJSONVersionWithBusinessHours, names["business_hours"])
	_, timeslicing := names["gpu_timeslicing"]
	assert.False(t, timeslicing, "timeslicing is a GPU sibling, not a separate envelope bump")
	assert.Equal(t, recommendJSONVersionWithBusinessHours, jsonEnvelopeMax())
}

func TestEnvelopeVersion_ContainerOnlyStaysOne(t *testing.T) {
	assert.Equal(t, recommendJSONVersion, envelopeVersion([]string{"container"}, false))
	assert.Equal(t, recommendJSONVersionWithBusinessHours, envelopeVersion([]string{"container"}, true))
	assert.Greater(t, jsonEnvelopeMax(), envelopeVersion([]string{"container"}, false))
}

func TestRecommendHelp_MentionsPerRunVersion(t *testing.T) {
	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"recommend", "--help"})
	require.NoError(t, root.Execute())
	out := buf.String()
	assert.True(t, strings.Contains(out, "per-run"), out)
	assert.True(t, strings.Contains(out, "robne version"), out)
}

func TestBinaryVersion_EmptyIsDevel(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })
	version = "  "
	assert.Equal(t, "devel", binaryVersion())
	version = "abc123-dirty"
	assert.Equal(t, "abc123-dirty", binaryVersion())
}
