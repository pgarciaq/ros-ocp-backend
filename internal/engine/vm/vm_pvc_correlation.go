package vm

import (
	"fmt"
	"sort"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// pvcNSKey scopes a PVC name to a namespace for the reverse index.
type pvcNSKey struct {
	Namespace string
	PVCName   string
}

// ClusterContext holds cluster-wide data pre-computed once per engine run.
// The PVCToVMs reverse index allows O(P) shared-PVC detection per VM
// instead of O(N×P) full scans (see PVC-SCAN performance finding).
type ClusterContext struct {
	Latest   []model.DailyVMDigest
	PVCToVMs map[pvcNSKey][]string
}

// NewClusterContext builds a ClusterContext from the latest digests,
// pre-computing the PVC→VMs reverse index in a single O(N×P) pass.
func NewClusterContext(latest []model.DailyVMDigest) *ClusterContext {
	if len(latest) == 0 {
		return &ClusterContext{}
	}
	idx := make(map[pvcNSKey][]string)
	for _, d := range latest {
		for _, pvc := range d.PVCs {
			key := pvcNSKey{Namespace: d.Namespace, PVCName: pvc.PVCName}
			idx[key] = append(idx[key], d.VMName)
		}
	}
	return &ClusterContext{Latest: latest, PVCToVMs: idx}
}

// DetectSharedPVCs identifies VMs sharing PersistentVolumeClaims.
//
// When PVC data is available (from the ros-openshift-vm-pvc companion CSV),
// detection uses the pre-built PVCToVMs reverse index for O(P) lookup
// per VM instead of scanning all cluster VMs.
//
// When PVC data is absent (legacy payloads without the companion CSV),
// falls back to proxy detection using namespace + placement profile matching.
func DetectSharedPVCs(clusterCtx *ClusterContext, currentVM model.DailyVMDigest, cfg VMRecConfig) ([]VMNotification, bool) {
	if clusterCtx == nil || !cfg.EnableSharedPVCCorrelation || len(clusterCtx.Latest) < 2 {
		return nil, false
	}

	if len(currentVM.PVCs) > 0 {
		return detectSharedPVCsByName(clusterCtx, currentVM)
	}
	return detectSharedPVCsByProxy(clusterCtx.Latest, currentVM)
}

// detectSharedPVCsByName uses the pre-built PVC→VMs reverse index.
// Complexity is O(P) per VM instead of O(N×P), where P = PVCs on currentVM.
func detectSharedPVCsByName(clusterCtx *ClusterContext, currentVM model.DailyVMDigest) ([]VMNotification, bool) {
	type sharedPVC struct {
		pvcName string
		peers   []string
	}
	shared := make(map[string]*sharedPVC)

	for _, pvc := range currentVM.PVCs {
		key := pvcNSKey{Namespace: currentVM.Namespace, PVCName: pvc.PVCName}
		vms := clusterCtx.PVCToVMs[key]
		for _, vmName := range vms {
			if vmName == currentVM.VMName {
				continue
			}
			sp, ok := shared[pvc.PVCName]
			if !ok {
				sp = &sharedPVC{pvcName: pvc.PVCName}
				shared[pvc.PVCName] = sp
			}
			sp.peers = append(sp.peers, vmName)
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
