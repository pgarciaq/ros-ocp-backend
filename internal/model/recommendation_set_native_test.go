package model

import (
	"testing"
	"time"
)

func TestNativeNamespaceID_Deterministic(t *testing.T) {
	id1 := NativeNamespaceID("cluster-uuid-1", "kube-system")
	id2 := NativeNamespaceID("cluster-uuid-1", "kube-system")
	if id1 != id2 {
		t.Errorf("NativeNamespaceID should be deterministic: %s != %s", id1, id2)
	}
}

func TestNativeNamespaceID_DifferentInputsDifferentIDs(t *testing.T) {
	id1 := NativeNamespaceID("cluster-uuid-1", "kube-system")
	id2 := NativeNamespaceID("cluster-uuid-1", "default")
	id3 := NativeNamespaceID("cluster-uuid-2", "kube-system")

	if id1 == id2 {
		t.Error("different namespaces should produce different IDs")
	}
	if id1 == id3 {
		t.Error("different clusters should produce different IDs")
	}
}

func TestNativeContainerID_Deterministic(t *testing.T) {
	id1 := NativeContainerID("cluster-1", "ns", "deploy", "Deployment", "container")
	id2 := NativeContainerID("cluster-1", "ns", "deploy", "Deployment", "container")
	if id1 != id2 {
		t.Errorf("NativeContainerID should be deterministic: %s != %s", id1, id2)
	}
}

func TestNativeContainerID_DiffersAcrossWorkloadTypes(t *testing.T) {
	id1 := NativeContainerID("cluster-1", "ns", "api", "Deployment", "main")
	id2 := NativeContainerID("cluster-1", "ns", "api", "StatefulSet", "main")
	if id1 == id2 {
		t.Error("same workload name with different types should produce different IDs")
	}
}

func TestNativeNamespaceID_DiffersFromContainerID(t *testing.T) {
	nsID := NativeNamespaceID("cluster-1", "default")
	containerID := NativeContainerID("cluster-1", "default", "", "", "")
	if nsID == containerID {
		t.Error("namespace and container IDs should differ even for same cluster+namespace")
	}
}

func TestNativeNamespaceID_IsValidUUID(t *testing.T) {
	id := NativeNamespaceID("cluster-uuid", "test-namespace")
	if len(id) != 36 {
		t.Errorf("expected UUID length 36, got %d: %s", len(id), id)
	}
	// UUID format: 8-4-4-4-12
	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Errorf("invalid UUID format: %s", id)
	}
}

func TestSmallintArray_Scan_Nil(t *testing.T) {
	var a SmallintArray
	if err := a.Scan(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a != nil {
		t.Errorf("expected nil, got %v", a)
	}
}

func TestSmallintArray_Scan_String(t *testing.T) {
	var a SmallintArray
	if err := a.Scan("{1,2,7}"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(a))
	}
	if a[0] != 1 || a[1] != 2 || a[2] != 7 {
		t.Errorf("expected [1,2,7], got %v", a)
	}
}

func TestSmallintArray_Scan_Bytes(t *testing.T) {
	var a SmallintArray
	if err := a.Scan([]byte("{10,20}")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a) != 2 || a[0] != 10 || a[1] != 20 {
		t.Errorf("expected [10,20], got %v", a)
	}
}

