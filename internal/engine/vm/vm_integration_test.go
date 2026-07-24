package vm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/vm"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/vm"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// TestVMRecommendationPipeline_Integration exercises CSV parse → daily digests → GPU devices →
// recommendations → API DB reads → history append on re-run.
func TestVMRecommendationPipeline_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	orgID := "org-vm-pipeline-" + uuid.New().String()[:8]
	clusterUUID := uuid.MustParse(testutil.TestClusterUUID)

	_, err := pool.Exec(ctx,
		`INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		 VALUES (1, $1, 'vm-pipeline-test', 'src-vm', now()) ON CONFLICT DO NOTHING`,
		testutil.TestClusterUUID,
	)
	require.NoError(t, err)

	vmCSV := buildVMIntegrationUsageCSV()
	vmRows, err := ingestion.ParseVMCSVRows(strings.NewReader(vmCSV))
	require.NoError(t, err)
	require.NotEmpty(t, vmRows)

	digestMap := ingestion.BuildDailyVMDigests(vmRows)
	digests := make([]ingestion.VMDigestResult, 0, len(digestMap))
	for _, d := range digestMap {
		digests = append(digests, d)
	}
	require.NoError(t, ingestion.UpsertDailyVMDigests(ctx, pool, orgID, clusterUUID.String(), digests))

	gpuCSV := buildVMIntegrationGPUDeviceCSV()
	require.NoError(t, ingestion.IngestVMGPUDeviceCSV(ctx, pool, strings.NewReader(gpuCSV), orgID, clusterUUID.String()))

	cfg := vm.DefaultVMRecConfig()
	cfg.EnableInstanceTypeMatching = true
	require.NoError(t, vm.RunVMRecommendations(ctx, pool, orgID, clusterUUID, cfg))

	t.Run("vm.ListVMRecommendations returns seeded VMs with notifications", func(t *testing.T) {
		recs, total, err := vm.ListVMRecommendations(ctx, pool, orgID, vm.VMRecommendationFilters{
			Limit: 100,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, total, int64(6))

		byKey := map[string]model.VMRecommendation{}
		for _, r := range recs {
			if r.Term == "short_term" && r.Engine == "cost" {
				byKey[r.Namespace+"/"+r.VMName] = r
			}
		}

		assertContainsNotif(t, byKey["vm-notif/disk-grow-hypervisor-01"], vm.NotifVMDiskGrowingNoCapacity)
		assertContainsNotif(t, byKey["vm-notif/no-guest-agent-01"], vm.NotifVMNoGuestAgent)
		assertContainsNotif(t, byKey["vm-notif/high-io-vm-01"], vm.NotifVMHighIO)
		assertContainsNotif(t, byKey["vm-notif/disk-filling-guest-01"], vm.NotifVMDiskFillingGuest)
		assertContainsNotif(t, byKey["vm-notif/instance-type-rec-01"], vm.NotifVMInstanceTypeRec)
		assertContainsNotif(t, byKey["vm-notif/disk-critical-01"], vm.NotifVMDiskCritical)

		guestVM := byKey["vm-notif/guest-agent-vm-01"]
		require.NotEmpty(t, guestVM.VMName)
		assert.True(t, guestVM.GuestAgentDetected)
		assert.NotEqual(t, model.VMCategoryIdle, guestVM.Category)
	})

	t.Run("GetVMRecommendationDetail includes digests and GPU devices", func(t *testing.T) {
		rec, daily, err := vm.GetVMRecommendationDetail(
			ctx, pool, orgID, clusterUUID.String(),
			"gpu-idle-vm-01", "vm-notif", "short_term", "cost",
		)
		require.NoError(t, err)
		require.NotNil(t, rec)
		assert.Equal(t, "gpu-idle-vm-01", rec.VMName)
		require.NotEmpty(t, daily)
		assert.GreaterOrEqual(t, len(daily), 3)
		assertContainsNotif(t, *rec, vm.NotifVMGPUIdle)
	})

	t.Run("Re-run recommendations appends history", func(t *testing.T) {
		var before int64
		err := pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM vm_recommendation_history
			WHERE org_id = $1 AND cluster_id = $2 AND vm_name = $3 AND namespace = $4`,
			orgID, clusterUUID.String(), "guest-agent-vm-01", "vm-notif",
		).Scan(&before)
		require.NoError(t, err)

		time.Sleep(10 * time.Millisecond)
		require.NoError(t, vm.RunVMRecommendations(ctx, pool, orgID, clusterUUID, cfg))

		var after int64
		err = pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM vm_recommendation_history
			WHERE org_id = $1 AND cluster_id = $2 AND vm_name = $3 AND namespace = $4`,
			orgID, clusterUUID.String(), "guest-agent-vm-01", "vm-notif",
		).Scan(&after)
		require.NoError(t, err)
		assert.Greater(t, after, before)

		history, total, err := vm.ListVMRecommendationHistory(
			ctx, pool, orgID, clusterUUID.String(),
			"guest-agent-vm-01", "vm-notif", "short_term", "cost", 10, 0,
		)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, total, int64(1))
		assert.NotEmpty(t, history)
	})
}

