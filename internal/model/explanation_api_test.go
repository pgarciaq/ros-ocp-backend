package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContainerExplanationAPI_JSONTags(t *testing.T) {
	dataDays := 7
	margin := int32(11500)
	expl := &ContainerExplanationAPI{
		DataDays:            &dataDays,
		CPUAdaptiveMarginBP: &margin,
	}
	b, err := json.Marshal(expl)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	require.Contains(t, m, "data_days")
	require.Contains(t, m, "cpu_adaptive_margin_basis_points")
	require.NotContains(t, m, "cpu_adaptive_margin_bp")
}

func TestBuildContainerExplanationAPI_NilWhenEmpty(t *testing.T) {
	row := NativeRecommendationRow{}
	require.Nil(t, BuildContainerExplanationAPI(row))
}

func TestBuildVMExplanationAPI_FromPersistedColumns(t *testing.T) {
	dataDays := 14
	branch := "active"
	rec := VMRecommendation{
		ExplDataDays:     &dataDays,
		ExplSizingBranch: &branch,
	}
	expl := BuildVMExplanationAPI(rec)
	require.NotNil(t, expl)
	require.Equal(t, 14, *expl.DataDays)
	require.Equal(t, "active", *expl.SizingBranch)
}
