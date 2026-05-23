package pvc

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

func TestPVCPlugin_traitAssertions(t *testing.T) {
	t.Parallel()

	var (
		_ plugin.Plugin            = (*PVCPlugin)(nil)
		_ plugin.CSVIngestor       = (*PVCPlugin)(nil)
		_ plugin.APIProvider       = (*PVCPlugin)(nil)
		_ plugin.RetentionProvider = (*PVCPlugin)(nil)
	)
}

func TestPVCPlugin_nameTypesRetentionTables(t *testing.T) {
	t.Parallel()

	p := &PVCPlugin{}
	assert.Equal(t, "pvc", p.Name())
	assert.Equal(t, []string{string(types.PayloadTypeStorage)}, p.SupportedCSVTypes())
	assert.Equal(t, []string{"daily_pvc_digests"}, p.RetentionTables())
}

// BH-UNIT-111: v1 PVC ingestion must not add schedule_type to digest upserts.
func TestPVCPlugin_V1_NoScheduleType(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	pvcGo := filepath.Join(filepath.Dir(thisFile), "..", "..", "ingestion", "pvc.go")
	body, err := os.ReadFile(pvcGo)
	require.NoError(t, err)
	src := string(body)
	assert.NotContains(t, src, "schedule_type")
	assert.NotContains(t, src, "business_hours")
}
