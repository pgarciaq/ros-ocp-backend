package gpu

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/model/types"
	libgpu "github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
)

// VMFBUsedFraction returns the fraction of GPU frame buffer used by a VM device digest.
func VMFBUsedFraction(dev types.GPUDeviceDigest) float64 {
	return libgpu.VMFBUsedFraction(dev.Model, dev.FBUsedMaxMiB, dev.FBUsedAvgMiB)
}
