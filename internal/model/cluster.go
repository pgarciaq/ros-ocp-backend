package model

import (
	"fmt"
	"strings"
	"time"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Cluster struct {
	ID                    uint `gorm:"primaryKey;not null;autoIncrement"`
	TenantID              uint
	RHAccount             RHAccount `gorm:"foreignKey:TenantID"`
	OrgID                 string    `gorm:"column:org_id"`
	SourceId              string    `gorm:"type:text;unique"`
	ClusterUUID           string    `gorm:"type:text;unique"`
	ClusterAlias          string    `gorm:"type:text;unique"`
	LastReportedAt        time.Time
	LastReportedAtStr     string     `gorm:"-"`
	AnalyticsIncomplete   bool       `gorm:"column:analytics_incomplete"`
	AnalyticsIncompleteAt *time.Time `gorm:"column:analytics_incomplete_at"`
	IngestHooksFailed     bool       `gorm:"column:ingest_hooks_failed"`
	IngestHooksFailedAt   *time.Time `gorm:"column:ingest_hooks_failed_at"`
}

func (c *Cluster) AfterFind(tx *gorm.DB) error {
	c.LastReportedAtStr = c.LastReportedAt.Format(time.RFC3339)
	return nil
}

func (c *Cluster) CreateCluster() error {
	if strings.TrimSpace(c.OrgID) == "" {
		return fmt.Errorf("create cluster: org_id is required")
	}
	db := database.GetDB()
	// #551: trust rh_accounts, not the caller. The ON CONFLICT target below
	// includes tenant_id, so a mismatched caller org would otherwise repoint
	// the row's org_id (the #508 pattern). Look up the tenant's truth first:
	// a match proceeds (and the DoUpdates org_id assignment converges a
	// diverged stored value back to truth — the only heal, since the 000191
	// trigger fills only NULL/empty and 000192 made the column NOT NULL);
	// a mismatch is rejected with no write. org_id stays in DoUpdates
	// deliberately for heal-on-match — the gate above, not the column list,
	// is what prevents repointing (corrects the issue's rec-1 "drop org_id",
	// which would cement a wrong first write forever). One PK lookup per
	// Kafka message; never per row. No new metric: a reject is not a DB
	// failure (dbError would mislabel it) and org_id must never become a
	// label value; the caller already Error-logs the full struct plus this
	// error, which names tenant, UUID, and both org values.
	var truth RHAccount
	if res := db.Where("id = ?", c.TenantID).First(&truth); res.Error != nil {
		return fmt.Errorf("create cluster: rh_accounts %d not found: %w", c.TenantID, res.Error)
	}
	if truth.OrgId != c.OrgID {
		return fmt.Errorf("create cluster: org_id mismatch for tenant %d cluster %s: got %q, want %q (rejecting repoint)",
			c.TenantID, c.ClusterUUID, c.OrgID, truth.OrgId)
	}
	result := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "source_id"}, {Name: "cluster_uuid"}, {Name: "cluster_alias"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_reported_at", "org_id"}),
	}).Create(c)

	if result.Error != nil {
		dbError.Inc()
		return result.Error
	}
	return nil
}

func (c *Cluster) DeleteCluster() error {
	db := database.GetDB()
	result := db.Where("source_id = ?", c.SourceId).Delete(c)
	if result.Error != nil {
		dbError.Inc()
		return result.Error
	}
	return nil
}
