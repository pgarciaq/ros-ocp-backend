package costgroups

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const maxEntriesPerSync = 10000

// SyncEntry is one admin-curated platform namespace. Prefix reports a
// koku `%`-wildcard entry (matches namespace prefixes); exact otherwise.
// SystemDefault records provenance (shipped default vs admin-added) for
// debugging only — both exclude identically.
type SyncEntry struct {
	Namespace     string `json:"namespace"`
	IsPrefix      bool   `json:"is_prefix"`
	SystemDefault bool   `json:"system_default"`
}

// SyncRequest is the koku push body (mirrors the tag-sync shape:
// org + timestamp + entries).
type SyncRequest struct {
	OrgID    string      `json:"org_id"`
	SyncedAt string      `json:"synced_at"`
	Entries  []SyncEntry `json:"entries"`
}

// SyncResponse reports how many rows the org now holds.
type SyncResponse struct {
	Updated int `json:"updated"`
}

func parseSyncedAt(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("synced_at is required")
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z07:00", time.DateTime} {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts, nil
		}
	}
	return time.Time{}, fmt.Errorf("synced_at %q is not a recognized timestamp", raw)
}

// ValidateSyncRequest enforces org, timestamp, size, and entry shape.
// Namespace rules mirror koku's own constraints loosely (non-empty,
// bounded): strictness here must never reject a legitimate admin edit.
func ValidateSyncRequest(req SyncRequest) error {
	orgID := strings.TrimSpace(req.OrgID)
	if orgID == "" {
		return fmt.Errorf("org_id is required")
	}
	if len(orgID) > 64 {
		return fmt.Errorf("org_id exceeds maximum length of 64 characters")
	}
	if _, err := parseSyncedAt(req.SyncedAt); err != nil {
		return err
	}
	if len(req.Entries) > maxEntriesPerSync {
		return fmt.Errorf("entries exceeds maximum of %d entries", maxEntriesPerSync)
	}
	for i, e := range req.Entries {
		ns := strings.TrimSpace(e.Namespace)
		if ns == "" {
			return fmt.Errorf("entries[%d]: namespace is required", i)
		}
		if len(ns) > 253 {
			return fmt.Errorf("entries[%d]: namespace exceeds maximum length", i)
		}
		if strings.ContainsAny(ns, " \t\n") && !e.IsPrefix {
			return fmt.Errorf("entries[%d]: exact namespace must not contain whitespace", i)
		}
	}
	return nil
}

// SyncService persists pushed cost-group platform namespaces.
type SyncService struct {
	pool *pgxpool.Pool
}

// NewSyncService constructs the service; nil pool fails at call time,
// never at construction (mirrors tags.SyncService).
func NewSyncService(pool *pgxpool.Pool) *SyncService {
	return &SyncService{pool: pool}
}

// SyncOrgCostGroups replaces the org's platform-namespace set wholesale
// (DELETE + INSERT in one tx). Replace — not upsert — because admin
// REMOVALS must stop excluding: a removed project left behind would
// silence zombies on live namespaces, the unsafe direction.
func (s *SyncService) SyncOrgCostGroups(ctx context.Context, req SyncRequest) (int, error) {
	if s == nil || s.pool == nil {
		return 0, fmt.Errorf("cost-groups sync service is not configured")
	}
	orgID := strings.TrimSpace(req.OrgID)
	if orgID == "" {
		return 0, fmt.Errorf("org_id is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin cost-groups sync tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM hcp_platform_namespaces WHERE org_id = $1`, orgID); err != nil {
		return 0, fmt.Errorf("clear cost-groups rows: %w", err)
	}
	for _, e := range req.Entries {
		ns := strings.TrimSpace(e.Namespace)
		if ns == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO hcp_platform_namespaces (org_id, namespace, is_prefix, system_default, fetched_at)
			VALUES ($1, $2, $3, $4, now())`, orgID, ns, e.IsPrefix, e.SystemDefault); err != nil {
			return 0, fmt.Errorf("upsert cost-groups row: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit cost-groups sync tx: %w", err)
	}
	return len(req.Entries), nil
}

// LoadOrgPlatformNamespaces returns the org's synced exclusion set as
// exact names and prefix stems (trailing % stripped at read; stored
// verbatim). Empty (nil, nil) means never synced — callers fall back to
// compiled defaults, never error.
func LoadOrgPlatformNamespaces(ctx context.Context, pool *pgxpool.Pool, orgID string) (exact map[string]bool, prefixes []string, err error) {
	rows, err := pool.Query(ctx, `
		SELECT namespace, is_prefix FROM hcp_platform_namespaces WHERE org_id = $1`, orgID)
	if err != nil {
		return nil, nil, fmt.Errorf("load platform namespaces: %w", err)
	}
	defer rows.Close()
	exact = map[string]bool{}
	for rows.Next() {
		var ns string
		var isPrefix bool
		if err := rows.Scan(&ns, &isPrefix); err != nil {
			return nil, nil, fmt.Errorf("scan platform namespace: %w", err)
		}
		if isPrefix {
			prefixes = append(prefixes, strings.TrimSuffix(ns, "%"))
		} else {
			exact[ns] = true
		}
	}
	return exact, prefixes, rows.Err()
}