func assertContainsNotif(t *testing.T, rec model.VMRecommendation, code int16) {
	t.Helper()
	require.NotEmpty(t, rec.VMName, "missing recommendation row for notification %d", code)
	codes := vmIntegrationNotifCodes(t, rec.Notifications)
	assert.Contains(t, codes, code, "vm %s/%s notifications %v", rec.Namespace, rec.VMName, codes)
}

func vmIntegrationNotifCodes(t *testing.T, raw []byte) []int16 {
	t.Helper()
	var notifs []vm.VMNotification
	require.NoError(t, json.Unmarshal(raw, &notifs))
	out := make([]int16, len(notifs))
	for i, n := range notifs {
		out[i] = n.Code
	}
	return out
}

func buildVMIntegrationUsageCSV() string {
	const ns = "vm-notif"
	base := testutil.RecentStart()
	days := 7
	intervalsPerDay := 24

	vmCSVHeader := strings.Join([]string{
		"interval_start", "interval_end", "vm_name", "namespace", "node_name", "guest_os",
		"cpu_usage_mc", "cpu_request_mc", "cpu_limit_mc",
		"memory_usage_kib", "memory_request_kib", "memory_available_kib",
		"disk_allocated_bytes", "filesystem_used_bytes", "filesystem_capacity_bytes",
		"disk_read_iops", "disk_write_iops", "disk_read_bytes_per_sec", "disk_write_bytes_per_sec",
		"restart_count",
		"gpu_count", "gpu_model", "gpu_utilization_avg", "gpu_utilization_max",
		"gpu_fb_used_avg_mib", "gpu_fb_used_max_mib",
		"gpu_sm_active_avg", "gpu_tensor_active_avg", "gpu_dram_active_avg",
		"gpu_mig_profile", "gpu_max_slices",
	}, ",")

	var b strings.Builder
	b.WriteString(vmCSVHeader)
	b.WriteString("\n")

	scenarios := []struct {
		vmName    string
		guestOS   string
		withAgent bool
		mutate    func(day int, row *vmCSVRow)
	}{
		{
			vmName: "guest-agent-vm-01", guestOS: "linux", withAgent: true,
			mutate: func(day int, row *vmCSVRow) {
				row.cpuUsageMC = 3000
				row.memUsageKiB = 2 * 1024 * 1024
				row.memAvailKiB = 6 * 1024 * 1024
				row.fsUsed = 40 * gib
				row.fsCap = 200 * gib
			},
		},
		{
			vmName: "disk-grow-hypervisor-01", guestOS: "linux", withAgent: false,
			mutate: func(day int, row *vmCSVRow) {
				row.cpuUsageMC = 3000
				row.memUsageKiB = 2 * 1024 * 1024
				row.diskAlloc = int64((100 + day*6) * gib)
			},
		},
		{
			vmName: "no-guest-agent-01", guestOS: "linux", withAgent: false,
			mutate: func(day int, row *vmCSVRow) {
				row.cpuUsageMC = 3000
				row.memUsageKiB = 4 * 1024 * 1024
			},
		},
		{
			vmName: "high-io-vm-01", guestOS: "linux", withAgent: true,
			mutate: func(day int, row *vmCSVRow) {
				row.cpuUsageMC = 3000
				row.memUsageKiB = 2 * 1024 * 1024
				row.memAvailKiB = 5 * 1024 * 1024
				row.diskReadIOPS = 6000
			},
		},
		{
			vmName: "disk-filling-guest-01", guestOS: "linux", withAgent: true,
			mutate: func(day int, row *vmCSVRow) {
				row.cpuUsageMC = 3000
				row.memUsageKiB = 2 * 1024 * 1024
				row.memAvailKiB = 2 * 1024 * 1024
				row.fsCap = 200 * gib
				row.fsUsed = int64((50 + day*8) * gib)
			},
		},
		{
			vmName: "instance-type-rec-01", guestOS: "linux", withAgent: true,
			mutate: func(day int, row *vmCSVRow) {
				row.cpuRequestMC = 8000
				row.cpuUsageMC = 1500
				row.memRequestKiB = 16 * 1024 * 1024
				row.memUsageKiB = 4 * 1024 * 1024
				row.memAvailKiB = 10 * 1024 * 1024
			},
		},
		{
			vmName: "disk-critical-01", guestOS: "linux", withAgent: true,
			mutate: func(day int, row *vmCSVRow) {
				row.cpuUsageMC = 3000
				row.memUsageKiB = 2 * 1024 * 1024
				row.memAvailKiB = 1 * 1024 * 1024
				row.fsCap = 100 * gib
				row.fsUsed = 96 * gib
			},
		},
		{
			vmName: "gpu-idle-vm-01", guestOS: "linux", withAgent: true,
			mutate: func(day int, row *vmCSVRow) {
				row.cpuUsageMC = 4000
				row.memUsageKiB = 8 * 1024 * 1024
				row.memAvailKiB = 40 * 1024 * 1024
				row.gpuCount = 1
				row.gpuModel = "NVIDIA T4"
				row.gpuUtilAvg = 0.01
			},
		},
	}

	for _, sc := range scenarios {
		for day := 0; day < days; day++ {
			for interval := 0; interval < intervalsPerDay; interval++ {
				start := base.AddDate(0, 0, day).Add(time.Duration(interval) * time.Hour)
				end := start.Add(15 * time.Minute)
				row := vmCSVRow{
					vmName: sc.vmName, namespace: ns, guestOS: sc.guestOS,
					cpuRequestMC: 4000, cpuLimitMC: 8000,
					memRequestKiB: 8 * 1024 * 1024,
					diskAlloc:     100 * gib,
				}
				if sc.withAgent {
					row.memAvailKiB = 4 * 1024 * 1024
				}
				if sc.mutate != nil {
					sc.mutate(day, &row)
				}
				appendVMCSVLine(&b, start, end, row)
			}
		}
	}
	return b.String()
}

