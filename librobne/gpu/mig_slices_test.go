package gpu

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigTotalSlices(t *testing.T) {
	spec := MatchGPUModel("NVIDIA A100-SXM4-80GB")
	require.NotNil(t, spec)
	assert.Equal(t, 7, MigTotalSlices(spec))
}

func TestMigProfileSlices(t *testing.T) {
	spec := MatchGPUModel("NVIDIA A100-SXM4-80GB")
	require.NotNil(t, spec)
	assert.Equal(t, 1, MigProfileSlices(spec, "1g.10gb"))
	assert.Equal(t, 3, MigProfileSlices(spec, "3g.40gb"))
	assert.Equal(t, 7, MigProfileSlices(spec, "7g.80gb"))
	assert.Equal(t, 0, MigProfileSlices(spec, "nonexistent"))
}
