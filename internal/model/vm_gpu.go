package model

// GPUDeviceDigest holds per-GPU metrics for a VM daily digest.
type GPUDeviceDigest struct {
	UUID          string  `json:"uuid"`
	Model         string  `json:"model"`
	UtilAvgBP     int32   `json:"util_avg_bp"`
	UtilMaxBP     int32   `json:"util_max_bp"`
	FBUsedAvgMiB  float64 `json:"fb_used_avg_mib"`
	FBUsedMaxMiB  float64 `json:"fb_used_max_mib"`
	SMActiveAvgBP int32   `json:"sm_active_avg_bp"`
	TensorAvgBP   int32   `json:"tensor_avg_bp"`
	DRAMAvgBP     int32   `json:"dram_avg_bp"`
	MIGProfile    string  `json:"mig_profile,omitempty"`
	MaxSlices     int32   `json:"max_slices,omitempty"`
}
