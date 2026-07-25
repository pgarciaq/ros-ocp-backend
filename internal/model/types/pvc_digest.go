package types

// PVCDigest holds per-PVC storage information for a VM daily digest.
type PVCDigest struct {
	PVCName           string `json:"pvc_name" db:"pvc_name"`
	DiskCapacityBytes int64  `json:"disk_capacity_bytes" db:"disk_capacity_bytes"`
	VolumeMode        string `json:"volume_mode" db:"volume_mode"`
}
