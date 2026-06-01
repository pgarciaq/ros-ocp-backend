package engine

import (
	"fmt"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// DetectSharedPVCs identifies correlated workload groups in a namespace.
//
// True PVC-to-VM mapping requires persistentvolumeclaim_name on the ROS VM CSV
// (available on cm-openshift-vm-usage from the operator but not ingested yet).
// Until then, peers with the same namespace and placement profile are reported
// as a correlated group when shared-storage checks are enabled.
func DetectSharedPVCs(clusterLatest []model.DailyVMDigest, currentVM model.DailyVMDigest, cfg VMRecConfig) ([]VMNotification, bool) {
	if !cfg.EnableSharedPVCCorrelation || len(clusterLatest) < 2 {
		return nil, false
	}
	profile := vmPlacementProfileKey(currentVM)
	var peers []string
	for _, d := range clusterLatest {
		if d.Namespace != currentVM.Namespace {
			continue
		}
		if d.VMName == currentVM.VMName {
			continue
		}
		if vmPlacementProfileKey(d) != profile {
			continue
		}
		peers = append(peers, d.VMName)
	}
	if len(peers) == 0 {
		return nil, false
	}
	return []VMNotification{{
		Code: NotifVMSharedStorage,
		Type: vmNotifTypeInfo,
		Message: fmt.Sprintf(
			"Correlated workload group in namespace %s (matching resource profile) — peers: %s. "+
				"Per-PVC correlation requires operator persistentvolumeclaim_name on ros-openshift-vm-usage CSV.",
			currentVM.Namespace, strings.Join(peers, ", "),
		),
	}}, true
}
