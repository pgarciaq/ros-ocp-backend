package pgdigest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

func TestWriteContainerDigests_EmptyNoOp(t *testing.T) {
	t.Parallel()
	err := WriteContainerDigests(context.Background(), nil, "", "", nil)
	if err != nil {
		t.Fatalf("empty write: %v", err)
	}
}

func TestWriteContainerDigests_RequiresIdentity(t *testing.T) {
	t.Parallel()
	digests := []types.KeyedDigest{sampleKeyedDigest()}
	err := WriteContainerDigests(context.Background(), nil, "", "02059694-68ab-4d58-8809-de1e91f1d0e5", digests)
	if err == nil || !strings.Contains(err.Error(), "org_id") {
		t.Fatalf("got %v, want org_id error", err)
	}
	err = WriteContainerDigests(context.Background(), nil, "1234567", "", digests)
	if err == nil || !strings.Contains(err.Error(), "cluster") {
		t.Fatalf("got %v, want cluster error", err)
	}
}

func TestWriteRows_RequiresScheduleType(t *testing.T) {
	t.Parallel()
	err := WriteRows(context.Background(), nil, []Row{{
		OrgID:        "1234567",
		ClusterUUID:  "02059694-68ab-4d58-8809-de1e91f1d0e5",
		ScheduleType: "",
		Digest:       sampleKeyedDigest(),
	}})
	if err == nil || !strings.Contains(err.Error(), "schedule") {
		t.Fatalf("got %v, want schedule error", err)
	}
}

func TestSortRows_UniqueIndexOrder(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	row := func(ns, sched string) Row {
		return Row{
			OrgID:        "1",
			ClusterUUID:  "c",
			ScheduleType: sched,
			Digest: types.KeyedDigest{
				Key: types.ContainerKey{Namespace: ns, Workload: "w", WorkloadType: "deployment", ContainerName: "c"},
				Row: types.DigestRow{BucketDate: day},
			},
		}
	}
	rows := []Row{row("b", "business_hours"), row("a", "business_hours"), row("a", "all_hours")}
	sortRows(rows)
	if rows[0].Digest.Key.Namespace != "a" || rows[0].ScheduleType != "all_hours" {
		t.Fatalf("first = %+v, want ns=a all_hours", rows[0])
	}
	if rows[1].Digest.Key.Namespace != "a" || rows[1].ScheduleType != "business_hours" {
		t.Fatalf("second = %+v, want ns=a business_hours", rows[1])
	}
	if rows[2].Digest.Key.Namespace != "b" {
		t.Fatalf("third = %+v, want ns=b", rows[2])
	}
}

func TestPartitionName(t *testing.T) {
	t.Parallel()
	got := partitionName(time.Date(2026, 8, 16, 15, 4, 0, 0, time.UTC))
	if got != "daily_container_digests_202608" {
		t.Fatalf("partitionName = %q", got)
	}
	got = partitionName(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if got != "daily_container_digests_202608" {
		t.Fatalf("first-of-month partitionName = %q", got)
	}
}

func sampleKeyedDigest() types.KeyedDigest {
	return types.KeyedDigest{
		Key: types.ContainerKey{
			Namespace:     "app",
			Workload:      "api",
			WorkloadType:  "deployment",
			ContainerName: "api",
		},
		Row: types.DigestRow{
			BucketDate:      time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			CPUUsageP95MC:   100,
			SampleCount:     24,
			MemUsageP95KiB:  1024,
			CPURequestP50MC: 50,
		},
	}
}
