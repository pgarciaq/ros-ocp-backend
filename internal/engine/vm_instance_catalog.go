package engine

import "math"

const vmSeriesGPU = "gpu"

// InstanceType describes an OpenShift Virtualization instance type.
type InstanceType struct {
	Name      string
	Series    string // general-purpose, compute-optimized, memory-optimized, gpu
	VCPU      int32
	MemoryGiB int32 // nominal GiB; use memoryCapacityMiB for smallest-fit comparison
	GPUs      int32
}

func memoryCapacityMiB(t InstanceType) int32 {
	switch t.Name {
	case "u1.nano":
		return 512
	default:
		return t.MemoryGiB * 1024
	}
}

// vmInstanceCatalog lists OpenShift Virtualization instance types (non-GPU), sorted by
// series then vCPU then memory.
var vmInstanceCatalog = []InstanceType{
	// General purpose (u-series)
	{Name: "u1.nano", Series: vmSeriesGeneralPurpose, VCPU: 1, MemoryGiB: 1, GPUs: 0},
	{Name: "u1.micro", Series: vmSeriesGeneralPurpose, VCPU: 1, MemoryGiB: 1, GPUs: 0},
	{Name: "u1.small", Series: vmSeriesGeneralPurpose, VCPU: 1, MemoryGiB: 2, GPUs: 0},
	{Name: "u1.medium", Series: vmSeriesGeneralPurpose, VCPU: 1, MemoryGiB: 4, GPUs: 0},
	{Name: "u1.large", Series: vmSeriesGeneralPurpose, VCPU: 2, MemoryGiB: 8, GPUs: 0},
	{Name: "u1.xlarge", Series: vmSeriesGeneralPurpose, VCPU: 4, MemoryGiB: 16, GPUs: 0},
	{Name: "u1.2xlarge", Series: vmSeriesGeneralPurpose, VCPU: 8, MemoryGiB: 32, GPUs: 0},
	{Name: "u1.4xlarge", Series: vmSeriesGeneralPurpose, VCPU: 16, MemoryGiB: 64, GPUs: 0},
	{Name: "u1.8xlarge", Series: vmSeriesGeneralPurpose, VCPU: 32, MemoryGiB: 128, GPUs: 0},
	// Compute optimized (cx-series)
	{Name: "cx1.medium", Series: vmSeriesComputeOptimized, VCPU: 1, MemoryGiB: 2, GPUs: 0},
	{Name: "cx1.large", Series: vmSeriesComputeOptimized, VCPU: 2, MemoryGiB: 4, GPUs: 0},
	{Name: "cx1.xlarge", Series: vmSeriesComputeOptimized, VCPU: 4, MemoryGiB: 8, GPUs: 0},
	{Name: "cx1.2xlarge", Series: vmSeriesComputeOptimized, VCPU: 8, MemoryGiB: 16, GPUs: 0},
	{Name: "cx1.4xlarge", Series: vmSeriesComputeOptimized, VCPU: 16, MemoryGiB: 32, GPUs: 0},
	{Name: "cx1.8xlarge", Series: vmSeriesComputeOptimized, VCPU: 32, MemoryGiB: 64, GPUs: 0},
	// Memory optimized (m-series)
	{Name: "m1.large", Series: vmSeriesMemoryOptimized, VCPU: 2, MemoryGiB: 16, GPUs: 0},
	{Name: "m1.xlarge", Series: vmSeriesMemoryOptimized, VCPU: 4, MemoryGiB: 32, GPUs: 0},
	{Name: "m1.2xlarge", Series: vmSeriesMemoryOptimized, VCPU: 8, MemoryGiB: 64, GPUs: 0},
	{Name: "m1.4xlarge", Series: vmSeriesMemoryOptimized, VCPU: 16, MemoryGiB: 128, GPUs: 0},
}

// vmInstanceCatalogGPU is reference-only; not used for matching until GPU metrics exist.
var vmInstanceCatalogGPU = []InstanceType{
	{Name: "gn1.xlarge", Series: vmSeriesGPU, VCPU: 4, MemoryGiB: 16, GPUs: 1},
	{Name: "gn1.2xlarge", Series: vmSeriesGPU, VCPU: 8, MemoryGiB: 32, GPUs: 1},
	{Name: "gn1.4xlarge", Series: vmSeriesGPU, VCPU: 16, MemoryGiB: 64, GPUs: 2},
	{Name: "gn1.8xlarge", Series: vmSeriesGPU, VCPU: 32, MemoryGiB: 128, GPUs: 4},
}

func recommendedMemoryMiB(recommendedMemoryGiB int32) int32 {
	if recommendedMemoryGiB <= 0 {
		return 512
	}
	return recommendedMemoryGiB * 1024
}

func instanceTypeWaste(t InstanceType, recommendedVCPU, recommendedMemoryMiB int32) int32 {
	memMiB := memoryCapacityMiB(t)
	return (t.VCPU - recommendedVCPU) + (memMiB-recommendedMemoryMiB)/1024
}

func smallestFitInCatalog(catalog []InstanceType, series string, recommendedVCPU, recommendedMemoryMiB int32) *InstanceType {
	var best *InstanceType
	var bestWaste int32 = math.MaxInt32

	for i := range catalog {
		t := &catalog[i]
		if t.GPUs > 0 {
			continue
		}
		if t.Series != series {
			continue
		}
		if t.VCPU < recommendedVCPU || memoryCapacityMiB(*t) < recommendedMemoryMiB {
			continue
		}
		waste := instanceTypeWaste(*t, recommendedVCPU, recommendedMemoryMiB)
		if best == nil || waste < bestWaste ||
			(waste == bestWaste && (t.VCPU < best.VCPU || (t.VCPU == best.VCPU && memoryCapacityMiB(*t) < memoryCapacityMiB(*best)))) {
			best = t
			bestWaste = waste
		}
	}
	return best
}

func smallestFitInSeries(series string, recommendedVCPU, recommendedMemoryMiB int32) *InstanceType {
	return smallestFitInCatalog(vmInstanceCatalog, series, recommendedVCPU, recommendedMemoryMiB)
}

func matchInCatalog(catalog []InstanceType, recommendedVCPU, recommendedMemoryGiB int32, preferredSeries string) *InstanceType {
	if len(catalog) == 0 {
		return nil
	}
	if match := smallestFitInCatalog(catalog, preferredSeries, recommendedVCPU, recommendedMemoryGiB); match != nil {
		return match
	}
	if preferredSeries != vmSeriesGeneralPurpose {
		return smallestFitInCatalog(catalog, vmSeriesGeneralPurpose, recommendedVCPU, recommendedMemoryGiB)
	}
	return nil
}

// MatchInstanceType finds the smallest instance type that fits the recommended vCPU and memory.
// It prefers clusterTypes when provided, then falls back to the global OpenShift Virtualization catalog.
// Returns nil if no suitable type is found. GPU types are never matched.
// Callers should skip this when instance type matching is disabled.
func MatchInstanceType(recommendedVCPU, recommendedMemoryGiB int32, preferredSeries string, clusterTypes []InstanceType) *InstanceType {
	if recommendedVCPU < 1 {
		recommendedVCPU = 1
	}
	recMemMiB := recommendedMemoryMiB(recommendedMemoryGiB)

	if match := matchInCatalog(clusterTypes, recommendedVCPU, recMemMiB, preferredSeries); match != nil {
		return match
	}
	if match := matchInCatalog(vmInstanceCatalog, recommendedVCPU, recMemMiB, preferredSeries); match != nil {
		return match
	}
	return nil
}
