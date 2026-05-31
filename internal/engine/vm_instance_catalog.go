package engine

import "math"

const (
	vmSeriesGPU              = "gpu"
	vmSeriesNetworkOptimized = "network-optimized"
)

// InstanceType describes an OpenShift Virtualization instance type.
type InstanceType struct {
	Name       string
	Series     string // general-purpose, compute-optimized, memory-optimized, network-optimized, gpu
	VCPU       int32
	MemoryGiB  int32 // nominal GiB; use memoryCapacityMiB for smallest-fit comparison
	GPUs       int32
	Selectable bool // false = recognition only (n-series, gn-series until metrics exist)
}

func memoryCapacityMiB(t InstanceType) int32 {
	switch t.Name {
	case "u1.nano":
		return 512
	default:
		return t.MemoryGiB * 1024
	}
}

// vmInstanceCatalog lists OpenShift Virtualization instance types for matching and recognition.
var vmInstanceCatalog = []InstanceType{
	// General purpose (u-series)
	{Name: "u1.nano", Series: vmSeriesGeneralPurpose, VCPU: 1, MemoryGiB: 1, GPUs: 0, Selectable: true},
	{Name: "u1.micro", Series: vmSeriesGeneralPurpose, VCPU: 1, MemoryGiB: 1, GPUs: 0, Selectable: true},
	{Name: "u1.small", Series: vmSeriesGeneralPurpose, VCPU: 1, MemoryGiB: 2, GPUs: 0, Selectable: true},
	{Name: "u1.medium", Series: vmSeriesGeneralPurpose, VCPU: 1, MemoryGiB: 4, GPUs: 0, Selectable: true},
	{Name: "u1.large", Series: vmSeriesGeneralPurpose, VCPU: 2, MemoryGiB: 8, GPUs: 0, Selectable: true},
	{Name: "u1.xlarge", Series: vmSeriesGeneralPurpose, VCPU: 4, MemoryGiB: 16, GPUs: 0, Selectable: true},
	{Name: "u1.2xlarge", Series: vmSeriesGeneralPurpose, VCPU: 8, MemoryGiB: 32, GPUs: 0, Selectable: true},
	{Name: "u1.4xlarge", Series: vmSeriesGeneralPurpose, VCPU: 16, MemoryGiB: 64, GPUs: 0, Selectable: true},
	{Name: "u1.8xlarge", Series: vmSeriesGeneralPurpose, VCPU: 32, MemoryGiB: 128, GPUs: 0, Selectable: true},
	// Compute optimized (cx-series)
	{Name: "cx1.medium", Series: vmSeriesComputeOptimized, VCPU: 1, MemoryGiB: 2, GPUs: 0, Selectable: true},
	{Name: "cx1.large", Series: vmSeriesComputeOptimized, VCPU: 2, MemoryGiB: 4, GPUs: 0, Selectable: true},
	{Name: "cx1.xlarge", Series: vmSeriesComputeOptimized, VCPU: 4, MemoryGiB: 8, GPUs: 0, Selectable: true},
	{Name: "cx1.2xlarge", Series: vmSeriesComputeOptimized, VCPU: 8, MemoryGiB: 16, GPUs: 0, Selectable: true},
	{Name: "cx1.4xlarge", Series: vmSeriesComputeOptimized, VCPU: 16, MemoryGiB: 32, GPUs: 0, Selectable: true},
	{Name: "cx1.8xlarge", Series: vmSeriesComputeOptimized, VCPU: 32, MemoryGiB: 64, GPUs: 0, Selectable: true},
	// Memory optimized (m-series)
	{Name: "m1.large", Series: vmSeriesMemoryOptimized, VCPU: 2, MemoryGiB: 16, GPUs: 0, Selectable: true},
	{Name: "m1.xlarge", Series: vmSeriesMemoryOptimized, VCPU: 4, MemoryGiB: 32, GPUs: 0, Selectable: true},
	{Name: "m1.2xlarge", Series: vmSeriesMemoryOptimized, VCPU: 8, MemoryGiB: 64, GPUs: 0, Selectable: true},
	{Name: "m1.4xlarge", Series: vmSeriesMemoryOptimized, VCPU: 16, MemoryGiB: 128, GPUs: 0, Selectable: true},
	// Network optimized (n-series) — recognition only until network metrics exist
	{Name: "n1.medium", Series: vmSeriesNetworkOptimized, VCPU: 1, MemoryGiB: 4, GPUs: 0, Selectable: false},
	{Name: "n1.large", Series: vmSeriesNetworkOptimized, VCPU: 2, MemoryGiB: 8, GPUs: 0, Selectable: false},
	{Name: "n1.xlarge", Series: vmSeriesNetworkOptimized, VCPU: 4, MemoryGiB: 16, GPUs: 0, Selectable: false},
	{Name: "n1.2xlarge", Series: vmSeriesNetworkOptimized, VCPU: 8, MemoryGiB: 32, GPUs: 0, Selectable: false},
}

