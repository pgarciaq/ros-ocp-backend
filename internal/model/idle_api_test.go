package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdleStateFilterValues(t *testing.T) {
	states, err := IdleStateFilterValues("zombie,idle")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"zombie", "idle"}, states)

	_, err = IdleStateFilterValues("bogus")
	require.Error(t, err)
}

func TestPopulateContainerIdleFields_Zombie(t *testing.T) {
	since := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	days := 21
	peakCPU := int64(5)
	waste := int64(420000)

	var result NativeContainerResult
	PopulateContainerIdleFields(&result, "zombie", &since, &days, &peakCPU, nil, &waste, true)

	assert.Equal(t, "zombie", result.IdleState)
	require.NotNil(t, result.IdleSince)
	assert.Equal(t, "2026-04-15", *result.IdleSince)
	require.NotNil(t, result.IdleRecommendation)
	assert.Equal(t, "terminate", result.IdleRecommendation.Action)
	assert.Equal(t, "high", result.IdleRecommendation.Confidence)
	assert.Nil(t, result.EstimatedMonthlySavings)
	require.NotNil(t, result.EstimatedMonthlyWaste)
}

func TestPopulateContainerIdleFields_Active(t *testing.T) {
	var result NativeContainerResult
	PopulateContainerIdleFields(&result, "active", nil, nil, nil, nil, nil, true)
	assert.Equal(t, "active", result.IdleState)
	assert.Nil(t, result.IdleRecommendation)
}
