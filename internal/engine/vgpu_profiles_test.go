package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchVGPUModel_A100(t *testing.T) {
	spec := MatchVGPUModel("NVIDIA A100-SXM4-80GB")
	require.NotNil(t, spec)
	assert.NotEmpty(t, spec.Profiles)
}

func TestVGPUProfileFBMiB(t *testing.T) {
	assert.Equal(t, 10240, VGPUProfileFBMiB("NVIDIA A100-SXM4-80GB", "grid_a100-10q"))
}
