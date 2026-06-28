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
		{"B200", "NVIDIA B200", "B200_180GB"},
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
		{"bare A100", "A100", "A100_80GB"},
		{"bare H100", "H100", "H100_80GB"},
		{"bare V100", "V100", "V100_32GB"},
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

func TestGPUModelCount(t *testing.T) {
	count := GPUModelCount()
	if count < 16 {
		t.Errorf("GPUModelCount() = %d, want at least 16 (catalog may be missing entries)", count)
	}
}

func TestGPUCatalogMIGProfileNames(t *testing.T) {
	// Profile names must match NVIDIA MIG User Guide (docs.nvidia.com/datacenter/tesla/mig-user-guide).
	tests := []struct {
		modelKey string
		want     []string
	}{
		{
			modelKey: "H100_94GB",
			want:     []string{"1g.12gb", "1g.24gb", "2g.24gb", "3g.47gb", "4g.47gb", "7g.94gb"},
		},
		{
			modelKey: "B200_180GB",
			want:     []string{"1g.23gb", "1g.45gb", "2g.45gb", "3g.90gb", "4g.90gb", "7g.180gb"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.modelKey, func(t *testing.T) {
			spec := gpuModels[tt.modelKey]
			got := make([]string, len(spec.Profiles))
			for i, p := range spec.Profiles {
				got[i] = p.Name
			}
			if len(got) != len(tt.want) {
				t.Fatalf("profile count = %d, want %d: got %v want %v", len(got), len(tt.want), got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("profiles[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGPUModelMIGProfiles(t *testing.T) {
	// Verify MIG-capable models have profiles
	migModels := []string{"A30", "A100_40GB", "A100_80GB", "H100_80GB", "H100_94GB", "H200_141GB", "B200_180GB"}
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