func TestSmallintArray_Scan_EmptyBraces(t *testing.T) {
	var a SmallintArray
	if err := a.Scan("{}"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a != nil {
		t.Errorf("expected nil for empty braces, got %v", a)
	}
}

func TestSmallintArray_Scan_InvalidType(t *testing.T) {
	var a SmallintArray
	if err := a.Scan(42); err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestSmallintArray_Scan_InvalidNumber(t *testing.T) {
	var a SmallintArray
	if err := a.Scan("{1,abc,3}"); err == nil {
		t.Fatal("expected error for invalid number")
	}
}

func TestSmallintArray_Value_Nil(t *testing.T) {
	var a SmallintArray
	val, err := a.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil value, got %v", val)
	}
}

func TestSmallintArray_Value_NonEmpty(t *testing.T) {
	a := SmallintArray{1, 7, 24}
	val, err := a.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := val.(string)
	if !ok {
		t.Fatalf("expected string value, got %T", val)
	}
	if s != "{1,7,24}" {
		t.Errorf("expected {1,7,24}, got %s", s)
	}
}

func TestSmallintArray_RoundTrip(t *testing.T) {
	original := SmallintArray{3, 5, 9}
	val, err := original.Value()
	if err != nil {
		t.Fatalf("Value() error: %v", err)
	}

	var restored SmallintArray
	if err := restored.Scan(val); err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	if len(restored) != len(original) {
		t.Fatalf("expected %d elements, got %d", len(original), len(restored))
	}
	for i := range original {
		if original[i] != restored[i] {
			t.Errorf("mismatch at index %d: %d != %d", i, original[i], restored[i])
		}
	}
}

func TestNativeContainerSortExpr_LastReported(t *testing.T) {
	expr, filter := nativeContainerSortExpr("clusters.last_reported_at")
	if expr != "c.last_reported_at" {
		t.Errorf("sort expr = %q, want c.last_reported_at", expr)
	}
	if filter != "" {
		t.Errorf("filter = %q, want empty", filter)
	}
}

func TestNativeContainerSortExpr_Variation(t *testing.T) {
	expr, filter := nativeContainerSortExpr("recommendation_sets.cpu_variation_short_cost_pct")
	if expr != "rs.variation_cpu_request_pct" {
		t.Errorf("sort expr = %q, want rs.variation_cpu_request_pct", expr)
	}
	wantFilter := "rs.term = 'short_term' AND rs.engine = 'cost'"
	if filter != wantFilter {
		t.Errorf("filter = %q, want %q", filter, wantFilter)
	}
}

func TestAssembleNativeResults_PreservesRowOrder(t *testing.T) {
	older := time.Date(2026, 5, 1, 13, 17, 17, 0, time.UTC)
	newer := time.Date(2026, 5, 1, 14, 17, 16, 0, time.UTC)
	rows := []NativeRecommendationRow{
		{ClusterUUID: "c-new", Namespace: "ns-b", Workload: "w", WorkloadType: "deployment", ContainerName: "app", Term: "short_term", Engine: "cost", LastReported: newer},
		{ClusterUUID: "c-old", Namespace: "ns-a", Workload: "w", WorkloadType: "deployment", ContainerName: "app", Term: "short_term", Engine: "cost", LastReported: older},
	}
	results := assembleNativeResults(rows, "", false)
	if len(results) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(results))
	}
	if results[0].ClusterUUID != "c-new" || results[1].ClusterUUID != "c-old" {
		t.Errorf("order not preserved: got %s then %s", results[0].ClusterUUID, results[1].ClusterUUID)
	}
}

func TestAssembleNativeResults_WithPodCounts(t *testing.T) {
	now := time.Now().UTC()
	cpuReq := int64(500)
	pcMin := 2
	pcMax := 5
	pcAvg := 3

	rows := []NativeRecommendationRow{
		{
			ClusterUUID:     "cluster-1",
			Namespace:       "ns",
			Workload:        "deploy",
			WorkloadType:    "deployment",
			ContainerName:   "main",
			Term:            "short",
			Engine:          "cost",
			RecCPURequestMC: &cpuReq,
			PodCountMin:     &pcMin,
			PodCountMax:     &pcMax,
			PodCountAvg:     &pcAvg,
			UpdatedAt:       now,
			LastReported:    now,
		},
	}

	results := assembleNativeResults(rows, "", false)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Replicas == nil {
		t.Fatal("expected replicas to be populated")
	}
	if results[0].Replicas.Min != 2 {
		t.Errorf("expected min=2, got %d", results[0].Replicas.Min)
	}
	if results[0].Replicas.Max != 5 {
		t.Errorf("expected max=5, got %d", results[0].Replicas.Max)
	}
	if results[0].Replicas.Avg != 3 {
		t.Errorf("expected avg=3, got %d", results[0].Replicas.Avg)
	}
}

func TestAssembleNativeResults_NilPodCounts(t *testing.T) {
	now := time.Now().UTC()
	cpuReq := int64(500)

	rows := []NativeRecommendationRow{
		{
			ClusterUUID:     "cluster-1",
			Namespace:       "ns",
			Workload:        "deploy",
			WorkloadType:    "deployment",
			ContainerName:   "main",
			Term:            "short",
			Engine:          "cost",
			RecCPURequestMC: &cpuReq,
			UpdatedAt:       now,
			LastReported:    now,
		},
	}

	results := assembleNativeResults(rows, "", false)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Replicas != nil {
		t.Error("expected nil replicas when pod count columns are nil")
	}
}

