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

func TestNotificationDefinitionsComplete(t *testing.T) {
	for code := int16(1); code <= 24; code++ {
		_, ok := Definitions[code]
		assert.True(t, ok, "notification code %d should be defined", code)
	}
}

func TestMapToKruizeFormat_AllDefinedCodes(t *testing.T) {
	var allCodes []int16
	for code := int16(1); code <= 24; code++ {
		allCodes = append(allCodes, code)
	}

	result := MapToKruizeFormat(allCodes)
	require.Len(t, result, 24)

	for _, entry := range result {
		assert.NotEmpty(t, entry.Type)
		assert.NotEmpty(t, entry.Message)
		assert.True(t, entry.Code >= 1 && entry.Code <= 24)
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
