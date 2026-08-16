package pgrec

import (
	"fmt"

	"github.com/google/uuid"
)

// NativeIDNamespace matches internal/model/types so container_id stays stable.
var NativeIDNamespace = uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479")

// NativeContainerID generates a deterministic UUID v5 for a container recommendation.
func NativeContainerID(clusterUUID, namespace, workload, workloadType, container string) string {
	name := fmt.Sprintf("%s/%s/%s/%s/%s", clusterUUID, namespace, workload, workloadType, container)
	return uuid.NewSHA1(NativeIDNamespace, []byte(name)).String()
}

// NativeNamespaceID generates a deterministic UUID v5 for a namespace recommendation.
func NativeNamespaceID(clusterUUID, namespace string) string {
	name := fmt.Sprintf("%s/%s", clusterUUID, namespace)
	return uuid.NewSHA1(NativeIDNamespace, []byte(name)).String()
}

// NativeNodeID generates a deterministic UUID v5 for a node utilization recommendation.
func NativeNodeID(clusterUUID, node string) string {
	name := fmt.Sprintf("node/%s/%s", clusterUUID, node)
	return uuid.NewSHA1(NativeIDNamespace, []byte(name)).String()
}

// NativePvcID generates a deterministic UUID v5 for a PVC recommendation.
func NativePvcID(clusterUUID, namespace, persistentVolumeClaim string) string {
	name := fmt.Sprintf("pvc/%s/%s/%s", clusterUUID, namespace, persistentVolumeClaim)
	return uuid.NewSHA1(NativeIDNamespace, []byte(name)).String()
}

// NativeQuotaID generates a deterministic UUID v5 for a namespace quota recommendation.
func NativeQuotaID(clusterUUID, namespace, quotaName string) string {
	name := fmt.Sprintf("quota/%s/%s/%s", clusterUUID, namespace, quotaName)
	return uuid.NewSHA1(NativeIDNamespace, []byte(name)).String()
}

// NativeClusterQuotaID generates a deterministic UUID v5 for a cluster quota recommendation.
func NativeClusterQuotaID(clusterUUID, clusterQuotaName string) string {
	name := fmt.Sprintf("cluster-quota/%s/%s", clusterUUID, clusterQuotaName)
	return uuid.NewSHA1(NativeIDNamespace, []byte(name)).String()
}

// NativeVMID generates a deterministic UUID v5 for a VM recommendation.
func NativeVMID(clusterUUID, namespace, vmName string) string {
	name := fmt.Sprintf("vm/%s/%s/%s", clusterUUID, namespace, vmName)
	return uuid.NewSHA1(NativeIDNamespace, []byte(name)).String()
}