func TestAssembleNativeResults_ZeroPodCountMax(t *testing.T) {
	now := time.Now().UTC()
	cpuReq := int64(500)
	zero := 0

	rows := []NativeRecommendationRow{
		{
			ClusterUUID:     "cluster-1",
			Namespace:       "ns",
			Workload:        "deploy",
			WorkloadType:    "deployment",
			ContainerName:   "main",
			Term:            "short",
			Engine:          "cost",
			RecCPURequestMC: &cpuReq,
			PodCountMin:     &zero,
			PodCountMax:     &zero,
			PodCountAvg:     &zero,
			UpdatedAt:       now,
			LastReported:    now,
		},
	}

	results := assembleNativeResults(rows, "", false)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Replicas != nil {
		t.Error("expected nil replicas when pod_count_max is 0")
	}
}

func TestAssembleNativeResults_SourceFieldKubeStateMetrics(t *testing.T) {
	now := time.Now().UTC()
	cpuReq := int64(500)
	pcMin := 2
	pcMax := 5
	pcAvg := 3
	desired := 5
	available := 4

	rows := []NativeRecommendationRow{
		{
			ClusterUUID:       "cluster-1",
			Namespace:         "ns",
			Workload:          "deploy",
			WorkloadType:      "deployment",
			ContainerName:     "main",
			Term:              "short",
			Engine:            "cost",
			RecCPURequestMC:   &cpuReq,
			PodCountMin:       &pcMin,
			PodCountMax:       &pcMax,
			PodCountAvg:       &pcAvg,
			DesiredReplicas:   &desired,
			AvailableReplicas: &available,
			UpdatedAt:         now,
			LastReported:      now,
		},
	}

	results := assembleNativeResults(rows, "", false)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Replicas == nil {
		t.Fatal("expected replicas to be populated")
	}
	if results[0].Replicas.Source != "kube_state_metrics" {
		t.Errorf("expected source=kube_state_metrics, got %q", results[0].Replicas.Source)
	}
	if results[0].Replicas.Desired != 5 {
		t.Errorf("expected desired=5, got %d", results[0].Replicas.Desired)
	}
	if results[0].Replicas.Available != 4 {
		t.Errorf("expected available=4, got %d", results[0].Replicas.Available)
	}
}

func TestAssembleNativeResults_SourceFieldDerived(t *testing.T) {
	now := time.Now().UTC()
	cpuReq := int64(500)
	pcMin := 2
	pcMax := 5
	pcAvg := 3

	rows := []NativeRecommendationRow{
		{
			ClusterUUID:     "cluster-1",
			Namespace:       "ns",
			Workload:        "deploy",
			WorkloadType:    "deployment",
			ContainerName:   "main",
			Term:            "short",
			Engine:          "cost",
			RecCPURequestMC: &cpuReq,
			PodCountMin:     &pcMin,
			PodCountMax:     &pcMax,
			PodCountAvg:     &pcAvg,
			UpdatedAt:       now,
			LastReported:    now,
		},
	}

	results := assembleNativeResults(rows, "", false)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Replicas == nil {
		t.Fatal("expected replicas to be populated")
	}
	if results[0].Replicas.Source != "derived" {
		t.Errorf("expected source=derived, got %q", results[0].Replicas.Source)
	}
	if results[0].Replicas.Desired != 0 {
		t.Errorf("expected desired=0 (omitted), got %d", results[0].Replicas.Desired)
	}
	if results[0].Replicas.Available != 0 {
		t.Errorf("expected available=0 (omitted), got %d", results[0].Replicas.Available)
	}
}

