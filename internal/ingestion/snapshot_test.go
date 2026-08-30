package ingestion

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSnapshotRows_Basic(t *testing.T) {
	csv := "interval_start,interval_end,namespace,snapshot_name,source_pvc_name,volume_snapshot_class,storageclass,creation_timestamp,ready_to_use,restore_size_bytes,source_pvc_exists,restored_pvc_count,labels\n" +
		"2026-01-15T00:00:00Z,2026-01-15T06:00:00Z,production,db-backup-2025-12-01,postgres-data,csi-aws-vsc,gp3,2025-12-01T03:00:00Z,true,53687091200,true,0,\"{\"\"velero.io/backup-name\"\":\"\"daily-backup\"\"}\"\n" +
		"2026-01-15T00:00:00Z,2026-01-15T06:00:00Z,staging,orphaned-snap,deleted-pvc,csi-aws-vsc,gp3,2025-11-01T00:00:00Z,true,21474836480,false,0,{}\n"
	rows, err := ParseSnapshotRows(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Len(t, rows, 2)

	r := rows[0]
	assert.Equal(t, "production", r.Namespace)
	assert.Equal(t, "db-backup-2025-12-01", r.SnapshotName)
	assert.Equal(t, "postgres-data", r.SourcePVCName)
	assert.Equal(t, "csi-aws-vsc", r.VolumeSnapshotClass)
	assert.Equal(t, "gp3", r.StorageClass)
	assert.Equal(t, time.Date(2025, 12, 1, 3, 0, 0, 0, time.UTC), r.CreationTimestamp)
	assert.True(t, r.ReadyToUse)
	assert.Equal(t, int64(53687091200), r.RestoreSizeBytes)
	assert.True(t, r.SourcePVCExists)
	assert.Equal(t, 0, r.RestoredPVCCount)
	assert.Equal(t, "daily-backup", r.Labels["velero.io/backup-name"])

	r2 := rows[1]
	assert.Equal(t, "staging", r2.Namespace)
	assert.False(t, r2.SourcePVCExists)
}

func TestParseSnapshotRows_MissingRequiredColumns(t *testing.T) {
	csv := `interval_start,interval_end,some_other_col
2026-01-15T00:00:00Z,2026-01-15T06:00:00Z,value
`
	_, err := ParseSnapshotRows(strings.NewReader(csv))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing columns")
}

func TestParseSnapshotRows_EmptySnapshotName(t *testing.T) {
	csv := `namespace,snapshot_name,creation_timestamp
production,,2025-12-01T03:00:00Z
`
	rows, err := ParseSnapshotRows(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Len(t, rows, 0) // empty snapshot_name is skipped
}

func TestParseSnapshotRows_EmptySourcePVC(t *testing.T) {
	csv := `namespace,snapshot_name,source_pvc_name,creation_timestamp,restore_size_bytes,source_pvc_exists,restored_pvc_count,labels
production,pre-provisioned-snap,,2025-12-01T03:00:00Z,1073741824,true,0,{}
`
	rows, err := ParseSnapshotRows(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "", rows[0].SourcePVCName)
	assert.Equal(t, "pre-provisioned-snap", rows[0].SnapshotName)
}

func TestParseSnapshotRows_BooleanParsing(t *testing.T) {
	csv := `namespace,snapshot_name,creation_timestamp,ready_to_use,source_pvc_exists
ns,snap1,2025-12-01T03:00:00Z,true,false
ns,snap2,2025-12-01T03:00:00Z,1,0
ns,snap3,2025-12-01T03:00:00Z,yes,no
`
	rows, err := ParseSnapshotRows(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 3)

	assert.True(t, rows[0].ReadyToUse)
	assert.False(t, rows[0].SourcePVCExists)
	assert.True(t, rows[1].ReadyToUse)
	assert.False(t, rows[1].SourcePVCExists)
	assert.True(t, rows[2].ReadyToUse)
	assert.False(t, rows[2].SourcePVCExists)
}

func TestForEachSnapshotCSVRow_DoesNotMaterializeFullSlice(t *testing.T) {
	csv := `namespace,snapshot_name,creation_timestamp
production,snap-a,2025-12-01T03:00:00Z
production,snap-b,2025-12-01T04:00:00Z
`
	seen := 0
	count, err := forEachSnapshotCSVRow(context.Background(), strings.NewReader(csv), func(row SnapshotRow) error {
		seen++
		assert.NotEmpty(t, row.SnapshotName)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Equal(t, 2, seen)
}
