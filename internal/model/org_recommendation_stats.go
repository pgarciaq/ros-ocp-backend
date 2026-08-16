package model

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
	"gorm.io/gorm"
)

// OrgRecommendationStats holds pre-computed list counts for an org.
type OrgRecommendationStats struct {
	OrgID          string `gorm:"column:org_id"`
	ContainerCount int64  `gorm:"column:container_count"`
	NamespaceCount int64  `gorm:"column:namespace_count"`
}

func (OrgRecommendationStats) TableName() string {
	return "org_recommendation_stats"
}

// NativeListPage is the paginated result from native list queries.
type NativeListPage struct {
	Results []NativeContainerResult
	Count   int
	HasNext bool
	// LastAnchor is set when HasNext is true for keyset cursor encoding.
	LastAnchor *ContainerPaginationAnchor
}

// NativeNamespaceListPage is the paginated result from native namespace list queries.
type NativeNamespaceListPage struct {
	Results []NativeNamespaceResult
	Count   int
	HasNext bool
	// LastAnchor is set when HasNext is true for keyset cursor encoding.
	LastAnchor *NamespacePaginationAnchor
}

// GetOrgContainerCount returns the pre-computed container count for an org when available.
func GetOrgContainerCount(orgID string) (int64, bool, error) {
	db := database.GetDB()
	var stats OrgRecommendationStats
	err := db.Where("org_id = ?", orgID).First(&stats).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return stats.ContainerCount, true, nil
}

// GetOrgNamespaceCount returns the pre-computed namespace count for an org when available.
func GetOrgNamespaceCount(orgID string) (int64, bool, error) {
	db := database.GetDB()
	var stats OrgRecommendationStats
	err := db.Where("org_id = ?", orgID).First(&stats).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return stats.NamespaceCount, true, nil
}

// RefreshOrgRecommendationStats recomputes and upserts org list counts.
func RefreshOrgRecommendationStats(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	return pgrec.RefreshOrgRecommendationStats(ctx, pool, orgID)
}

// RefreshOrgRecommendationStatsTx is like RefreshOrgRecommendationStats but uses an existing tx.
func RefreshOrgRecommendationStatsTx(ctx context.Context, tx pgx.Tx, orgID string) error {
	return pgrec.RefreshOrgRecommendationStatsTx(ctx, tx, orgID)
}
