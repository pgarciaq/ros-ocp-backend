package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