// vmInstanceCatalogGPU is reference-only; not recommended until GPU metrics exist.
var vmInstanceCatalogGPU = []InstanceType{
	{Name: "gn1.xlarge", Series: vmSeriesGPU, VCPU: 4, MemoryGiB: 16, GPUs: 1, Selectable: false},
	{Name: "gn1.2xlarge", Series: vmSeriesGPU, VCPU: 8, MemoryGiB: 32, GPUs: 1, Selectable: false},
	{Name: "gn1.4xlarge", Series: vmSeriesGPU, VCPU: 16, MemoryGiB: 64, GPUs: 2, Selectable: false},
	{Name: "gn1.8xlarge", Series: vmSeriesGPU, VCPU: 32, MemoryGiB: 128, GPUs: 4, Selectable: false},
}

func vmAllInstanceTypes(clusterTypes []InstanceType) []InstanceType {
	out := make([]InstanceType, 0, len(vmInstanceCatalog)+len(vmInstanceCatalogGPU)+len(clusterTypes))
	out = append(out, vmInstanceCatalog...)
	out = append(out, vmInstanceCatalogGPU...)
	out = append(out, clusterTypes...)
	return out
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

func smallestFitInCatalog(catalog []InstanceType, series string, recommendedVCPU, recommendedMemoryMiB int32, selectableOnly bool) *InstanceType {
	var best *InstanceType
	var bestWaste int32 = math.MaxInt32

	for i := range catalog {
		t := &catalog[i]
		if selectableOnly && !t.Selectable {
			continue
		}
		if t.GPUs > 0 && selectableOnly {
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
	return smallestFitInCatalog(vmInstanceCatalog, series, recommendedVCPU, recommendedMemoryMiB, true)
}

func matchInCatalog(
	catalog []InstanceType,
	recommendedVCPU, recommendedMemoryMiB int32,
	preferredSeries string,
	selectableOnly bool,
) *InstanceType {
	if len(catalog) == 0 {
		return nil
	}
	if match := smallestFitInCatalog(catalog, preferredSeries, recommendedVCPU, recommendedMemoryMiB, selectableOnly); match != nil {
		return match
	}
	if preferredSeries != vmSeriesGeneralPurpose {
		return smallestFitInCatalog(catalog, vmSeriesGeneralPurpose, recommendedVCPU, recommendedMemoryMiB, selectableOnly)
	}
	return nil
}

// MatchInstanceType finds the smallest selectable instance type that fits recommended vCPU and memory.
// Cluster types may include non-selectable entries for recognition; only Selectable types are returned.
func MatchInstanceType(recommendedVCPU, recommendedMemoryGiB int32, preferredSeries string, clusterTypes []InstanceType) *InstanceType {
	if recommendedVCPU < 1 {
		recommendedVCPU = 1
	}
	recMemMiB := recommendedMemoryMiB(recommendedMemoryGiB)

	if match := matchInCatalog(clusterTypes, recommendedVCPU, recMemMiB, preferredSeries, true); match != nil {
		return match
	}
	if match := matchInCatalog(vmInstanceCatalog, recommendedVCPU, recMemMiB, preferredSeries, true); match != nil {
		return match
	}
	return nil
}

// LookupInstanceTypeByName returns a catalog entry by name (cluster types, then global, then GPU).
func LookupInstanceTypeByName(name string, clusterTypes []InstanceType) *InstanceType {
	for _, catalog := range [][]InstanceType{clusterTypes, vmInstanceCatalog, vmInstanceCatalogGPU} {
		for i := range catalog {
			if catalog[i].Name == name {
				t := catalog[i]
				return &t
			}
		}
	}
	return nil
}

// RecognizeInstanceType finds the smallest catalog type matching current vCPU/memory (any series).
// Includes non-selectable n-series and gn-series for current_instance_type identification.
func RecognizeInstanceType(vcpu, memoryGiB int32, clusterTypes []InstanceType) *InstanceType {
	if vcpu < 1 {
		vcpu = 1
	}
	recMemMiB := recommendedMemoryMiB(memoryGiB)
	catalog := vmAllInstanceTypes(clusterTypes)

	var best *InstanceType
	var bestWaste int32 = math.MaxInt32
	for i := range catalog {
		t := &catalog[i]
		if t.VCPU < vcpu || memoryCapacityMiB(*t) < recMemMiB {
			continue
		}
		waste := instanceTypeWaste(*t, vcpu, recMemMiB)
		if best == nil || waste < bestWaste ||
			(waste == bestWaste && (t.VCPU < best.VCPU || (t.VCPU == best.VCPU && memoryCapacityMiB(*t) < memoryCapacityMiB(*best)))) {
			best = t
			bestWaste = waste
		}
	}
	return best
}
