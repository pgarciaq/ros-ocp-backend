package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBucketGranularitySQL_KnownValues(t *testing.T) {
	sql, err := BucketGranularity6Hour.sql()
	require.NoError(t, err)
	assert.Contains(t, sql, "21600")

	sql, err = BucketGranularityDaily.sql()
	require.NoError(t, err)
	assert.Contains(t, sql, "date_trunc")
}

func TestBucketGranularitySQL_UnknownValueReturnsError(t *testing.T) {
	var bg BucketGranularity = 99
	_, err := bg.sql()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unhandled BucketGranularity")
}
