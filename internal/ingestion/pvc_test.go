package ingestion

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePVCRows_BasicCSV(t *testing.T) {
	csv := `report_period_start,report_period_end,interval_start,interval_end,namespace,pod,node,persistentvolumeclaim,persistentvolume,storageclass,csi_driver,csi_volume_handle,persistentvolumeclaim_capacity_bytes,persistentvolumeclaim_capacity_byte_seconds,volume_request_storage_byte_seconds,persistentvolumeclaim_usage_byte_seconds,persistentvolume_labels,persistentvolumeclaim_labels
2026-05-01 00:00:00+00:00,2026-05-31 00:00:00+00:00,2026-05-01 00:00:00+00:00,2026-05-01 01:00:00+00:00,production,app-pod-1,worker-1,data-pvc,pv-data,gp3,ebs.csi.aws.com,vol-123,10737418240,38654705664000,36000000000000,18000000000000,label_a:val_a,label_b:val_b
2026-05-01 00:00:00+00:00,2026-05-31 00:00:00+00:00,2026-05-01 01:00:00+00:00,2026-05-01 02:00:00+00:00,production,app-pod-1,worker-1,data-pvc,pv-data,gp3,ebs.csi.aws.com,vol-123,10737418240,38654705664000,36000000000000,21600000000000,label_a:val_a,label_b:val_b
`

	rows, err := ParsePVCRows(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Len(t, rows, 2)

	assert.Equal(t, "production", rows[0].Namespace)
	assert.Equal(t, "data-pvc", rows[0].PersistentVolumeClaim)
	assert.Equal(t, "app-pod-1", rows[0].Pod)
	assert.Equal(t, "pv-data", rows[0].PersistentVolume)
	assert.Equal(t, "gp3", rows[0].StorageClass)
	assert.Equal(t, int64(10737418240), rows[0].CapacityBytes)
	// Usage byte-seconds: 18000000000000 for a 3600s interval = 5000000000 bytes
	assert.Equal(t, int64(18000000000000), rows[0].UsageByteSeconds)
}

func TestParsePVCRows_MissingColumns(t *testing.T) {
	csv := `interval_start,namespace,persistentvolumeclaim
2026-05-01 00:00:00+00:00,ns1,pvc-1
`
	rows, err := ParsePVCRows(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Len(t, rows, 1)
	assert.Equal(t, "ns1", rows[0].Namespace)
	assert.Equal(t, "pvc-1", rows[0].PersistentVolumeClaim)
}

func TestParsePVCRows_EmptyPVCName(t *testing.T) {
	csv := `interval_start,namespace,persistentvolumeclaim
2026-05-01 00:00:00+00:00,ns1,
`
	rows, err := ParsePVCRows(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Len(t, rows, 0) // Empty PVC name is skipped
}

func TestParsePVCRows_RequiredColumnsMissing(t *testing.T) {
	csv := `some_column,another_column
val1,val2
`
	_, err := ParsePVCRows(strings.NewReader(csv))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing required columns")
}

func TestParsePVCRows_VMNameColumn(t *testing.T) {
	csv := `interval_start,interval_end,namespace,pod,vm_name,persistentvolumeclaim
2026-05-01 00:00:00+00:00,2026-05-01 01:00:00+00:00,kubevirt,virt-launcher-fedora-vm-x9y8z,fedora-vm,vm-disk
`
	rows, err := ParsePVCRows(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "fedora-vm", rows[0].VMName)
}

func TestComputePVCDigests_VMNamePropagation(t *testing.T) {
	rows := []PVCRow{
		{
			IntervalStart:         mustParseTime("2026-05-01 00:00:00+00:00"),
			IntervalEnd:           mustParseTime("2026-05-01 01:00:00+00:00"),
			Namespace:             "kubevirt",
			Pod:                   "virt-launcher-fedora-vm-x9y8z",
			VMName:                "fedora-vm",
			PersistentVolumeClaim: "vm-disk",
			CapacityBytes:         10 << 30,
			UsageByteSeconds:      3600 * 5e9,
		},
	}
	digests := ComputePVCDigests(rows)
	require.Len(t, digests, 1)
	assert.Equal(t, "fedora-vm", digests[0].VMName)
}

func TestParsePVCRows_NoVMNameColumn_VMNameEmpty(t *testing.T) {
	csv := `interval_start,interval_end,namespace,pod,persistentvolumeclaim
2026-05-01 00:00:00+00:00,2026-05-01 01:00:00+00:00,kubevirt,virt-launcher-fedora-vm-x9y8z,vm-disk
`
	rows, err := ParsePVCRows(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "", rows[0].VMName, "VMName must be empty when CSV has no vm_name column")
	assert.Equal(t, "virt-launcher-fedora-vm-x9y8z", rows[0].Pod)
}

func TestComputePVCDigests_NoVMName_DigestVMNameEmpty(t *testing.T) {
	rows := []PVCRow{
		{
			IntervalStart:         mustParseTime("2026-05-01 00:00:00+00:00"),
			IntervalEnd:           mustParseTime("2026-05-01 01:00:00+00:00"),
			Namespace:             "kubevirt",
			Pod:                   "virt-launcher-fedora-vm-x9y8z",
			PersistentVolumeClaim: "vm-disk",
			CapacityBytes:         10 << 30,
			UsageByteSeconds:      3600 * 5e9,
		},
	}
	digests := ComputePVCDigests(rows)
	require.Len(t, digests, 1)
	assert.Equal(t, "", digests[0].VMName, "VMName must be empty when PVCRow has no VMName set")
	assert.Equal(t, "virt-launcher-fedora-vm-x9y8z", digests[0].LastSeenPod)
}

func TestComputePVCDigests_BasicAggregation(t *testing.T) {
	rows := []PVCRow{
		{
			IntervalStart:         mustParseTime("2026-05-01 00:00:00+00:00"),
			IntervalEnd:           mustParseTime("2026-05-01 01:00:00+00:00"),
			Namespace:             "prod",
			PersistentVolumeClaim: "data-pvc",
			PersistentVolume:      "pv-1",
			StorageClass:          "gp3",
			CapacityBytes:         10 << 30,
			UsageByteSeconds:      3600 * 5e9, // 5 GiB for 1 hour
		},
		{
			IntervalStart:         mustParseTime("2026-05-01 01:00:00+00:00"),
			IntervalEnd:           mustParseTime("2026-05-01 02:00:00+00:00"),
			Namespace:             "prod",
			Pod:                   "virt-launcher-my-vm-abc12",
			PersistentVolumeClaim: "data-pvc",
			PersistentVolume:      "pv-1",
			StorageClass:          "gp3",
			CapacityBytes:         10 << 30,
			UsageByteSeconds:      3600 * 7e9, // 7 GiB for 1 hour
		},
	}

	digests := ComputePVCDigests(rows)
	require.Len(t, digests, 1)

	d := digests[0]
	assert.Equal(t, "prod", d.Namespace)
	assert.Equal(t, "data-pvc", d.PVC)
	assert.Equal(t, "virt-launcher-my-vm-abc12", d.LastSeenPod)
	assert.Equal(t, 2, d.SampleCount)
	// Min usage: 5 GiB, Max: 7 GiB
	assert.Equal(t, int64(5e9), d.UsageBytesMin)
	assert.Equal(t, int64(7e9), d.UsageBytesMax)
}

func TestComputePVCDigests_MultipleDays(t *testing.T) {
	rows := []PVCRow{
		{
			IntervalStart:         mustParseTime("2026-05-01 10:00:00+00:00"),
			IntervalEnd:           mustParseTime("2026-05-01 11:00:00+00:00"),
			Namespace:             "prod",
			PersistentVolumeClaim: "pvc-a",
			CapacityBytes:         10 << 30,
			UsageByteSeconds:      3600 * 1e9,
		},
		{
			IntervalStart:         mustParseTime("2026-05-02 10:00:00+00:00"),
			IntervalEnd:           mustParseTime("2026-05-02 11:00:00+00:00"),
			Namespace:             "prod",
			PersistentVolumeClaim: "pvc-a",
			CapacityBytes:         10 << 30,
			UsageByteSeconds:      3600 * 2e9,
		},
	}

	digests := ComputePVCDigests(rows)
	assert.Len(t, digests, 2) // One per day
}

func mustParseTime(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05+00:00", s)
	if err != nil {
		panic(err)
	}
	return t
}
