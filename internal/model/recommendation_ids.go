package model

import (
	"fmt"

	"github.com/google/uuid"
)

// NativeNodeID generates a deterministic UUID v5 for a node utilization recommendation.
// The "node/" prefix avoids collisions with NativeNamespaceID for the same cluster/name pair.
//
// Security: same org_id invariant as NativeContainerID — IDs are not tenant-scoped.
func NativeNodeID(clusterUUID, node string) string {
	name := fmt.Sprintf("node/%s/%s", clusterUUID, node)
	return uuid.NewSHA1(nativeIDNamespace, []byte(name)).String()
}

// NativePvcID generates a deterministic UUID v5 for a PVC recommendation.
func NativePvcID(clusterUUID, namespace, persistentVolumeClaim string) string {
	name := fmt.Sprintf("pvc/%s/%s/%s", clusterUUID, namespace, persistentVolumeClaim)
	return uuid.NewSHA1(nativeIDNamespace, []byte(name)).String()
}

// NativeQuotaID generates a deterministic UUID v5 for a namespace quota recommendation.
func NativeQuotaID(clusterUUID, namespace, quotaName string) string {
	name := fmt.Sprintf("quota/%s/%s/%s", clusterUUID, namespace, quotaName)
	return uuid.NewSHA1(nativeIDNamespace, []byte(name)).String()
}

// NativeClusterQuotaID generates a deterministic UUID v5 for a cluster quota recommendation.
func NativeClusterQuotaID(clusterUUID, clusterQuotaName string) string {
	name := fmt.Sprintf("cluster-quota/%s/%s", clusterUUID, clusterQuotaName)
	return uuid.NewSHA1(nativeIDNamespace, []byte(name)).String()
}

// NativeSnapshotID generates a deterministic UUID v5 for a snapshot recommendation.
func NativeSnapshotID(clusterUUID, namespace, snapshotName string) string {
	name := fmt.Sprintf("snapshot/%s/%s/%s", clusterUUID, namespace, snapshotName)
	return uuid.NewSHA1(nativeIDNamespace, []byte(name)).String()
}

// NativeVMID generates a deterministic UUID v5 for a VM recommendation.
func NativeVMID(clusterUUID, namespace, vmName string) string {
	name := fmt.Sprintf("vm/%s/%s/%s", clusterUUID, namespace, vmName)
	return uuid.NewSHA1(nativeIDNamespace, []byte(name)).String()
}
