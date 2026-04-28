package engine

import "strings"

// GPUModelSpec describes the hardware capabilities of a GPU model.
type GPUModelSpec struct {
	Name               string       // canonical short name, e.g. "A100_80GB"
	TotalFBMiB         int          // total frame buffer in MiB
	SMCount            int          // number of streaming multiprocessors
	MIGSupported       bool         // whether MIG partitioning is available
	ProfilingSupported bool         // whether DCGM PROF_ metrics work (Turing+ datacenter)
	Profiles           []MIGProfile // available MIG profiles (empty if !MIGSupported)
}

// MIGProfile describes a single MIG partition configuration.
type MIGProfile struct {
	Name        string  // e.g. "1g.5gb", "3g.40gb"
	Slices      int     // number of GPU slices (1, 2, 3, 4, 7)
	FBSizeMiB   int     // frame buffer for this partition in MiB
	ComputeFrac float64 // fraction of full GPU compute capacity (Slices/7)
}

func migFrac(slices int) float64 {
	return float64(slices) / 7.0
}

// gpuModels maps canonical GPU keys to hardware specifications.
var gpuModels = map[string]GPUModelSpec{
	"T4": {
		Name: "T4", TotalFBMiB: 16384, SMCount: 40,
		MIGSupported: false, ProfilingSupported: true,
	},
	"A10": {
		Name: "A10", TotalFBMiB: 24576, SMCount: 72,
		MIGSupported: false, ProfilingSupported: true,
	},
	"A30": {
		Name: "A30", TotalFBMiB: 24576, SMCount: 56,
		MIGSupported: true, ProfilingSupported: true,
		Profiles: []MIGProfile{
			{"1g.6gb", 1, 6144, migFrac(1)},
			{"2g.12gb", 2, 12288, migFrac(2)},
			{"4g.24gb", 4, 24576, migFrac(4)},
		},
	},
	"A100_40GB": {
		Name: "A100_40GB", TotalFBMiB: 40960, SMCount: 108,
		MIGSupported: true, ProfilingSupported: true,
		Profiles: []MIGProfile{
			{"1g.5gb", 1, 5120, migFrac(1)},
			{"1g.10gb", 1, 10240, migFrac(1)},
			{"2g.10gb", 2, 10240, migFrac(2)},
			{"3g.20gb", 3, 20480, migFrac(3)},
			{"4g.20gb", 4, 20480, migFrac(4)},
			{"7g.40gb", 7, 40960, migFrac(7)},
		},
	},
	"A100_80GB": {
		Name: "A100_80GB", TotalFBMiB: 81920, SMCount: 108,
		MIGSupported: true, ProfilingSupported: true,
		Profiles: []MIGProfile{
			{"1g.10gb", 1, 10240, migFrac(1)},
			{"1g.20gb", 1, 20480, migFrac(1)},
			{"2g.20gb", 2, 20480, migFrac(2)},
			{"3g.40gb", 3, 40960, migFrac(3)},
			{"4g.40gb", 4, 40960, migFrac(4)},
			{"7g.80gb", 7, 81920, migFrac(7)},
		},
	},
	"L4": {
		Name: "L4", TotalFBMiB: 24576, SMCount: 60,
		MIGSupported: false, ProfilingSupported: true,
	},
	"L40": {
		Name: "L40", TotalFBMiB: 49152, SMCount: 142,
		MIGSupported: false, ProfilingSupported: true,
	},
	"L40S": {
		Name: "L40S", TotalFBMiB: 49152, SMCount: 142,
		MIGSupported: false, ProfilingSupported: true,
	},
	"H100_80GB": {
		Name: "H100_80GB", TotalFBMiB: 81920, SMCount: 132,
		MIGSupported: true, ProfilingSupported: true,
		Profiles: []MIGProfile{
			{"1g.10gb", 1, 10240, migFrac(1)},
			{"1g.20gb", 1, 20480, migFrac(1)},
			{"2g.20gb", 2, 20480, migFrac(2)},
			{"3g.40gb", 3, 40960, migFrac(3)},
			{"4g.40gb", 4, 40960, migFrac(4)},
			{"7g.80gb", 7, 81920, migFrac(7)},
		},
	},
	"H100_94GB": {
		Name: "H100_94GB", TotalFBMiB: 96256, SMCount: 132,
		MIGSupported: true, ProfilingSupported: true,
		Profiles: []MIGProfile{
			{"1g.12gb", 1, 12288, migFrac(1)},
			{"1g.24gb", 1, 24576, migFrac(1)},
			{"2g.24gb", 2, 24576, migFrac(2)},
			{"3g.48gb", 3, 49152, migFrac(3)},
			{"4g.48gb", 4, 49152, migFrac(4)},
			{"7g.94gb", 7, 96256, migFrac(7)},
		},
	},
	"H200_141GB": {
		Name: "H200_141GB", TotalFBMiB: 144384, SMCount: 132,
		MIGSupported: true, ProfilingSupported: true,
		Profiles: []MIGProfile{
			{"1g.18gb", 1, 18432, migFrac(1)},
			{"1g.35gb", 1, 35840, migFrac(1)},
			{"2g.35gb", 2, 35840, migFrac(2)},
			{"3g.71gb", 3, 72704, migFrac(3)},
			{"4g.71gb", 4, 72704, migFrac(4)},
			{"7g.141gb", 7, 144384, migFrac(7)},
		},
	},
	"B200_192GB": {
		Name: "B200_192GB", TotalFBMiB: 196608, SMCount: 160,
		MIGSupported: true, ProfilingSupported: true,
		Profiles: []MIGProfile{
			{"1g.24gb", 1, 24576, migFrac(1)},
			{"1g.48gb", 1, 49152, migFrac(1)},
			{"2g.48gb", 2, 49152, migFrac(2)},
			{"3g.96gb", 3, 98304, migFrac(3)},
			{"4g.96gb", 4, 98304, migFrac(4)},
			{"7g.192gb", 7, 196608, migFrac(7)},
		},
	},
	"V100_16GB": {
		Name: "V100_16GB", TotalFBMiB: 16384, SMCount: 80,
		MIGSupported: false, ProfilingSupported: false,
	},
	"V100_32GB": {
		Name: "V100_32GB", TotalFBMiB: 32768, SMCount: 80,
		MIGSupported: false, ProfilingSupported: false,
	},
	"P100": {
		Name: "P100", TotalFBMiB: 16384, SMCount: 56,
		MIGSupported: false, ProfilingSupported: false,
	},
	"P40": {
		Name: "P40", TotalFBMiB: 24576, SMCount: 30,
		MIGSupported: false, ProfilingSupported: false,
	},
}

