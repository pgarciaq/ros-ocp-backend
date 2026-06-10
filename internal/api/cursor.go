package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// ContainerCursor is the decoded keyset position for container list pagination.
type ContainerCursor struct {
	Namespace     string          `json:"ns"`
	Workload      string          `json:"wl"`
	WorkloadType  string          `json:"wt,omitempty"`
	ContainerName string          `json:"cn"`
	ClusterUUID   string          `json:"cu,omitempty"`
	SortValue     json.RawMessage `json:"sv,omitempty"`
}

// NamespaceCursor is the decoded keyset position for namespace list pagination.
type NamespaceCursor struct {
	Namespace   string          `json:"ns"`
	ClusterUUID string          `json:"cu"`
	SortValue   json.RawMessage `json:"sv,omitempty"`
	OrderBy     string          `json:"ob,omitempty"`
	OrderHow    string          `json:"oh,omitempty"`
}

// EncodeContainerCursor returns an opaque base64url cursor for the next page.
func EncodeContainerCursor(c ContainerCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeContainerCursor decodes an opaque container list cursor.
func DecodeContainerCursor(s string) (ContainerCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return ContainerCursor{}, fmt.Errorf("invalid after cursor: %w", err)
	}
	var c ContainerCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return ContainerCursor{}, fmt.Errorf("invalid after cursor: %w", err)
	}
	return c, nil
}

// EncodeNamespaceCursor returns an opaque base64url cursor for the next namespace page.
func EncodeNamespaceCursor(c NamespaceCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeNamespaceCursor decodes an opaque namespace list cursor.
func DecodeNamespaceCursor(s string) (NamespaceCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return NamespaceCursor{}, fmt.Errorf("invalid after cursor: %w", err)
	}
	var c NamespaceCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return NamespaceCursor{}, fmt.Errorf("invalid after cursor: %w", err)
	}
	return c, nil
}

// PVCCursor is the decoded keyset position for PVC list pagination.
type PVCCursor struct {
	ClusterUUID           string          `json:"cu"`
	Namespace             string          `json:"ns"`
	PersistentVolumeClaim string          `json:"pvc"`
	SortValue             json.RawMessage `json:"sv,omitempty"`
}

// NodeUtilCursor is the decoded keyset position for node utilization list pagination.
type NodeUtilCursor struct {
	ClusterUUID string          `json:"cu"`
	Node        string          `json:"node"`
	SortValue   json.RawMessage `json:"sv,omitempty"`
}

// NodeGPUCursor is the decoded keyset position for GPU time-slicing list pagination.
type NodeGPUCursor struct {
	ClusterUUID string          `json:"cu"`
	NodeName    string          `json:"node"`
	GPUModel    string          `json:"gm"`
	Term        string          `json:"term,omitempty"`
	SortValue   json.RawMessage `json:"sv,omitempty"`
}

// GPUMIGCursor is the decoded keyset position for GPU MIG list pagination.
type GPUMIGCursor struct {
	ClusterUUID string          `json:"cu"`
	Namespace   string          `json:"ns"`
	Container   string          `json:"cn"`
	GPUModel    string          `json:"gm"`
	Term        string          `json:"term,omitempty"`
	SortValue   json.RawMessage `json:"sv,omitempty"`
}

