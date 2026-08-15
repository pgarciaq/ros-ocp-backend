package vm

import "github.com/redhatinsights/ros-ocp-backend/internal/model/types"

// Digest is the in-memory VM daily digest used by compute and pgx Scan.
// Same underlying type as model.DailyVMDigest (alias of types.DailyVMDigest).
// Do not add a converter or a second struct.
type Digest = types.DailyVMDigest

// Recommendation is the in-memory VM recommendation used by compute and pgx Scan.
// Same underlying type as model.VMRecommendation.
type Recommendation = types.VMRecommendation

// GPUDeviceDigest and PVCDigest are nested digest slices on Digest.
type GPUDeviceDigest = types.GPUDeviceDigest
type PVCDigest = types.PVCDigest

const (
	VMCategoryAbandoned         = types.VMCategoryAbandoned
	VMCategoryPowerOffCandidate = types.VMCategoryPowerOffCandidate
	VMCategoryIdle              = types.VMCategoryIdle
	VMCategoryOversized         = types.VMCategoryOversized
	VMCategoryUndersized        = types.VMCategoryUndersized
	VMCategoryOptimized         = types.VMCategoryOptimized
)