// matchGPUModelKey resolves a lowercase model string to gpuModels lookup keys.
func matchGPUModelKey(lower string) string {
	if lower == "" {
		return ""
	}

	// NVIDIA / Tesla product lines — order is explicit (specific before general).
	switch {
	case strings.Contains(lower, "b200"):
		return "B200_192GB"
	case strings.Contains(lower, "h200"):
		return "H200_141GB"
	case strings.Contains(lower, "h100") && strings.Contains(lower, "nvl"):
		return "H100_94GB"
	case strings.Contains(lower, "h100") && strings.Contains(lower, "80gb"):
		return "H100_80GB"

	case strings.Contains(lower, "a100") && strings.Contains(lower, "80gb"):
		return "A100_80GB"
	case strings.Contains(lower, "a100") && strings.Contains(lower, "40gb"):
		return "A100_40GB"

	case strings.Contains(lower, "a30") && !strings.Contains(lower, "a300"):
		return "A30"
	case strings.Contains(lower, "a10") && !strings.Contains(lower, "a100"):
		return "A10"

	case strings.Contains(lower, "l40s"):
		return "L40S"
	case strings.Contains(lower, "l40") && !strings.Contains(lower, "l40s"):
		return "L40"
	case strings.Contains(lower, "l4") && !strings.Contains(lower, "l40"):
		return "L4"

	case strings.Contains(lower, "v100") && strings.Contains(lower, "32gb"):
		return "V100_32GB"
	case strings.Contains(lower, "v100") && strings.Contains(lower, "16gb"):
		return "V100_16GB"

	case strings.Contains(lower, "p100"):
		return "P100"
	case strings.Contains(lower, "p40") && !strings.Contains(lower, "p400"):
		return "P40"

	case strings.Contains(lower, "t4"):
		return "T4"
	default:
		return ""
	}
}

// MatchGPUModel resolves a DCGM-reported model name string to a GPUModelSpec.
// Returns nil if the GPU model is not recognized.
func MatchGPUModel(modelName string) *GPUModelSpec {
	s := strings.ToLower(strings.TrimSpace(modelName))
	key := matchGPUModelKey(s)
	if key == "" {
		return nil
	}
	spec := gpuModels[key]
	specCopy := spec
	return &specCopy
}