func encodeCursor(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor[T any](s string) (T, error) {
	var out T
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return out, fmt.Errorf("invalid after cursor: %w", err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("invalid after cursor: %w", err)
	}
	return out, nil
}

// EncodePVCCursor returns an opaque base64url cursor for the next PVC page.
func EncodePVCCursor(c PVCCursor) string { return encodeCursor(c) }

// DecodePVCCursor decodes an opaque PVC list cursor.
func DecodePVCCursor(s string) (PVCCursor, error) { return decodeCursor[PVCCursor](s) }

// EncodeNodeUtilCursor returns an opaque base64url cursor for the next node utilization page.
func EncodeNodeUtilCursor(c NodeUtilCursor) string { return encodeCursor(c) }

// DecodeNodeUtilCursor decodes an opaque node utilization list cursor.
func DecodeNodeUtilCursor(s string) (NodeUtilCursor, error) { return decodeCursor[NodeUtilCursor](s) }

// EncodeNodeGPUCursor returns an opaque base64url cursor for the next GPU time-slicing page.
func EncodeNodeGPUCursor(c NodeGPUCursor) string { return encodeCursor(c) }

// DecodeNodeGPUCursor decodes an opaque GPU time-slicing list cursor.
func DecodeNodeGPUCursor(s string) (NodeGPUCursor, error) { return decodeCursor[NodeGPUCursor](s) }

// EncodeGPUMIGCursor returns an opaque base64url cursor for the next GPU MIG page.
func EncodeGPUMIGCursor(c GPUMIGCursor) string { return encodeCursor(c) }

// DecodeGPUMIGCursor decodes an opaque GPU MIG list cursor.
func DecodeGPUMIGCursor(s string) (GPUMIGCursor, error) { return decodeCursor[GPUMIGCursor](s) }

// QuotaCursor is the decoded keyset position for quota list pagination.
type QuotaCursor struct {
	ClusterUUID string          `json:"cu"`
	Namespace   string          `json:"ns"`
	QuotaName   string          `json:"qn"`
	GroupKey    string          `json:"gk,omitempty"`
	SortValue   json.RawMessage `json:"sv,omitempty"`
}

// ClusterQuotaCursor is the decoded keyset position for cluster-quota list pagination.
type ClusterQuotaCursor struct {
	ClusterUUID      string          `json:"cu"`
	ClusterQuotaName string          `json:"cqn"`
	GroupKey         string          `json:"gk,omitempty"`
	SortValue        json.RawMessage `json:"sv,omitempty"`
}

// VMCursor is the decoded keyset position for VM list pagination.
type VMCursor struct {
	ClusterUUID string          `json:"cu"`
	VMName      string          `json:"vm"`
	Namespace   string          `json:"ns"`
	Term        string          `json:"term,omitempty"`
	Engine      string          `json:"eng,omitempty"`
	SortValue   json.RawMessage `json:"sv,omitempty"`
}

// MachineSetCursor is the decoded keyset position for MachineSet list pagination.
type MachineSetCursor struct {
	MachineSetName string          `json:"ms"`
	ClusterUUID    string          `json:"cu"`
	SortValue      json.RawMessage `json:"sv,omitempty"`
}

// SnapshotCursor is the decoded keyset position for snapshot list pagination.
type SnapshotCursor struct {
	ClusterUUID  string          `json:"cu"`
	Namespace    string          `json:"ns"`
	SnapshotName string          `json:"sn"`
	SortValue    json.RawMessage `json:"sv,omitempty"`
}

// EncodeQuotaCursor returns an opaque base64url cursor for the next quota page.
func EncodeQuotaCursor(c QuotaCursor) string { return encodeCursor(c) }

// DecodeQuotaCursor decodes an opaque quota list cursor.
func DecodeQuotaCursor(s string) (QuotaCursor, error) { return decodeCursor[QuotaCursor](s) }

// EncodeClusterQuotaCursor returns an opaque base64url cursor for the next cluster-quota page.
func EncodeClusterQuotaCursor(c ClusterQuotaCursor) string { return encodeCursor(c) }

// DecodeClusterQuotaCursor decodes an opaque cluster-quota list cursor.
func DecodeClusterQuotaCursor(s string) (ClusterQuotaCursor, error) {
	return decodeCursor[ClusterQuotaCursor](s)
}

// EncodeVMCursor returns an opaque base64url cursor for the next VM page.
func EncodeVMCursor(c VMCursor) string { return encodeCursor(c) }

// DecodeVMCursor decodes an opaque VM list cursor.
func DecodeVMCursor(s string) (VMCursor, error) { return decodeCursor[VMCursor](s) }

// EncodeMachineSetCursor returns an opaque base64url cursor for the next MachineSet page.
func EncodeMachineSetCursor(c MachineSetCursor) string { return encodeCursor(c) }

// DecodeMachineSetCursor decodes an opaque MachineSet list cursor.
func DecodeMachineSetCursor(s string) (MachineSetCursor, error) {
	return decodeCursor[MachineSetCursor](s)
}

// EncodeSnapshotCursor returns an opaque base64url cursor for the next snapshot page.
func EncodeSnapshotCursor(c SnapshotCursor) string { return encodeCursor(c) }

// DecodeSnapshotCursor decodes an opaque snapshot list cursor.
func DecodeSnapshotCursor(s string) (SnapshotCursor, error) { return decodeCursor[SnapshotCursor](s) }
