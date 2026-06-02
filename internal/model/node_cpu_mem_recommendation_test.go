package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStrandedResourceFilterValue(t *testing.T) {
	t.Run("cpu", func(t *testing.T) {
		v, none, err := StrandedResourceFilterValue("cpu")
		require.NoError(t, err)
		assert.Equal(t, "cpu", v)
		assert.False(t, none)
	})
	t.Run("memory", func(t *testing.T) {
		v, none, err := StrandedResourceFilterValue("MEMORY")
		require.NoError(t, err)
		assert.Equal(t, "memory", v)
		assert.False(t, none)
	})
	t.Run("none", func(t *testing.T) {
		_, none, err := StrandedResourceFilterValue("none")
		require.NoError(t, err)
		assert.True(t, none)
	})
	t.Run("invalid", func(t *testing.T) {
		_, _, err := StrandedResourceFilterValue("network")
		require.Error(t, err)
	})
}