func buildVMIntegrationGPUDeviceCSV() string {
	base := testutil.RecentStart().Add(12 * time.Hour)
	var b strings.Builder
	b.WriteString(strings.Join([]string{
		"interval_start", "namespace", "vm_name", "gpu_uuid", "gpu_model",
		"utilization_avg", "utilization_max", "fb_used_avg_mib", "fb_used_max_mib",
		"sm_active_avg", "tensor_active_avg", "dram_active_avg", "mig_profile", "max_slices",
	}, ","))
	b.WriteString("\n")
	for day := 0; day < 7; day++ {
		ts := base.AddDate(0, 0, day).Format(time.RFC3339)
		b.WriteString(fmt.Sprintf(
			"%s,vm-notif,gpu-idle-vm-01,GPU-INT-001,NVIDIA T4,0.01,0.02,512,768,0.01,0.01,0.01,,0\n",
			ts,
		))
	}
	return b.String()
}

const gib = 1024 * 1024 * 1024

type vmCSVRow struct {
	vmName, namespace, guestOS              string
	cpuUsageMC, cpuRequestMC, cpuLimitMC    int64
	memUsageKiB, memRequestKiB, memAvailKiB int64
	diskAlloc, fsUsed, fsCap                int64
	diskReadIOPS                            int64
	gpuCount                                int32
	gpuModel                                string
	gpuUtilAvg                              float64
}

func appendVMCSVLine(b *strings.Builder, start, end time.Time, r vmCSVRow) {
	avail := ""
	fsUsed := ""
	fsCap := ""
	if r.memAvailKiB > 0 {
		avail = fmt.Sprintf("%d", r.memAvailKiB)
		fsUsed = fmt.Sprintf("%d", r.fsUsed)
		fsCap = fmt.Sprintf("%d", r.fsCap)
	}
	readIOPS, writeIOPS := "100", "50"
	readBPS, writeBPS := "1024", "512"
	if r.diskReadIOPS > 0 {
		readIOPS = fmt.Sprintf("%d", r.diskReadIOPS)
		writeIOPS = "500"
		readBPS = "2000000"
		writeBPS = "1000000"
	}
	gpuCols := ",,,,,,,,,,,"
	if r.gpuCount > 0 {
		gpuCols = fmt.Sprintf(",%d,%s,%.4f,%.4f,512,768,0.01,0.01,0.01,,0",
			r.gpuCount, r.gpuModel, r.gpuUtilAvg, r.gpuUtilAvg+0.01)
	}
	b.WriteString(fmt.Sprintf(
		"%s,%s,%s,%s,%s,%s,%d,%d,%d,%d,%d,%s,%d,%s,%s,%s,%s,%s,%s,0%s\n",
		start.Format(time.RFC3339), end.Format(time.RFC3339),
		r.vmName, r.namespace, "worker-1", r.guestOS,
		r.cpuUsageMC, r.cpuRequestMC, r.cpuLimitMC,
		r.memUsageKiB, r.memRequestKiB, avail,
		r.diskAlloc, fsUsed, fsCap,
		readIOPS, writeIOPS, readBPS, writeBPS,
		gpuCols,
	))
}
