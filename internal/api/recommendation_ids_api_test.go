package api_test

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// Cross-repo parity with koku-ui/apps/koku-ui-ros/src/utils/recommendationIds.ts.
func TestRecommendationIDs_MatchUIFormulas(t *testing.T) {
	t.Parallel()
	if got, want := model.NativeNodeID("cluster-1", "kube-system"), "2197ba7c-d75a-5b5d-83d9-fc712236809c"; got != want {
		t.Errorf("NativeNodeID: got %q want %q", got, want)
	}
	if got, want := model.NativePvcID("c1", "apps", "data"), "7eda0e9b-46a3-50c6-aaab-dd7d2a844c01"; got != want {
		t.Errorf("NativePvcID: got %q want %q", got, want)
	}
}

func TestRecommendationIDs_DoNotCollideWithNamespace(t *testing.T) {
	nodeID := model.NativeNodeID("cluster-1", "kube-system")
	nsID := model.NativeNamespaceID("cluster-1", "kube-system")
	if nodeID == nsID {
		t.Error("node and namespace IDs must differ for same cluster/name")
	}
}
