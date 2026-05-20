package engine

import "testing"

func TestMatchGPUModel(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		wantKey   string // expected GPUModelSpec.Name, or "" for nil
	}{
		{"A100 SXM 80GB", "NVIDIA A100-SXM4-80GB", "A100_80GB"},
		{"A100 PCIe 40GB", "NVIDIA A100-PCIE-40GB", "A100_40GB"},
		{"H100 80GB", "NVIDIA H100 80GB HBM3", "H100_80GB"},
		{"H100 NVL 94GB", "NVIDIA H100 NVL", "H100_94GB"},
		{"H200", "NVIDIA H200", "H200_141GB"},
		{"B200", "NVIDIA B200", "B200_192GB"},
		{"T4", "NVIDIA T4", "T4"},
		{"V100 32GB", "Tesla V100-SXM2-32GB", "V100_32GB"},
		{"V100 16GB", "Tesla V100-PCIE-16GB", "V100_16GB"},
		{"P100", "Tesla P100-SXM2-16GB", "P100"},
		{"L4", "NVIDIA L4", "L4"},
		{"L40 not L40S", "NVIDIA L40", "L40"},
		{"L40S", "NVIDIA L40S", "L40S"},
		{"A10 not A100", "NVIDIA A10", "A10"},
		{"A10G distinct from A10", "NVIDIA A10G", "A10G"},
		{"A30", "NVIDIA A30", "A30"},
		{"P40", "NVIDIA Tesla P40", "P40"},
		{"unknown", "AMD Instinct MI300X", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchGPUModel(tt.modelName)
			if tt.wantKey == "" {
				if got != nil {
					t.Errorf("MatchGPUModel(%q) = %v, want nil", tt.modelName, got.Name)
				}
				return
			}
			if got == nil {
				t.Fatalf("MatchGPUModel(%q) = nil, want %q", tt.modelName, tt.wantKey)
			}
			if got.Name != tt.wantKey {
				t.Errorf("MatchGPUModel(%q).Name = %q, want %q", tt.modelName, got.Name, tt.wantKey)
			}
		})
	}
}

func TestGPUModelMIGProfiles(t *testing.T) {
	// Verify MIG-capable models have profiles
	migModels := []string{"A30", "A100_40GB", "A100_80GB", "H100_80GB", "H100_94GB", "H200_141GB", "B200_192GB"}
	for _, name := range migModels {
		spec := gpuModels[name]
		if !spec.MIGSupported {
			t.Errorf("%s: MIGSupported should be true", name)
		}
		if len(spec.Profiles) == 0 {
			t.Errorf("%s: expected MIG profiles, got none", name)
		}
	}
	// Verify non-MIG models have no profiles
	nonMIG := []string{"T4", "A10", "A10G", "L4", "L40", "L40S", "V100_16GB", "V100_32GB", "P100", "P40"}
	for _, name := range nonMIG {
		spec := gpuModels[name]
		if spec.MIGSupported {
			t.Errorf("%s: MIGSupported should be false", name)
		}
	}
}
