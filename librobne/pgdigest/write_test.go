package pgdigest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
	"github.com/redhatinsights/ros-ocp-backend/librobne/namespace"
	"github.com/redhatinsights/ros-ocp-backend/librobne/node"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pvc"
	"github.com/redhatinsights/ros-ocp-backend/librobne/quota"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/redhatinsights/ros-ocp-backend/librobne/vm"
)

func TestReadContainerDigests_RequiresIdentity(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	_, err := ReadContainerDigests(context.Background(), nil, "", "02059694-68ab-4d58-8809-de1e91f1d0e5", start, end)
	if err == nil || !strings.Contains(err.Error(), "org_id") {
		t.Fatalf("got %v, want org_id error", err)
	}
	_, err = ReadContainerDigests(context.Background(), nil, "1234567", "", start, end)
	if err == nil || !strings.Contains(err.Error(), "cluster") {
		t.Fatalf("got %v, want cluster error", err)
	}
	_, err = ReadContainerDigestsBySchedule(context.Background(), nil, "1234567", "02059694-68ab-4d58-8809-de1e91f1d0e5", start, end, "")
	if err == nil || !strings.Contains(err.Error(), "schedule_type") {
		t.Fatalf("got %v, want schedule_type error", err)
	}
}

func TestReadOtherEntityDigests_RequiresIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	cluster := "02059694-68ab-4d58-8809-de1e91f1d0e5"
	if _, err := ReadNamespaceDigests(ctx, nil, "", cluster, start, end); err == nil || !strings.Contains(err.Error(), "org_id") {
		t.Fatalf("namespace org: %v", err)
	}
	if _, err := ReadNamespaceDigests(ctx, nil, "1234567", "", start, end); err == nil || !strings.Contains(err.Error(), "cluster") {
		t.Fatalf("namespace cluster: %v", err)
	}
	if _, err := ReadNodeDigests(ctx, nil, "", cluster, start, end); err == nil || !strings.Contains(err.Error(), "org_id") {
		t.Fatalf("node org: %v", err)
	}
	if _, err := ReadNodeDigestsWithSchedule(ctx, nil, "1234567", cluster, start, end, ""); err == nil || !strings.Contains(err.Error(), "schedule_type") {
		t.Fatalf("node schedule_type: %v", err)
	}
	if _, err := ReadGPUContainerDigests(ctx, nil, "", start, end); err == nil || !strings.Contains(err.Error(), "cluster") {
		t.Fatalf("gpu cluster: %v", err)
	}
	if _, err := ReadPVCDigests(ctx, nil, "1234567", "", start, end); err == nil || !strings.Contains(err.Error(), "cluster") {
		t.Fatalf("pvc cluster: %v", err)
	}
	if _, err := ReadVMDigests(ctx, nil, "", cluster, start, end); err == nil || !strings.Contains(err.Error(), "org_id") {
		t.Fatalf("vm org: %v", err)
	}
	if _, err := ReadNamespaceQuotaDigests(ctx, nil, "", cluster, start, end); err == nil || !strings.Contains(err.Error(), "org_id") {
		t.Fatalf("quota org: %v", err)
	}
	if _, err := ReadClusterQuotaDigests(ctx, nil, "1234567", "", start, end); err == nil || !strings.Contains(err.Error(), "cluster") {
		t.Fatalf("crq cluster: %v", err)
	}
	if _, err := MaxAnyDigestDate(ctx, nil, "", cluster); err == nil || !strings.Contains(err.Error(), "org_id") {
		t.Fatalf("max any org: %v", err)
	}
}

func TestWriteContainerDigests_EmptyNoOp(t *testing.T) {
	t.Parallel()
	err := WriteContainerDigests(context.Background(), nil, "", "", nil)
	if err != nil {
		t.Fatalf("empty write: %v", err)
	}
}

