package types

import libvm "github.com/redhatinsights/ros-ocp-backend/librobne/vm"

// VMRecommendation is the canonical VM recommendation (librobne/vm).
type VMRecommendation = libvm.VMRecommendation

const (
	VMCategoryAbandoned         = libvm.VMCategoryAbandoned
	VMCategoryPowerOffCandidate = libvm.VMCategoryPowerOffCandidate
	VMCategoryIdle              = libvm.VMCategoryIdle
	VMCategoryOversized         = libvm.VMCategoryOversized
	VMCategoryUndersized        = libvm.VMCategoryUndersized
	VMCategoryOptimized         = libvm.VMCategoryOptimized
)

// ValidVMCategories lists all valid category values for filter validation.
var ValidVMCategories = libvm.ValidVMCategories
