package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// csvTimeFmt is the container-CSV timestamp layout the flexible parser
// accepts first (shared by the SLO/api-tax fixtures).
const csvTimeFmt = "2006-01-02 15:04:05 +0000 UTC"

// #721: the advisory assembly over the multi-payload input — the primary
// (guest) payload carries the dual digests; the context (management)
// payload carries the topology with hcTier and the NodePool rows. The
// fixture shapes mirror the nise/parity templates.

// writeAdvisoryFixture writes a guest payload (container CSVs with both
// schedule streams) and a mgmt context payload (manifest.json topology +
// nodepool CSV), returning their directories.
func writeAdvisoryFixture(t *testing.T, tier string) (guestDir, mgmtDir string) {
	t.Helper()
	root := t.TempDir()
	guestDir = filepath.Join(root, "guest")
	mgmtDir = filepath.Join(root, "mgmt")
	require.NoError(t, os.MkdirAll(guestDir, 0o750))
	require.NoError(t, os.MkdirAll(mgmtDir, 0o750))

	// Guest: 14 days of container rows — the BH stream busy in hours, the
	// all-hours stream carrying the outside-BH usage. The CSV format
	// follows the container template: date,namespace,workload,container,
	// cpu_usage_p50_mc per schedule... the loader derives both streams
	// from one container CSV plus the schedule (the digest layer splits).
	// For the assembly test we exercise loadFiles end to end.
	// The real ROS container CSV shape (the parity/nise template format).
	hdr := "report_period_start,report_period_end,interval_start,interval_end,container_name,pod,owner_name,owner_kind,workload,workload_type,namespace,image_name,node,resource_id,node_capacity_cpu_cores,node_capacity_memory_bytes,node_capacity_pods,node_allocatable_cpu_cores,node_allocatable_memory_bytes,node_allocatable_gpu_count,instance_type,machineset_name,node_role,cpu_request_container_avg,cpu_request_container_sum,cpu_limit_container_avg,cpu_limit_container_sum,cpu_usage_container_avg,cpu_usage_container_min,cpu_usage_container_max,cpu_usage_container_sum,cpu_throttle_container_avg,cpu_throttle_container_max,cpu_throttle_container_min,cpu_throttle_container_sum,memory_request_container_avg,memory_request_container_sum,memory_limit_container_avg,memory_limit_container_sum,memory_usage_container_avg,memory_usage_container_min,memory_usage_container_max,memory_usage_container_sum,memory_rss_usage_container_avg,memory_rss_usage_container_min,memory_rss_usage_container_max,memory_rss_usage_container_sum,oom_count,restart_count,workload_pod_count,desired_replicas,available_replicas,accelerator_model_name,accelerator_profile_name,gpu_uuid,accelerator_frame_buffer_usage_min,accelerator_frame_buffer_usage_max,accelerator_frame_buffer_usage_avg,tensor_pipe_active_min,tensor_pipe_active_max,tensor_pipe_active_avg,dram_active_min,dram_active_max,dram_active_avg,sm_active_min,sm_active_max,sm_active_avg\n"
	var rows string
	for i := 0; i < 14; i++ {
		day := 29 - i
		rows += "2026-10-01 00:00:00 +0000 UTC,2026-11-01 00:00:00 +0000 UTC,2026-10-" + pad2(day) + " 09:00:00 +0000 UTC,2026-10-" + pad2(day) + " 17:00:00 +0000 UTC,c0,demo-app-pod,web,ReplicaSet,web,deployment,demo-app,quay.io/example/app:latest,worker-0,i-0,8,33554432000,250,7.5,32212254720,0,m5.xlarge,workers,worker,0.05,0.05,0.2,0.2,0.1,0.08,0.15,0.1,0,0,0,0,1073741824,1073741824,1127428915,1127428915,268435456,241591910,322122547,268435456,255013683,228170137,295279001,255013683,0,0,1,1,1,,,,,,,,,,,,,,,\n"

	}
	require.NoError(t, os.WriteFile(filepath.Join(guestDir, "ros-openshift-container-202610.csv"), []byte(hdr+rows), 0o600))

	// Guest manifest: the guest cluster identity.
	guestManifest := `{"cluster_id": "bbbbbbbb-1111-1111-1111-111111111111"}`
	require.NoError(t, os.WriteFile(filepath.Join(guestDir, "manifest.json"), []byte(guestManifest), 0o600))

	// Mgmt manifest: topology with hcTier for the guest's HC.
	mgmtManifest := `{"cluster_id": "cccccccc-1111-1111-1111-111111111111", "cr_status": {"topology": {"hostedControlPlaneNamespaces": ["hc-a-infra-hc-a"], "hcpSnapshot": [{"hcpNamespace": "hc-a-infra-hc-a", "hostedClusterID": "bbbbbbbb-1111-1111-1111-111111111111", "hcUID": "uid-hc-1", "hcpUID": "uid-hcp-1", "namespaceUID": "uid-ns-1", "observedAt": "2026-10-29T12:00:00Z", "complete": true, "hcTier": "` + tier + `"}]}}}`
	require.NoError(t, os.WriteFile(filepath.Join(mgmtDir, "manifest.json"), []byte(mgmtManifest), 0o600))

	// Mgmt nodepool CSV (ros-openshift-nodepool-YYYYMM.csv; the column
	// contract from librobne/csv/parse_nodepool.go).
	np := "hc_cluster_id,pool_name,window_start,window_end,spec_replicas,status_replicas,autoscaling,collected_at,source\n"
	np += "bbbbbbbb-1111-1111-1111-111111111111,workers,2026-10-29 11:00:00 +0000 UTC,2026-10-29 12:00:00 +0000 UTC," +
		"2,2" + ",,2026-10-29 12:00:00 +0000 UTC,hypershift\n"
	require.NoError(t, os.WriteFile(filepath.Join(mgmtDir, "ros-openshift-nodepool-202610.csv"), []byte(np), 0o600))
	return guestDir, mgmtDir
}