func TestAssembleNativeResults_SourceFieldDerived_ZeroDesired(t *testing.T) {
	now := time.Now().UTC()
	cpuReq := int64(500)
	pcMin := 2
	pcMax := 5
	pcAvg := 3
	zero := 0

	rows := []NativeRecommendationRow{
		{
			ClusterUUID:       "cluster-1",
			Namespace:         "ns",
			Workload:          "deploy",
			WorkloadType:      "deployment",
			ContainerName:     "main",
			Term:              "short",
			Engine:            "cost",
			RecCPURequestMC:   &cpuReq,
			PodCountMin:       &pcMin,
			PodCountMax:       &pcMax,
			PodCountAvg:       &pcAvg,
			DesiredReplicas:   &zero,
			AvailableReplicas: &zero,
			UpdatedAt:         now,
			LastReported:      now,
		},
	}

	results := assembleNativeResults(rows, "", false)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Replicas == nil {
		t.Fatal("expected replicas to be populated")
	}
	if results[0].Replicas.Source != "derived" {
		t.Errorf("expected source=derived when desired_replicas=0, got %q", results[0].Replicas.Source)
	}
}

func TestDerefInt(t *testing.T) {
	v := 42
	if derefInt(&v) != 42 {
		t.Error("derefInt(&42) should return 42")
	}
	if derefInt(nil) != 0 {
		t.Error("derefInt(nil) should return 0")
	}
}

func TestAssembleNativeNamespaceResults_Empty(t *testing.T) {
	results := assembleNativeNamespaceResults(nil, "", false)
	if len(results) != 0 {
		t.Errorf("expected 0 results for nil input, got %d", len(results))
	}
}

func TestAssembleNativeNamespaceResults_GroupsCorrectly(t *testing.T) {
	now := time.Now().UTC()
	cpuReq := int64(500)
	cpuLim := int64(1000)
	memReq := int64(4096)
	memLim := int64(8192)
	conf := float32(0.85)

	rows := []NativeNamespaceRow{
		{
			ClusterUUID:      "cluster-1",
			NamespaceName:    "ns-a",
			Term:             "short",
			Engine:           "cost",
			RecCPURequestMC:  &cpuReq,
			RecCPULimitMC:    &cpuLim,
			RecMemRequestKiB: &memReq,
			RecMemLimitKiB:   &memLim,
			ConfidenceLevel:  &conf,
			UpdatedAt:        now,
			ClusterAlias:     "my-cluster",
			SourceID:         "src-1",
			LastReported:     now,
		},
		{
			ClusterUUID:      "cluster-1",
			NamespaceName:    "ns-a",
			Term:             "short",
			Engine:           "performance",
			RecCPURequestMC:  &cpuReq,
			RecCPULimitMC:    &cpuLim,
			RecMemRequestKiB: &memReq,
			RecMemLimitKiB:   &memLim,
			ConfidenceLevel:  &conf,
			UpdatedAt:        now,
			ClusterAlias:     "my-cluster",
			SourceID:         "src-1",
			LastReported:     now,
		},
		{
			ClusterUUID:      "cluster-1",
			NamespaceName:    "ns-b",
			Term:             "short",
			Engine:           "cost",
			RecCPURequestMC:  &cpuReq,
			RecCPULimitMC:    &cpuLim,
			RecMemRequestKiB: &memReq,
			RecMemLimitKiB:   &memLim,
			ConfidenceLevel:  &conf,
			UpdatedAt:        now,
			ClusterAlias:     "my-cluster",
			SourceID:         "src-1",
			LastReported:     now,
		},
	}

	results := assembleNativeNamespaceResults(rows, "", false)

	if len(results) != 2 {
		t.Fatalf("expected 2 results (2 namespaces), got %d", len(results))
	}

	// First result should be ns-a with both cost and performance.
	if results[0].Project != "ns-a" {
		t.Errorf("expected first result project=ns-a, got %s", results[0].Project)
	}
	if results[0].ClusterAlias != "my-cluster" {
		t.Errorf("expected cluster alias my-cluster, got %s", results[0].ClusterAlias)
	}

	termRecAny, ok := results[0].Recommendations["short_term"]
	if !ok {
		t.Fatal("expected short_term recommendation for ns-a")
	}
	termRec, ok := termRecAny.(TermRecommendation)
	if !ok {
		t.Fatalf("expected TermRecommendation, got %T", termRecAny)
	}
	if termRec.Cost == nil {
		t.Error("expected cost recommendation for ns-a short_term")
	}
	if termRec.Performance == nil {
		t.Error("expected performance recommendation for ns-a short_term")
	}

	// Second result should be ns-b with only cost.
	if results[1].Project != "ns-b" {
		t.Errorf("expected second result project=ns-b, got %s", results[1].Project)
	}
	termRecBAny, ok := results[1].Recommendations["short_term"]
	if !ok {
		t.Fatal("expected short_term recommendation for ns-b")
	}
	termRecB, ok := termRecBAny.(TermRecommendation)
	if !ok {
		t.Fatalf("expected TermRecommendation, got %T", termRecBAny)
	}
	if termRecB.Cost == nil {
		t.Error("expected cost recommendation for ns-b short_term")
	}
	if termRecB.Performance != nil {
		t.Error("expected no performance recommendation for ns-b short_term")
	}
}

