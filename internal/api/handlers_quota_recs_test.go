package api

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBpToPercentPtr(t *testing.T) {
	assert.Nil(t, bpToPercentPtr(sql.NullInt64{}))
	pct := bpToPercentPtr(sql.NullInt64{Int64: 2500, Valid: true})
	assert.NotNil(t, pct)
	assert.InDelta(t, 25.0, *pct, 0.001)
}

func TestQuotaValuesFromNull(t *testing.T) {
	assert.Nil(t, quotaValuesFromNull(sql.NullInt64{}, sql.NullInt64{}, sql.NullInt64{}, sql.NullInt64{}))
	vals := quotaValuesFromNull(
		sql.NullInt64{Int64: 1000, Valid: true},
		sql.NullInt64{}, sql.NullInt64{}, sql.NullInt64{},
	)
	assert.NotNil(t, vals)
	assert.Equal(t, int64(1000), *vals.CPURequestMillicores)
}
