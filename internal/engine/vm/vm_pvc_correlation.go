package vm

import (
	"fmt"
	"sort"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// DetectSharedPVCs identifies VMs sharing PersistentVolumeClaims.
//
// When PVC data is available (from the ros-openshift-vm-pvc companion CSV),
// detection uses actual PVC name overlap: two different VMs in the same
// namespace with a matching pvc_name in their vm_pvc_digests.
//
// When PVC data is absent (legacy payloads without the companion CSV),
// falls back to proxy detection using namespace + placement profile matching.
func DetectSharedPVCs(clusterLatest []model.DailyVMDigest, currentVM model.DailyVMDigest, cfg VMRecConfig) ([]VMNotification, bool) {
	if !cfg.EnableSharedPVCCorrelation || len(clusterLatest) < 2 {
		return nil, false
	}

	if len(currentVM.PVCs) > 0 {
		return detectSharedPVCsByName(clusterLatest, currentVM)
	}
	return detectSharedPVCsByProxy(clusterLatest, currentVM)
}

// detectSharedPVCsByName uses actual PVC names to detect sharing.
func detectSharedPVCsByName(clusterLatest []model.DailyVMDigest, currentVM model.DailyVMDigest) ([]VMNotification, bool) {
	currentPVCNames := make(map[string]bool, len(currentVM.PVCs))
	for _, pvc := range currentVM.PVCs {
		currentPVCNames[pvc.PVCName] = true
	}

	type sharedPVC struct {
		pvcName string
		peers   []string
	}
	shared := make(map[string]*sharedPVC)

	for _, d := range clusterLatest {
		if d.Namespace != currentVM.Namespace || d.VMName == currentVM.VMName {
			continue
		}
		for _, pvc := range d.PVCs {
			if currentPVCNames[pvc.PVCName] {
				sp, ok := shared[pvc.PVCName]
				if !ok {
					sp = &sharedPVC{pvcName: pvc.PVCName}
					shared[pvc.PVCName] = sp
				}
				sp.peers = append(sp.peers, d.VMName)
			}
		}
	}

	if len(shared) == 0 {
		return nil, false
	}

	var notifications []VMNotification
	for _, sp := range shared {
		uniquePeers := uniqueStrings(sp.peers)
		sort.Strings(uniquePeers)
		notifications = append(notifications, VMNotification{
			Code: NotifVMSharedStorage,
			Type: vmNotifTypeInfo,
			Message: fmt.Sprintf(
				"VMs sharing PVC %q in namespace %s: %s",
				sp.pvcName, currentVM.Namespace, strings.Join(uniquePeers, ", "),
			),
		})
	}
	sort.Slice(notifications, func(i, j int) bool {
		return notifications[i].Message < notifications[j].Message
	})
	return notifications, true
}

// detectSharedPVCsByProxy uses namespace + placement profile matching as a proxy.
func detectSharedPVCsByProxy(clusterLatest []model.DailyVMDigest, currentVM model.DailyVMDigest) ([]VMNotification, bool) {
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
				"Per-PVC correlation requires operator persistentvolumeclaim_name on ros-openshift-vm-pvc CSV.",
			currentVM.Namespace, strings.Join(peers, ", "),
		),
	}}, true
}

func uniqueStrings(s []string) []string {
	seen := make(map[string]bool, len(s))
	result := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}
