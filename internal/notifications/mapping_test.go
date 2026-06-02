package notifications

import (
	"context"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapToKruizeFormat_EmptyCodes(t *testing.T) {
	result := MapToKruizeFormat(nil)
	assert.Nil(t, result)

	result = MapToKruizeFormat([]int16{})
	assert.Nil(t, result)
}

func TestMapToKruizeFormat_SingleCode(t *testing.T) {
	result := MapToKruizeFormat([]int16{3})
	require.Len(t, result, 1)

	entry, ok := result["3"]
	require.True(t, ok)
	assert.Equal(t, "CRITICAL", entry.Type)
	assert.Contains(t, entry.Message, "OOM")
	assert.Equal(t, int16(3), entry.Code)
}

func TestMapToKruizeFormat_MultipleCodes(t *testing.T) {
	result := MapToKruizeFormat([]int16{1, 5, 9})
	require.Len(t, result, 3)

	assert.Equal(t, "WARNING", result["1"].Type)
	assert.Equal(t, int16(1), result["1"].Code)

	assert.Equal(t, "INFO", result["5"].Type)
	assert.Equal(t, int16(5), result["5"].Code)

	assert.Equal(t, "WARNING", result["9"].Type)
	assert.Equal(t, int16(9), result["9"].Code)
}

func TestMapToKruizeFormat_UnknownCode_Skipped(t *testing.T) {
	result := MapToKruizeFormat([]int16{1, 99})
	require.Len(t, result, 1)
	_, ok := result["99"]
	assert.False(t, ok, "unknown code 99 should be skipped")
}

func TestMapToKruizeFormat_VMPlacementCodes60to63(t *testing.T) {
	result := MapToKruizeFormat([]int16{60, 61, 62, 63})
	require.Len(t, result, 4)

	assert.Equal(t, "WARNING", result["60"].Type)
	assert.Contains(t, result["60"].Message, "anti-affinity")
	assert.Equal(t, int16(60), result["60"].Code)

	assert.Equal(t, "INFO", result["61"].Type)
	assert.Contains(t, result["61"].Message, "topologySpreadConstraints")
	assert.Equal(t, int16(61), result["61"].Code)

	assert.Equal(t, "INFO", result["62"].Type)
	assert.Contains(t, result["62"].Message, "correlated")
	assert.Equal(t, int16(62), result["62"].Code)

	assert.Equal(t, "WARNING", result["63"].Type)
	assert.Contains(t, result["63"].Message, "NUMA")
	assert.Equal(t, int16(63), result["63"].Code)
}

func TestFleetConsolidationRecommendation(t *testing.T) {
	msg := FleetConsolidationRecommendation("worker-us-east-1a", 2)
	assert.Equal(t, "reduce MachineSet 'worker-us-east-1a' by 2 nodes", msg)
	assert.Empty(t, FleetConsolidationRecommendation("", 2))
	assert.Empty(t, FleetConsolidationRecommendation("worker-a", 0))
}

func TestMapToKruizeFormatForNode_FleetConsolidation(t *testing.T) {
	result := MapToKruizeFormatForNode([]int16{76}, nil, "worker-a", 2)
	require.NotNil(t, result)
	entry, ok := result["76"]
	require.True(t, ok)
	assert.Equal(t, "reduce MachineSet 'worker-a' by 2 nodes", entry.Message)
}

func TestNotificationDefinitionsComplete(t *testing.T) {
	require.NotEmpty(t, Definitions)
	for code, def := range Definitions {
		assert.Greater(t, code, int16(0), "notification code must be positive")
		assert.NotEmpty(t, def.Severity, "code %d severity", code)
		assert.NotEmpty(t, def.Message, "code %d message", code)
	}
	_, ok := Definitions[74]
	require.True(t, ok, "notification code 74 (node pod scheduling limit) should be defined")
	assert.Contains(t, Definitions[74].Message, "pod scheduling")
	_, ok = Definitions[76]
	require.True(t, ok, "notification code 76 (node fleet consolidation) should be defined")
}

func TestMapToKruizeFormat_NotifAIdle_Code15(t *testing.T) {
	result := MapToKruizeFormat([]int16{15})
	require.Len(t, result, 1)
	entry := result["15"]
	assert.Equal(t, "INFO", entry.Type)
	assert.Equal(t, int16(15), entry.Code)
	assert.Contains(t, entry.Message, "idle")
	assert.NotContains(t, entry.Message, "MachineAutoscaler")
	assert.NotContains(t, entry.Message, "minReplicas")
}

func TestMapToKruizeFormatForNode_StrandedCPU(t *testing.T) {
	cpu := "cpu"
	result := MapToKruizeFormatForNode([]int16{13}, &cpu, "", 0)
	require.NotNil(t, result)
	entry := result["13"]
	assert.Equal(t, "memory-optimized", entry.SuggestedDirection)
	assert.Contains(t, entry.Message, "memory-optimized")
}

func TestMapToKruizeFormatForNode_StrandedMemory(t *testing.T) {
	mem := "memory"
	result := MapToKruizeFormatForNode([]int16{13}, &mem, "", 0)
	require.NotNil(t, result)
	entry := result["13"]
	assert.Equal(t, "compute-optimized", entry.SuggestedDirection)
	assert.Contains(t, entry.Message, "compute-optimized")
}

func TestMapToKruizeFormat_AllDefinedCodes(t *testing.T) {
	var allCodes []int16
	for code := range Definitions {
		allCodes = append(allCodes, code)
	}

	result := MapToKruizeFormat(allCodes)
	require.Len(t, result, len(Definitions))

	for _, entry := range result {
		assert.NotEmpty(t, entry.Type)
		assert.NotEmpty(t, entry.Message)
		_, ok := Definitions[entry.Code]
		assert.True(t, ok, "mapped entry code %d should exist in Definitions", entry.Code)
	}
}

// TestDefinitionsMatchDB spins up a test database with migrations applied and
// verifies the Go Definitions map exactly matches notification_code_definitions.
// This catches drift if someone adds a code via migration without updating Go.
func TestDefinitionsMatchDB(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	rows, err := pool.Query(ctx,
		`SELECT code, severity, description FROM notification_code_definitions ORDER BY code`)
	require.NoError(t, err)
	defer rows.Close()

	dbCodes := make(map[int16]notifDef)
	for rows.Next() {
		var code int16
		var severity, description string
		require.NoError(t, rows.Scan(&code, &severity, &description))
		dbCodes[code] = notifDef{Severity: severity, Message: description}
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, dbCodes, "notification_code_definitions table should have seed data")

	for code, dbDef := range dbCodes {
		goDef, ok := Definitions[code]
		assert.True(t, ok, "DB code %d missing from Go Definitions map", code)
		if ok {
			assert.Equal(t, dbDef.Severity, goDef.Severity,
				"severity mismatch for code %d", code)
			assert.Equal(t, dbDef.Message, goDef.Message,
				"message mismatch for code %d", code)
		}
	}

	for code := range Definitions {
		_, ok := dbCodes[code]
		assert.True(t, ok, "Go code %d missing from DB notification_code_definitions", code)
	}

	assert.Equal(t, len(dbCodes), len(Definitions),
		"Go Definitions and DB notification_code_definitions should have same number of entries")
}
