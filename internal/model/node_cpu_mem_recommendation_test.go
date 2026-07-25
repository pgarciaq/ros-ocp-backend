package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeCategoryFilterValues(t *testing.T) {
	t.Run("valid values", func(t *testing.T) {
		vals, err := NodeCategoryFilterValues([]string{"idle", "overcommitted", "stranded_cpu", "stranded_memory", "underutilized", "optimized"})
		require.NoError(t, err)
		assert.Equal(t, []string{"idle", "overcommitted", "stranded_cpu", "stranded_memory", "underutilized", "optimized"}, vals)
	})
	t.Run("single value", func(t *testing.T) {
		vals, err := NodeCategoryFilterValues([]string{"idle"})
		require.NoError(t, err)
		assert.Equal(t, []string{"idle"}, vals)
	})
	t.Run("invalid value", func(t *testing.T) {
		_, err := NodeCategoryFilterValues([]string{"active"})
		require.Error(t, err)
	})
	t.Run("empty", func(t *testing.T) {
		vals, err := NodeCategoryFilterValues([]string{})
		require.NoError(t, err)
		assert.Empty(t, vals)
	})
}
