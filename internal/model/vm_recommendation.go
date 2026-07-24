package model

import "github.com/redhatinsights/ros-ocp-backend/internal/model/types"

// Re-export VM recommendation types from the lightweight types sub-package.
type VMRecommendation = types.VMRecommendation

// VM category constants.
const (
	VMCategoryAbandoned         = types.VMCategoryAbandoned
	VMCategoryPowerOffCandidate = types.VMCategoryPowerOffCandidate
	VMCategoryIdle              = types.VMCategoryIdle
	VMCategoryOversized         = types.VMCategoryOversized
	VMCategoryUndersized        = types.VMCategoryUndersized
	VMCategoryOptimized         = types.VMCategoryOptimized
)

// ValidVMCategories re-exports the validation map.
var ValidVMCategories = types.ValidVMCategories