func pad2(n int) string {
	return fmt.Sprintf("%02d", n)
}

func TestComputeSleepAdvisory_AssemblyFires(t *testing.T) {
	guestDir, mgmtDir := writeAdvisoryFixture(t, "non-prod")
	cfgPath := filepath.Join(t.TempDir(), "adv.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`business_hours:
  enabled: true
  timezone: UTC
  days: [monday, tuesday, wednesday, thursday, friday, saturday, sunday]
  start_time: "09:00"
  end_time: "17:00"
`), 0o600))
	f := commonFlags{
		inputs:       []string{guestDir, mgmtDir},
		configPath:   cfgPath,
		noUserConfig: true,
		now:          "2026-10-30T00:00:00Z",
	}
	result, err := executeRecommend(f)
	require.NoError(t, err)
	require.NotEmpty(t, result.HCPAdvisories, "declared + BH signature must fire")
	adv := result.HCPAdvisories[0]
	assert.Equal(t, "schedule_nonprod_sleep", adv.Verdict)
	assert.Equal(t, "high", adv.Confidence)
	assert.Equal(t, "non-prod", adv.Details["declared_tier"])
	assert.Equal(t, 2, adv.Details["worker_node_count"])
}

func TestComputeSleepAdvisory_UnlabeledContextIsSilent(t *testing.T) {
	guestDir, mgmtDir := writeAdvisoryFixture(t, "")
	f := commonFlags{
		inputs:       []string{guestDir, mgmtDir},
		noUserConfig: true,
		now:          "2026-10-30T00:00:00Z",
	}
	result, err := executeRecommend(f)
	require.NoError(t, err)
	assert.Empty(t, result.HCPAdvisories, "no declaration, no advisory")
}

func TestComputeSleepAdvisory_NoContextPayloadIsSilent(t *testing.T) {
	guestDir, _ := writeAdvisoryFixture(t, "non-prod")
	f := commonFlags{
		inputs:       []string{guestDir},
		noUserConfig: true,
		now:          "2026-10-30T00:00:00Z",
	}
	result, err := executeRecommend(f)
	require.NoError(t, err)
	assert.Empty(t, result.HCPAdvisories, "no context payload, no advisory — never an error")
}

func TestComputeSleepAdvisory_JSONEnvelopeSibling(t *testing.T) {
	guestDir, mgmtDir := writeAdvisoryFixture(t, "non-prod")
	cfgPath := filepath.Join(t.TempDir(), "adv.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`business_hours:
  enabled: true
  timezone: UTC
  days: [monday, tuesday, wednesday, thursday, friday, saturday, sunday]
  start_time: "09:00"
  end_time: "17:00"
`), 0o600))
	f := commonFlags{
		inputs:       []string{guestDir, mgmtDir},
		configPath:   cfgPath,
		noUserConfig: true,
		now:          "2026-10-30T00:00:00Z",
	}
	result, err := executeRecommend(f)
	require.NoError(t, err)
	require.NotEmpty(t, result.HCPAdvisories)

	// The JSON envelope carries the sibling under hcp_advisories with the
	// #717 contract fields.
	var env struct {
		Version       int `json:"version"`
		HCPAdvisories []struct {
			HostedClusterID string         `json:"hosted_cluster_id"`
			Verdict         string         `json:"verdict"`
			Confidence      string         `json:"confidence"`
			Details         map[string]any `json:"details"`
			Evidence        []string       `json:"evidence"`
		} `json:"hcp_advisories"`
	}
	raw, err := json.Marshal(result.HCPAdvisories)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &env.HCPAdvisories))
	require.Len(t, env.HCPAdvisories, 1)
}

// #722: the zombie assembly over the multi-payload input — the primary
// (guest) payload carries the hosted digests (idle leg); the context
// (management) payload carries the association topology, the still-on
// digests, and the NodePool rows (short lane). Fixture shapes mirror the
// server's zombie_test.go topology.

// zombieContainerRow renders one container-CSV row (the header contract
// the loader parses; usage/request averages in cores).
func zombieContainerRow(day int, ns, wl string, usageAvg, reqAvg string) string {
	return "2026-10-01 00:00:00 +0000 UTC,2026-11-01 00:00:00 +0000 UTC,2026-10-" + pad2(day) + " 09:00:00 +0000 UTC,2026-10-" + pad2(day) + " 17:00:00 +0000 UTC,c0," + wl + "-pod,web,ReplicaSet,web,deployment," + ns + ",quay.io/example/app:latest,worker-0,i-0,8,33554432000,250,7.5,32212254720,0,m5.xlarge,workers,worker," + reqAvg + "," + reqAvg + ",0.2,0.2," + usageAvg + ",0.08,0.15,0.1,0,0,0,0,1073741824,1073741824,1127428915,1127428915,268435456,241591910,322122547,268435456,255013683,228170137,295279001,255013683,0,0,1,1,1,,,,,,,,,,,,,,,\n"
}

// writeZombieFixture writes a guest payload (container CSVs: idle user
// workload + busy platform rows) and a mgmt context payload (topology
// association + still-on digests + optional nodepool CSV). Days cover the
// trailing window before 2026-10-30 (the pinned now).
func writeZombieFixture(t *testing.T, days []int, userUsageAvg, mgmtReqAvg string, nodepool string) (guestDir, mgmtDir string) {
	t.Helper()
	root := t.TempDir()
	guestDir = filepath.Join(root, "guest")
	mgmtDir = filepath.Join(root, "mgmt")
	require.NoError(t, os.MkdirAll(guestDir, 0o750))
	require.NoError(t, os.MkdirAll(mgmtDir, 0o750))

	hdr := "report_period_start,report_period_end,interval_start,interval_end,container_name,pod,owner_name,owner_kind,workload,workload_type,namespace,image_name,node,resource_id,node_capacity_cpu_cores,node_capacity_memory_bytes,node_capacity_pods,node_allocatable_cpu_cores,node_allocatable_memory_bytes,node_allocatable_gpu_count,instance_type,machineset_name,node_role,cpu_request_container_avg,cpu_request_container_sum,cpu_limit_container_avg,cpu_limit_container_sum,cpu_usage_container_avg,cpu_usage_container_min,cpu_usage_container_max,cpu_usage_container_sum,cpu_throttle_container_avg,cpu_throttle_container_max,cpu_throttle_container_min,cpu_throttle_container_sum,memory_request_container_avg,memory_request_container_sum,memory_limit_container_avg,memory_limit_container_sum,memory_usage_container_avg,memory_usage_container_min,memory_usage_container_max,memory_usage_container_sum,memory_rss_usage_container_avg,memory_rss_usage_container_min,memory_rss_usage_container_max,memory_rss_usage_container_sum,oom_count,restart_count,workload_pod_count,desired_replicas,available_replicas,accelerator_model_name,accelerator_profile_name,gpu_uuid,accelerator_frame_buffer_usage_min,accelerator_frame_buffer_usage_max,accelerator_frame_buffer_usage_avg,tensor_pipe_active_min,tensor_pipe_active_max,tensor_pipe_active_avg,dram_active_min,dram_active_max,dram_active_avg,sm_active_min,sm_active_max,sm_active_avg\n"
	var rows string
	for _, d := range days {
		rows += zombieContainerRow(d, "demo-app", "web", userUsageAvg, "0.1")
		rows += zombieContainerRow(d, "kube-system", "coredns", "2.0", "2.0")
		rows += zombieContainerRow(d, "koku-metrics-operator", "collector", "5.0", "5.0")
	}
	require.NoError(t, os.WriteFile(filepath.Join(guestDir, "ros-openshift-container-202610.csv"), []byte(hdr+rows), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(guestDir, "manifest.json"), []byte(`{"cluster_id": "bbbbbbbb-1111-1111-1111-111111111111"}`), 0o600))

	// Mgmt: association topology (fresh complete snapshot naming the guest
	// HC) + still-on digests in the association namespace.
	mgmtManifest := `{"cluster_id": "cccccccc-1111-1111-1111-111111111111", "cr_status": {"topology": {"hostedControlPlaneNamespaces": ["clusters-hc9"], "hcpSnapshot": [{"hcpNamespace": "clusters-hc9", "hostedClusterID": "bbbbbbbb-1111-1111-1111-111111111111", "hcUID": "uid-hc-1", "hcpUID": "uid-hcp-1", "namespaceUID": "uid-ns-1", "observedAt": "2026-10-29T12:00:00Z", "complete": true}]}}}`
	require.NoError(t, os.WriteFile(filepath.Join(mgmtDir, "manifest.json"), []byte(mgmtManifest), 0o600))
	var mgmtRows string
	for _, d := range days {
		mgmtRows += zombieContainerRow(d, "clusters-hc9", "kube-apiserver", "0.05", mgmtReqAvg)
	}
	require.NoError(t, os.WriteFile(filepath.Join(mgmtDir, "ros-openshift-container-202610.csv"), []byte(hdr+mgmtRows), 0o600))
	if nodepool != "" {
		require.NoError(t, os.WriteFile(filepath.Join(mgmtDir, "ros-openshift-nodepool-202610.csv"), []byte(nodepool), 0o600))
	}
	return guestDir, mgmtDir
}

func zombieDays14() []int {
	return []int{16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29}
}

func TestComputeZombieAdvisory_AssemblyFires(t *testing.T) {
	guestDir, mgmtDir := writeZombieFixture(t, zombieDays14(), "0.001", "0.5", "")
	f := commonFlags{
		inputs:       []string{guestDir, mgmtDir},
		noUserConfig: true,
		now:          "2026-10-30T00:00:00Z",
	}
	result, err := executeRecommend(f)
	require.NoError(t, err)
	require.NotEmpty(t, result.HCPAdvisories, "idle user + provisioned CP must fire despite busy platform rows")
	adv := result.HCPAdvisories[0]
	assert.Equal(t, "review_unused_hosted_cluster", adv.Verdict)
	assert.Equal(t, "high", adv.Confidence)
	assert.Equal(t, "bbbbbbbb-1111-1111-1111-111111111111", adv.HostedClusterID)
	assert.Equal(t, int64(1), adv.Details["peak_user_cpu_mc"], "1mC user peak under the 10mC floor")
	assert.Equal(t, int64(10), adv.Details["idle_floor_cpu_mc"])
	assert.Equal(t, int64(500), adv.Details["control_plane_cpu_request_mc"])
	assert.NotContains(t, adv.Details, "api_max_daily_requests", "no SLO rows: api context unknown, field absent")
	require.NotEmpty(t, adv.Evidence)
	assert.Equal(t, "hc:bbbbbbbb-1111-1111-1111-111111111111", adv.Evidence[0])
}

func TestComputeZombieAdvisory_AliveGuestIsSilent(t *testing.T) {
	guestDir, mgmtDir := writeZombieFixture(t, zombieDays14(), "0.5", "0.5", "")
	f := commonFlags{
		inputs:       []string{guestDir, mgmtDir},
		noUserConfig: true,
		now:          "2026-10-30T00:00:00Z",
	}
	result, err := executeRecommend(f)
	require.NoError(t, err)
	assert.Empty(t, result.HCPAdvisories, "busy user workload vetoes the idle leg")
}

func TestComputeZombieAdvisory_ZeroCPIsSilent(t *testing.T) {
	guestDir, mgmtDir := writeZombieFixture(t, zombieDays14(), "0.001", "0.05", "")
	f := commonFlags{
		inputs:       []string{guestDir, mgmtDir},
		noUserConfig: true,
		now:          "2026-10-30T00:00:00Z",
	}
	result, err := executeRecommend(f)
	require.NoError(t, err)
	assert.Empty(t, result.HCPAdvisories, "CP requests present-but-below-floor is known-not-on: silence, not medium")
}

func TestComputeZombieAdvisory_NoContextPayloadIsSilent(t *testing.T) {
	guestDir, _ := writeZombieFixture(t, zombieDays14(), "0.001", "0.5", "")
	f := commonFlags{
		inputs:       []string{guestDir},
		noUserConfig: true,
		now:          "2026-10-30T00:00:00Z",
	}
	result, err := executeRecommend(f)
	require.NoError(t, err)
	assert.Empty(t, result.HCPAdvisories, "no context payload means no fresh snapshot: silence, never an error")
}

func TestComputeZombieAdvisory_ShortLaneFires(t *testing.T) {
	// 3 days of guest data only: the standard 14d lane is silent (missing
	// days read unknown); the pool-zero short lane fires over its window.
	np := "hc_cluster_id,pool_name,window_start,window_end,spec_replicas,status_replicas,autoscaling,collected_at,source\n"
	for _, d := range []int{27, 28, 29} {
		for h := 0; h < 4; h++ {
			np += "bbbbbbbb-1111-1111-1111-111111111111,workers,2026-10-" + pad2(d) + " " + pad2(h) + ":00:00 +0000 UTC,2026-10-" + pad2(d) + " " + pad2(h+1) + ":00:00 +0000 UTC,0,0,,2026-10-" + pad2(d) + " " + pad2(h+1) + ":00:00 +0000 UTC,hypershift\n"
		}
	}
	guestDir, mgmtDir := writeZombieFixture(t, []int{27, 28, 29}, "0.001", "0.5", np)
	f := commonFlags{
		inputs:       []string{guestDir, mgmtDir},
		noUserConfig: true,
		now:          "2026-10-30T00:00:00Z",
	}
	result, err := executeRecommend(f)
	require.NoError(t, err)
	require.NotEmpty(t, result.HCPAdvisories, "parked pools + idle short window must fire the short lane")
	adv := result.HCPAdvisories[0]
	assert.Equal(t, "review_unused_hosted_cluster", adv.Verdict)
	assert.Equal(t, "high", adv.Confidence)
	assert.Equal(t, 72.0, adv.WindowEnd.Sub(adv.WindowStart).Hours(), "short window spans 3 days")
	assert.Contains(t, adv.Evidence, "lane:short-3d")
	assert.Contains(t, adv.Evidence, "pool_zero_days:3")
}

// #723: the headroom assembly — the guest payload carries the SLO CSV
// (H leg) and the container CSVs (N leg via node recs); the mgmt context
// payload carries the association (window proof) and the CP digests
// (C leg). Pinned now 2026-10-30T00:00:00Z → fire window
// [2026-10-29T23:00, 2026-10-30T00:00).

// sloRowCSV renders one SLO CSV line.
func sloRowCSV(hcID string, collected time.Time, le string, count int64) string {
	return hcID + "," + collected.Add(-time.Hour).Format(csvTimeFmt) + "," + collected.Format(csvTimeFmt) +
		",mutating," + le + "," + fmt.Sprint(count) + "," + collected.Format(csvTimeFmt) + ",kubernetes\n"
}

func writeHeadroomFixture(t *testing.T, mgmtUsageAvg string, observedAt string) (guestDir, mgmtDir string) {
	t.Helper()
	root := t.TempDir()
	guestDir = filepath.Join(root, "guest")
	mgmtDir = filepath.Join(root, "mgmt")
	require.NoError(t, os.MkdirAll(guestDir, 0o750))
	require.NoError(t, os.MkdirAll(mgmtDir, 0o750))

	// Guest: container rows (node recs compute calm — no 12/74 codes) +
	// the SLO CSV with a hot p99 signature in the fire window.
	hdr := "report_period_start,report_period_end,interval_start,interval_end,container_name,pod,owner_name,owner_kind,workload,workload_type,namespace,image_name,node,resource_id,node_capacity_cpu_cores,node_capacity_memory_bytes,node_capacity_pods,node_allocatable_cpu_cores,node_allocatable_memory_bytes,node_allocatable_gpu_count,instance_type,machineset_name,node_role,cpu_request_container_avg,cpu_request_container_sum,cpu_limit_container_avg,cpu_limit_container_sum,cpu_usage_container_avg,cpu_usage_container_min,cpu_usage_container_max,cpu_usage_container_sum,cpu_throttle_container_avg,cpu_throttle_container_max,cpu_throttle_container_min,cpu_throttle_container_sum,memory_request_container_avg,memory_request_container_sum,memory_limit_container_avg,memory_limit_container_sum,memory_usage_container_avg,memory_usage_container_min,memory_usage_container_max,memory_usage_container_sum,memory_rss_usage_container_avg,memory_rss_usage_container_min,memory_rss_usage_container_max,memory_rss_usage_container_sum,oom_count,restart_count,workload_pod_count,desired_replicas,available_replicas,accelerator_model_name,accelerator_profile_name,gpu_uuid,accelerator_frame_buffer_usage_min,accelerator_frame_buffer_usage_max,accelerator_frame_buffer_usage_avg,tensor_pipe_active_min,tensor_pipe_active_max,tensor_pipe_active_avg,dram_active_min,dram_active_max,dram_active_avg,sm_active_min,sm_active_max,sm_active_avg\n"
	rows := ""
	for _, d := range []int{28, 29} {
		rows += zombieContainerRow(d, "demo-app", "web", "0.2", "0.3")
	}
	require.NoError(t, os.WriteFile(filepath.Join(guestDir, "ros-openshift-container-202610.csv"), []byte(hdr+rows), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(guestDir, "manifest.json"), []byte(`{"cluster_id": "bbbbbbbb-1111-1111-1111-111111111111"}`), 0o600))

	guestHC := "bbbbbbbb-1111-1111-1111-111111111111"
	slo := "hc_cluster_id,window_start,window_end,verb_group,le,bucket_count,collected_at,source\n"
	// Baseline snapshot 50m before the window start (the delta baseline).
	base := time.Date(2026, 10, 29, 22, 10, 0, 0, time.UTC)
	for _, le := range []string{"0.1", "0.5", "1.0", "2.0", "3.0", "+Inf"} {
		count := map[string]int64{"0.1": 5, "0.5": 6, "1.0": 7, "2.0": 8, "3.0": 9, "+Inf": 10}[le]
		slo += sloRowCSV(guestHC, base, le, count)
	}
	// Hot snapshot inside the window: mass concentrated at 3.0.
	hot := time.Date(2026, 10, 29, 23, 50, 0, 0, time.UTC)
	for _, le := range []string{"0.1", "0.5", "1.0", "2.0", "3.0", "+Inf"} {
		count := map[string]int64{"0.1": 6, "0.5": 7, "1.0": 8, "2.0": 9, "3.0": 1010, "+Inf": 1011}[le]
		slo += sloRowCSV(guestHC, hot, le, count)
	}
	require.NoError(t, os.WriteFile(filepath.Join(guestDir, "ros-openshift-slo-202610.csv"), []byte(slo), 0o600))

	// Mgmt: association proven FOR the window + saturated CP digests.
	mgmtManifest := `{"cluster_id": "cccccccc-1111-1111-1111-111111111111", "cr_status": {"topology": {"hostedControlPlaneNamespaces": ["clusters-hc9"], "hcpSnapshot": [{"hcpNamespace": "clusters-hc9", "hostedClusterID": "bbbbbbbb-1111-1111-1111-111111111111", "hcUID": "uid-hc-1", "hcpUID": "uid-hcp-1", "namespaceUID": "uid-ns-1", "observedAt": "` + observedAt + `", "complete": true}]}}}`
	require.NoError(t, os.WriteFile(filepath.Join(mgmtDir, "manifest.json"), []byte(mgmtManifest), 0o600))
	var mgmtRows string
	for _, d := range []int{28, 29} {
		mgmtRows += zombieContainerRow(d, "clusters-hc9", "kube-apiserver", mgmtUsageAvg, "1.0")
	}
	require.NoError(t, os.WriteFile(filepath.Join(mgmtDir, "ros-openshift-container-202610.csv"), []byte(hdr+mgmtRows), 0o600))
	return guestDir, mgmtDir
}

func TestComputeHeadroomAdvisory_AssemblyFires(t *testing.T) {
	guestDir, mgmtDir := writeHeadroomFixture(t, "0.9", "2026-10-29T23:30:00Z")
	f := commonFlags{
		inputs:       []string{guestDir, mgmtDir},
		noUserConfig: true,
		now:          "2026-10-30T00:00:00Z",
	}
	result, err := executeRecommend(f)
	require.NoError(t, err)
	require.NotEmpty(t, result.HCPAdvisories, "hot H + saturated C + calm N must fire")
	adv := result.HCPAdvisories[0]
	assert.Equal(t, "do_not_add_workers_first", adv.Verdict)
	assert.Equal(t, "high", adv.Confidence)
	assert.Equal(t, "bbbbbbbb-1111-1111-1111-111111111111", adv.HostedClusterID)
	p99 := adv.Details["hosted_api_p99_seconds"].(float64)
	assert.GreaterOrEqual(t, p99, 2.0)
	assert.Equal(t, 0.30, adv.Details["hosted_api_threshold_seconds"])
	ratio := adv.Details["control_plane_capacity_ratio"].(float64)
	assert.Greater(t, ratio, 0.80)
	// Hourly window.
	assert.Equal(t, 1.0, adv.WindowEnd.Sub(adv.WindowStart).Hours())
}

func TestComputeHeadroomAdvisory_CalmCPIsSilent(t *testing.T) {
	guestDir, mgmtDir := writeHeadroomFixture(t, "0.2", "2026-10-29T23:30:00Z")
	f := commonFlags{
		inputs:       []string{guestDir, mgmtDir},
		noUserConfig: true,
		now:          "2026-10-30T00:00:00Z",
	}
	result, err := executeRecommend(f)
	require.NoError(t, err)
	assert.Empty(t, result.HCPAdvisories, "calm C blames nothing")
}

func TestComputeHeadroomAdvisory_NoSLOIsSilent(t *testing.T) {
	guestDir, mgmtDir := writeHeadroomFixture(t, "0.9", "2026-10-29T23:30:00Z")
	require.NoError(t, os.Remove(filepath.Join(guestDir, "ros-openshift-slo-202610.csv")))
	f := commonFlags{
		inputs:       []string{guestDir, mgmtDir},
		noUserConfig: true,
		now:          "2026-10-30T00:00:00Z",
	}
	result, err := executeRecommend(f)
	require.NoError(t, err)
	assert.Empty(t, result.HCPAdvisories, "unknown H is silence")
}

func TestComputeHeadroomAdvisory_StaleSnapshotIsSilent(t *testing.T) {
	// Observed 5h before the window: the association is not proven FOR
	// this window (the skew tolerance is 5 minutes).
	guestDir, mgmtDir := writeHeadroomFixture(t, "0.9", "2026-10-29T18:00:00Z")
	f := commonFlags{
		inputs:       []string{guestDir, mgmtDir},
		noUserConfig: true,
		now:          "2026-10-30T00:00:00Z",
	}
	result, err := executeRecommend(f)
	require.NoError(t, err)
	assert.Empty(t, result.HCPAdvisories, "window proof fails: silence, never blame")
}

// The unknown-N assembly case (no node recs → silence, never calm) cannot
// be constructed end to end: any payload with a plugin CSV (required for
// the load to succeed) also feeds node digests, so node recs always
// compute. The semantics are pinned at the core level
// (TestHeadroom_VerdictMatrix_Silence, "unknown N is silence not calm").

// #724: the api-tax assembly — a single primary payload carrying an
// api-tax CSV with a storming webhook. No context payload needed (the
// verdict is cluster-scoped).

func TestComputeAPITaxAdvisory_AssemblyFires(t *testing.T) {
	root := t.TempDir()
	guestDir := filepath.Join(root, "guest")
	require.NoError(t, os.MkdirAll(guestDir, 0o750))

	hdr := "report_period_start,report_period_end,interval_start,interval_end,container_name,pod,owner_name,owner_kind,workload,workload_type,namespace,image_name,node,resource_id,node_capacity_cpu_cores,node_capacity_memory_bytes,node_capacity_pods,node_allocatable_cpu_cores,node_allocatable_memory_bytes,node_allocatable_gpu_count,instance_type,machineset_name,node_role,cpu_request_container_avg,cpu_request_container_sum,cpu_limit_container_avg,cpu_limit_container_sum,cpu_usage_container_avg,cpu_usage_container_min,cpu_usage_container_max,cpu_usage_container_sum,cpu_throttle_container_avg,cpu_throttle_container_max,cpu_throttle_container_min,cpu_throttle_container_sum,memory_request_container_avg,memory_request_container_sum,memory_limit_container_avg,memory_limit_container_sum,memory_usage_container_avg,memory_usage_container_min,memory_usage_container_max,memory_usage_container_sum,memory_rss_usage_container_avg,memory_rss_usage_container_min,memory_rss_usage_container_max,memory_rss_usage_container_sum,oom_count,restart_count,workload_pod_count,desired_replicas,available_replicas,accelerator_model_name,accelerator_profile_name,gpu_uuid,accelerator_frame_buffer_usage_min,accelerator_frame_buffer_usage_max,accelerator_frame_buffer_usage_avg,tensor_pipe_active_min,tensor_pipe_active_max,tensor_pipe_active_avg,dram_active_min,dram_active_max,dram_active_avg,sm_active_min,sm_active_max,sm_active_avg\n"
	rows := ""
	for _, d := range []int{28, 29} {
		rows += zombieContainerRow(d, "demo-app", "web", "0.2", "0.3")
	}
	require.NoError(t, os.WriteFile(filepath.Join(guestDir, "ros-openshift-container-202610.csv"), []byte(hdr+rows), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(guestDir, "manifest.json"), []byte(`{"cluster_id": "dddddddd-1111-1111-1111-111111111111"}`), 0o600))

	// API-tax CSV: two consecutive hourly cumulative snapshots around the
	// fire window [2026-10-29T23:00, 2026-10-30T00:00).
	tax := "webhook_name,window_start,window_end,le,bucket_count,total_count,rejected_count,collected_at,source\n"
	les := []string{"0.1", "0.5", "1.0", "2.0", "3.0", "10.0"}
	for i, ws := range []time.Time{
		time.Date(2026, 10, 29, 22, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 29, 23, 0, 0, 0, time.UTC),
	} {
		scale := 1.0 + 0.05*float64(i)
		collected := ws.Add(50 * time.Minute)
		cum := int64(0)
		for _, le := range les {
			d := int64(1)
			if le == "3.0" {
				d = int64(float64(1000) * scale)
			}
			cum += d
			total, rejected := "", ""
			if le == les[len(les)-1] {
				total = fmt.Sprint(int64(float64(5000) * scale))
				rejected = fmt.Sprint(int64(float64(10) * scale))
			}
			tax += "slow.mutating.example," + ws.Format(csvTimeFmt) + "," + ws.Add(time.Hour).Format(csvTimeFmt) +
				"," + le + "," + fmt.Sprint(cum) + "," + total + "," + rejected + "," + collected.Format(csvTimeFmt) + ",kubernetes\n"
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(guestDir, "ros-openshift-apitax-202610.csv"), []byte(tax), 0o600))

	f := commonFlags{
		inputs:       []string{guestDir},
		noUserConfig: true,
		now:          "2026-10-30T00:00:00Z",
	}
	result, err := executeRecommend(f)
	require.NoError(t, err)
	require.NotEmpty(t, result.HCPAdvisories, "p99 ~3s webhook must fire")
	adv := result.HCPAdvisories[0]
	assert.Equal(t, "tune_noisy_webhook", adv.Verdict)
	assert.Equal(t, "high", adv.Confidence)
	assert.Equal(t, "", adv.HostedClusterID, "shared-plane evidence names no single HC")
	assert.Equal(t, "slow.mutating.example", adv.Details["webhook"])
	p99 := adv.Details["webhook_p99_seconds"].(float64)
	assert.GreaterOrEqual(t, p99, 2.0)
	assert.Equal(t, 2.0, adv.Details["webhook_p99_threshold_seconds"])
}

func TestComputeAPITaxAdvisory_QuietClusterIsSilent(t *testing.T) {
	guestDir, _ := writeZombieFixture(t, zombieDays14(), "0.5", "0.5", "")
	f := commonFlags{
		inputs:       []string{guestDir},
		noUserConfig: true,
		now:          "2026-10-30T00:00:00Z",
	}
	result, err := executeRecommend(f)
	require.NoError(t, err)
	assert.Empty(t, result.HCPAdvisories, "no api-tax rows means silence, never an error")
}
