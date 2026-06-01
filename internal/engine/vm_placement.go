package engine

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// extractVMNamePrefix strips trailing instance identifiers from a VM name for HA grouping.
func extractVMNamePrefix(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	parts := strings.Split(name, "-")
	for len(parts) > 1 {
		last := parts[len(parts)-1]
		if isInstanceSuffix(last) {
			parts = parts[:len(parts)-1]
		} else {
			break
		}
	}
	return strings.Join(parts, "-")
}

func isInstanceSuffix(s string) bool {
	if s == "" {
		return false
	}
	if _, err := strconv.Atoi(s); err == nil {
		return true
	}
	if len(s) == 1 && s[0] >= 'a' && s[0] <= 'z' {
		return true
	}
	if len(s) >= 5 && len(s) <= 8 && isHexString(s) {
		return true
	}
	return false
}

func isHexString(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// vmPlacementPrefixGroupKey groups VMs by namespace and derived VM name prefix.
func vmPlacementPrefixGroupKey(d model.DailyVMDigest) string {
	return fmt.Sprintf("%s|%s", d.Namespace, extractVMNamePrefix(d.VMName))
}

// vmPlacementProfileKey groups VMs by namespace and matching resource profile.
// App labels are not in the VM CSV today; identical vCPU/memory/disk sizing in the
// same namespace is used as a redundancy signal for HA-style deployments.
func vmPlacementProfileKey(d model.DailyVMDigest) string {
	diskGiB := int64(0)
	if d.DiskAllocatedMaxBytes > 0 {
		diskGiB = d.DiskAllocatedMaxBytes / (1024 * 1024 * 1024)
	}
	return fmt.Sprintf("%s|%d|%d|%d", d.Namespace, vmCurrentVCPU(d), vmCurrentMemoryGiB(d), diskGiB)
}

// buildClusterLatestDigests returns the newest digest per VM in the cluster.
func buildClusterLatestDigests(all []model.DailyVMDigest) []model.DailyVMDigest {
	type vmKey struct {
		vmName    string
		namespace string
	}
	latest := make(map[vmKey]model.DailyVMDigest)
	for _, d := range all {
		k := vmKey{vmName: d.VMName, namespace: d.Namespace}
		if prev, ok := latest[k]; !ok || d.BucketDate.After(prev.BucketDate) {
			latest[k] = d
		}
	}
	out := make([]model.DailyVMDigest, 0, len(latest))
	for _, d := range latest {
		out = append(out, d)
	}
	return out
}

// DetectSameNodeRedundancy flags co-located peers with the same placement profile
// and uneven node distribution within a profile group.
func DetectSameNodeRedundancy(clusterLatest []model.DailyVMDigest, currentVM model.DailyVMDigest, cfg VMRecConfig) []VMNotification {
	if !cfg.EnablePlacementChecks || len(clusterLatest) < 2 {
		return nil
	}
	node := strings.TrimSpace(currentVM.NodeName)
	if node == "" {
		return nil
	}
	profileKey := vmPlacementProfileKey(currentVM)
	prefixKey := vmPlacementPrefixGroupKey(currentVM)
	prefixPeers := 0
	for _, d := range clusterLatest {
		if vmPlacementPrefixGroupKey(d) == prefixKey {
			prefixPeers++
		}
	}

	var sameNodePeers []string
	perNode := make(map[string]int)
	var groupCount int

	for _, d := range clusterLatest {
		activeKey := vmPlacementPrefixGroupKey(d)
		if prefixPeers >= 2 {
			if activeKey != prefixKey {
				continue
			}
		} else if vmPlacementProfileKey(d) != profileKey {
			continue
		}
		groupCount++
		n := strings.TrimSpace(d.NodeName)
		if n == "" {
			continue
		}
		perNode[n]++
		if d.VMName == currentVM.VMName && d.Namespace == currentVM.Namespace {
			continue
		}
		if n == node {
			sameNodePeers = append(sameNodePeers, d.VMName)
		}
	}
	if groupCount < 2 {
		return nil
	}

	var out []VMNotification
	if len(sameNodePeers) > 0 {
		out = append(out, VMNotification{
			Code: NotifVMRedundantColocation,
			Type: vmNotifTypeWarning,
			Message: fmt.Sprintf(
				"Redundant VMs co-located on same node (%s) — peers: %s. Consider pod anti-affinity or topology spread constraints.",
				node, strings.Join(sameNodePeers, ", "),
			),
		})
	}

	ratio := cfg.PlacementSkewRatio
	if ratio < 2 {
		ratio = 3
	}
	min := 0
	max := 0
	for _, c := range perNode {
		if min == 0 || c < min {
			min = c
		}
		if c > max {
			max = c
		}
	}
	if min > 0 && max >= min*ratio {
		out = append(out, VMNotification{
			Code: NotifVMUnevenNodeDistribution,
			Type: vmNotifTypeInfo,
			Message: fmt.Sprintf(
				"Uneven VM distribution across nodes for namespace %s (max %d vs min %d per node) — consider topologySpreadConstraints.",
				currentVM.Namespace, max, min,
			),
		})
	}
	return out
}
