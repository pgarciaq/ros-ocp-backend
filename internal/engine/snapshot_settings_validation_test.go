package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSnapshotSettingsUpdate_RejectsInvalidRanges(t *testing.T) {
	zero := 0
	err := ValidateSnapshotSettingsUpdate(SnapshotSettingsUpdate{OrphanAgeDays: &zero})
	require.Error(t, err)
	var valErr *ThresholdValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Errors[0], "orphan_age_days")

	badCost := -0.01
	err = ValidateSnapshotSettingsUpdate(SnapshotSettingsUpdate{CostPerGiBMonthUSD: &badCost})
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Errors[0], "cost_per_gib_month_usd")

	redundant := 0
	err = ValidateSnapshotSettingsUpdate(SnapshotSettingsUpdate{RedundantThreshold: &redundant})
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Errors[0], "redundant_threshold")
}

func TestValidateSnapshotSettingsUpdate_AcceptsValid(t *testing.T) {
	stale := 120
	fresh := 12
	cost := 0.02
	err := ValidateSnapshotSettingsUpdate(SnapshotSettingsUpdate{
		StaleDays:           &stale,
		InventoryFreshHours: &fresh,
		CostPerGiBMonthUSD:  &cost,
	})
	require.NoError(t, err)
}