func TestWriteOtherEntityDigests_EmptyNoOp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if err := WriteNamespaceDigests(ctx, nil, "", "", nil); err != nil {
		t.Fatalf("namespace: %v", err)
	}
	if err := WriteNodeDigests(ctx, nil, "", "", nil); err != nil {
		t.Fatalf("node: %v", err)
	}
	if err := WriteGPUContainerDigests(ctx, nil, "", nil); err != nil {
		t.Fatalf("gpu: %v", err)
	}
	if err := WritePVCDigests(ctx, nil, "", "", nil); err != nil {
		t.Fatalf("pvc: %v", err)
	}
	if err := WriteVMDigests(ctx, nil, "", "", nil); err != nil {
		t.Fatalf("vm: %v", err)
	}
	if err := WriteNamespaceQuotaDigests(ctx, nil, "", "", nil); err != nil {
		t.Fatalf("quota: %v", err)
	}
	if err := WriteClusterQuotaDigests(ctx, nil, "", "", nil); err != nil {
		t.Fatalf("cluster quota: %v", err)
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

func TestWriteOtherEntityDigests_RequiresIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cluster := "02059694-68ab-4d58-8809-de1e91f1d0e5"
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	ns := map[namespace.NamespaceKey][]types.DigestRow{
		{Namespace: "app"}: {{BucketDate: day}},
	}
	if err := WriteNamespaceDigests(ctx, nil, "", cluster, ns); err == nil || !strings.Contains(err.Error(), "org_id") {
		t.Fatalf("namespace org: %v", err)
	}
	if err := WriteNamespaceDigests(ctx, nil, "1234567", "", ns); err == nil || !strings.Contains(err.Error(), "cluster") {
		t.Fatalf("namespace cluster: %v", err)
	}
	nodes := []node.DigestRow{{BucketDate: day, Node: "worker-1"}}
	if err := WriteNodeDigests(ctx, nil, "", cluster, nodes); err == nil || !strings.Contains(err.Error(), "org_id") {
		t.Fatalf("node org: %v", err)
	}
	if err := WriteNodeDigestsWithSchedule(ctx, nil, "1234567", cluster, "", nodes); err == nil || !strings.Contains(err.Error(), "schedule_type") {
		t.Fatalf("node schedule_type: %v", err)
	}
	gpus := map[gpu.GPUContainerKey][]gpu.GPUDigestRow{
		{Namespace: "ml", Workload: "train", ContainerName: "gpu"}: {{IntervalStart: day, GPUModelName: "A100"}},
	}
	if err := WriteGPUContainerDigests(ctx, nil, "", gpus); err == nil || !strings.Contains(err.Error(), "cluster") {
		t.Fatalf("gpu cluster: %v", err)
	}
	pvcs := map[pvc.PVCKey][]pvc.PVCDigestRow{
		{Namespace: "app", PVC: "data"}: {{BucketDate: day, Namespace: "app", PVC: "data"}},
	}
	if err := WritePVCDigests(ctx, nil, "1234567", "", pvcs); err == nil || !strings.Contains(err.Error(), "cluster") {
		t.Fatalf("pvc cluster: %v", err)
	}
	vms := []vm.DailyVMDigest{{VMName: "web", Namespace: "vms", BucketDate: day}}
	if err := WriteVMDigests(ctx, nil, "", cluster, vms); err == nil || !strings.Contains(err.Error(), "org_id") {
		t.Fatalf("vm org: %v", err)
	}
	quotaRows := []quota.NamespaceQuotaSnapshot{{Namespace: "app", QuotaName: "compute", LastObservedAt: day}}
	if err := WriteNamespaceQuotaDigests(ctx, nil, "", cluster, quotaRows); err == nil || !strings.Contains(err.Error(), "org_id") {
		t.Fatalf("quota org: %v", err)
	}
	crq := []quota.ClusterQuotaSnapshot{{ClusterQuotaName: "team-a", LastObservedAt: day}}
	if err := WriteClusterQuotaDigests(ctx, nil, "1234567", "", crq); err == nil || !strings.Contains(err.Error(), "cluster") {
		t.Fatalf("crq cluster: %v", err)
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
	day := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	if got := partitionChildName("daily_namespace_digests", day); got != "daily_namespace_digests_202608" {
		t.Fatalf("namespace partition = %q", got)
	}
	if got := partitionChildName("daily_node_digests", day); got != "daily_node_digests_202608" {
		t.Fatalf("node partition = %q", got)
	}
	if got := partitionChildName("gpu_container_digests", day); got != "gpu_container_digests_202608" {
		t.Fatalf("gpu partition = %q", got)
	}
	if got := partitionChildName("daily_pvc_digests", day); got != "daily_pvc_digests_202608" {
		t.Fatalf("pvc partition = %q", got)
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