func TestAssembleNativeNamespaceResults_IDGeneration(t *testing.T) {
	now := time.Now().UTC()
	cpuReq := int64(100)
	rows := []NativeNamespaceRow{
		{
			ClusterUUID:     "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			NamespaceName:   "test-namespace",
			Term:            "short",
			Engine:          "cost",
			RecCPURequestMC: &cpuReq,
			UpdatedAt:       now,
			LastReported:    now,
		},
	}

	results := assembleNativeNamespaceResults(rows, "", false)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	expectedID := NativeNamespaceID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "test-namespace")
	if results[0].ID != expectedID {
		t.Errorf("expected ID %s, got %s", expectedID, results[0].ID)
	}
}

func TestAssembleNativeNamespaceResults_MonitoringEndTime(t *testing.T) {
	now := time.Now().UTC()
	cpuReq := int64(100)
	rows := []NativeNamespaceRow{
		{
			ClusterUUID:       "cluster-1",
			NamespaceName:     "ns-a",
			Term:              "short",
			Engine:            "cost",
			RecCPURequestMC:   &cpuReq,
			MonitoringEndTime: &now,
			UpdatedAt:         now,
			LastReported:      now,
		},
	}

	results := assembleNativeNamespaceResults(rows, "", false)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	metAny, ok := results[0].Recommendations["monitoring_end_time"]
	if !ok {
		t.Fatal("expected monitoring_end_time in recommendations")
	}
	metStr, ok := metAny.(string)
	if !ok {
		t.Fatalf("expected string monitoring_end_time, got %T", metAny)
	}
	if metStr == "" {
		t.Error("monitoring_end_time should not be empty")
	}
}

func TestAssembleNativeNamespaceResults_NilMonitoringEndTime(t *testing.T) {
	now := time.Now().UTC()
	cpuReq := int64(100)
	rows := []NativeNamespaceRow{
		{
			ClusterUUID:       "cluster-1",
			NamespaceName:     "ns-a",
			Term:              "short",
			Engine:            "cost",
			RecCPURequestMC:   &cpuReq,
			MonitoringEndTime: nil,
			UpdatedAt:         now,
			LastReported:      now,
		},
	}

	results := assembleNativeNamespaceResults(rows, "", false)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	_, ok := results[0].Recommendations["monitoring_end_time"]
	if ok {
		t.Error("nil monitoring_end_time should not be included in recommendations")
	}
}

func TestAssembleNativeNamespaceResults_NilNotificationCodes(t *testing.T) {
	now := time.Now().UTC()
	cpuReq := int64(100)
	rows := []NativeNamespaceRow{
		{
			ClusterUUID:       "cluster-1",
			NamespaceName:     "ns-a",
			Term:              "short",
			Engine:            "cost",
			RecCPURequestMC:   &cpuReq,
			NotificationCodes: nil, // nil codes should become empty slice
			UpdatedAt:         now,
			LastReported:      now,
		},
	}

	results := assembleNativeNamespaceResults(rows, "", false)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	recAny, ok := results[0].Recommendations["short_term"]
	if !ok {
		t.Fatal("expected short_term recommendation")
	}
	rec, ok := recAny.(TermRecommendation)
	if !ok {
		t.Fatalf("expected TermRecommendation, got %T", recAny)
	}
	if rec.Cost == nil {
		t.Fatal("expected cost recommendation")
	}
	if rec.Cost.NotificationCodes == nil {
		t.Error("nil notification codes should be converted to empty SmallintArray")
	}
}
