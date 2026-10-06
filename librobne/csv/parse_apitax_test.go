package csv

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAPITaxRows_Basic(t *testing.T) {
	csv := "webhook_name,window_start,window_end,le,bucket_count,total_count,rejected_count,collected_at,source\n" +
		"pod.network-node-identity.openshift.io,2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,0.5,87,102,2,2026-09-29 11:00:30 +0000 UTC,kubernetes\n" +
		"pod.network-node-identity.openshift.io,2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,+Inf,102,102,2,2026-09-29 11:00:30 +0000 UTC,kubernetes\n"
	rows, skipped, err := ParseAPITaxRows(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Equal(t, 0, skipped)
	require.Len(t, rows, 2)
	assert.Equal(t, "pod.network-node-identity.openshift.io", rows[0].WebhookName)
	assert.InDelta(t, 0.5, rows[0].Le, 1e-9)
	assert.Equal(t, int64(87), rows[0].BucketCount)
	assert.True(t, rows[0].HasTotal)
	assert.Equal(t, int64(102), rows[0].TotalCount)
	assert.True(t, rows[0].HasRejected)
	assert.Equal(t, int64(2), rows[0].RejectedCount)
	assert.True(t, rows[1].Le > 0) // +Inf
	assert.Equal(t, "kubernetes", rows[1].Source)
}

func TestParseAPITaxRows_OptionalTotalsMayBeEmpty(t *testing.T) {
	csv := "webhook_name,window_start,window_end,le,bucket_count,total_count,rejected_count,collected_at,source\n" +
		"pod.network-node-identity.openshift.io,2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,+Inf,102,,,2026-09-29 11:00:30 +0000 UTC,kubernetes\n"
	rows, skipped, err := ParseAPITaxRows(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Equal(t, 0, skipped)
	require.Len(t, rows, 1)
	assert.False(t, rows[0].HasTotal, "empty totals stay unknown, never zero-filled")
	assert.False(t, rows[0].HasRejected)
}

func TestParseAPITaxRows_SkipsMalformed(t *testing.T) {
	csv := "webhook_name,window_start,window_end,le,bucket_count,total_count,rejected_count,collected_at,source\n" +
		",2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,+Inf,5,5,0,2026-09-29 11:00:30 +0000 UTC,kubernetes\n" +
		"prometheusrules.openshift.io,2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,+Inf,-3,0,0,2026-09-29 11:00:30 +0000 UTC,kubernetes\n" +
		"prometheusrules.openshift.io,2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,+Inf,9,9,0,2026-09-29 11:00:30 +0000 UTC,kubernetes\n"
	rows, skipped, err := ParseAPITaxRows(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Equal(t, 2, skipped, "empty webhook + negative count skip with counters")
	require.Len(t, rows, 1)
	assert.Equal(t, int64(9), rows[0].BucketCount)
}

func TestParseAPITaxRows_MissingColumns(t *testing.T) {
	csv := "webhook_name,window_start,le,bucket_count\n" +
		"x,2026-09-29 10:00:00 +0000 UTC,+Inf,5\n"
	_, _, err := ParseAPITaxRows(strings.NewReader(csv))
	require.Error(t, err)
	var missing *MissingAPITaxColumnsError
	require.ErrorAs(t, err, &missing)
	assert.NotEmpty(t, missing.Columns)
}

func TestClassifyFilename_APITax(t *testing.T) {
	assert.Equal(t, KindAPITax, ClassifyFilename("ros-openshift-apitax-202609.csv"))
	assert.Equal(t, KindAPITax, ClassifyFilename("d684644b-1111-2222-3333-444455556666-ros-openshift-apitax-202609.csv"))
}
