package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestComputeGPUMIGStability(t *testing.T) {
	tests := []struct {
		name       string
		oldProfile string
		newProfile string
		want       float32
	}{
		{"same profile", "1g.5gb", "1g.5gb", 1.0},
		{"different profile", "1g.5gb", "2g.10gb", 0.0},
		{"empty old returns 1.0", "", "1g.5gb", 1.0},
		{"empty new returns 1.0", "1g.5gb", "", 1.0},
		{"both empty returns 1.0", "", "", 1.0},
		{"full_gpu same", "full_gpu", "full_gpu", 1.0},
		{"full_gpu to mig", "full_gpu", "1g.5gb", 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeGPUMIGStability(tt.oldProfile, tt.newProfile)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectGPUMIGAdoption(t *testing.T) {
	tests := []struct {
		name           string
		currentProfile string
		oldRecProfile  string
		wantAdopt      bool
	}{
		{"exact match", "1g.5gb", "1g.5gb", true},
		{"different", "2g.10gb", "1g.5gb", false},
		{"empty old rec", "1g.5gb", "", false},
		{"empty current, non-empty rec", "", "1g.5gb", false},
		{"both empty", "", "", false},
		{"full_gpu adopted", "full_gpu", "full_gpu", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectGPUMIGAdoption(tt.currentProfile, tt.oldRecProfile)
			assert.Equal(t, tt.wantAdopt, got)
		})
	}
}

func TestBuildGPUMIGQualityRows_Empty(t *testing.T) {
	rows := BuildGPUMIGQualityRows(nil, nil, "org1", "cluster-1", nil, nil, nil)
	assert.Nil(t, rows)
}

func TestBuildGPUMIGQualityRows_Basic(t *testing.T) {
	newRecs := map[GPUContainerKey][]*GPURec{
		{Namespace: "ns1", Workload: "wl1", ContainerName: "cn1"}: {
			{RecommendedGPUProfile: "1g.5gb", Term: "short"},
		},
	}
	oldRecs := map[gpuMIGQualityKey]OldGPUMIGRecommendation{
		{Namespace: "ns1", Workload: "wl1", ContainerName: "cn1"}: {
			RecommendedGPUProfile: "2g.10gb",
			UpdatedAt:             time.Now().UTC().Add(-48 * time.Hour),
		},
	}
	currentProfiles := map[gpuMIGQualityKey]string{
		{Namespace: "ns1", Workload: "wl1", ContainerName: "cn1"}: "2g.10gb",
	}

	rows := BuildGPUMIGQualityRows(nil, nil, "org1", "cluster-1", newRecs, oldRecs, currentProfiles)
	assert.Len(t, rows, 1)
	assert.Equal(t, "ns1", rows[0].Namespace)
	assert.Equal(t, "wl1", rows[0].Workload)
	assert.Equal(t, "cn1", rows[0].ContainerName)
	assert.Equal(t, float32(0.0), rows[0].StabilityPct)
	assert.True(t, rows[0].AdoptionDetected)
	assert.Equal(t, "cost", rows[0].Engine)
}

func TestBuildGPUMIGQualityRows_SkipsNoProfile(t *testing.T) {
	newRecs := map[GPUContainerKey][]*GPURec{
		{Namespace: "ns1", Workload: "wl1", ContainerName: "cn1"}: {
			{RecommendedGPUProfile: "", Term: "short"},
		},
	}
	rows := BuildGPUMIGQualityRows(nil, nil, "org1", "cluster-1", newRecs, nil, nil)
	assert.Len(t, rows, 0)
}
