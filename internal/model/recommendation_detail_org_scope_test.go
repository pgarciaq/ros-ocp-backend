package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
)

func TestDetailQueries_ScopeByOrgID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DryRun: true})
	require.NoError(t, err)

	database.DB = db
	t.Cleanup(func() { database.DB = nil })

	const orgID = "1234567"
	const recID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	t.Run("legacy container detail", func(t *testing.T) {
		sql := getRecommendationQuery(orgID).
			Where("recommendation_sets.container_id = ?", recID).
			ToSQL(func(tx *gorm.DB) *gorm.DB {
				return tx.First(nil)
			})
		assert.Contains(t, strings.ToLower(sql), "org_id")
		assert.Contains(t, sql, orgID)
		assert.NotContains(t, strings.ToLower(sql), "rh_accounts")
	})

	t.Run("legacy namespace detail", func(t *testing.T) {
		sql := getNamespaceRecommendationQuery(orgID).
			Where("namespace_recommendation_sets.id = ?", recID).
			ToSQL(func(tx *gorm.DB) *gorm.DB {
				return tx.First(nil)
			})
		assert.Contains(t, strings.ToLower(sql), "org_id")
		assert.Contains(t, sql, orgID)
	})

	t.Run("native container detail", func(t *testing.T) {
		sql := nativeContainerDetailQuery(db, orgID, recID, map[string][]string{"*": {}}).
			ToSQL(func(tx *gorm.DB) *gorm.DB {
				return tx.Find(nil)
			})
		assert.Contains(t, strings.ToLower(sql), "org_id")
		assert.Contains(t, sql, orgID)
		assert.Contains(t, sql, recID)
		assert.NotContains(t, strings.ToLower(sql), "rh_accounts")
	})

	t.Run("native namespace detail", func(t *testing.T) {
		sql := nativeNamespaceDetailQuery(db, orgID, recID, map[string][]string{"*": {}}).
			ToSQL(func(tx *gorm.DB) *gorm.DB {
				return tx.Find(nil)
			})
		assert.Contains(t, strings.ToLower(sql), "org_id")
		assert.Contains(t, sql, orgID)
		assert.Contains(t, sql, recID)
		assert.NotContains(t, strings.ToLower(sql), "rh_accounts")
	})
}
