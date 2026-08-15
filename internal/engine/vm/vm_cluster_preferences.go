package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UpsertClusterVMPreferencesMeta stores preference catalog data for a cluster.
func UpsertClusterVMPreferencesMeta(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID string,
	clusterUUID uuid.UUID,
	prefs []ClusterPreferenceRecord,
	vmPrefs map[string]string,
	collectedAt time.Time,
) error {
	if collectedAt.IsZero() {
		collectedAt = time.Now().UTC()
	}
	if prefs == nil {
		prefs = []ClusterPreferenceRecord{}
	}
	if vmPrefs == nil {
		vmPrefs = map[string]string{}
	}
	prefsJSON, err := json.Marshal(prefs)
	if err != nil {
		return fmt.Errorf("marshal preferences: %w", err)
	}
	vmPrefsJSON, err := json.Marshal(vmPrefs)
	if err != nil {
		return fmt.Errorf("marshal vm_preferences: %w", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO cluster_vm_preferences_meta (org_id, cluster_uuid, preferences, vm_preferences, collected_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (org_id, cluster_uuid) DO UPDATE SET
			preferences = EXCLUDED.preferences,
			vm_preferences = EXCLUDED.vm_preferences,
			collected_at = EXCLUDED.collected_at
	`, orgID, clusterUUID, prefsJSON, vmPrefsJSON, collectedAt)
	if err != nil {
		return fmt.Errorf("upsert cluster vm preferences meta: %w", err)
	}
	return nil
}

// QueryClusterVMPreferences loads preference context for recommendation matching.
func QueryClusterVMPreferences(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUID uuid.UUID) (*VMPreferenceContext, error) {
	var prefsJSON, vmPrefsJSON []byte
	err := pool.QueryRow(ctx, `
		SELECT preferences, vm_preferences
		FROM cluster_vm_preferences_meta
		WHERE org_id = $1 AND cluster_uuid = $2
	`, orgID, clusterUUID).Scan(&prefsJSON, &vmPrefsJSON)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query cluster vm preferences meta: %w", err)
	}
	var prefs []ClusterPreferenceRecord
	if len(prefsJSON) > 0 {
		if err := json.Unmarshal(prefsJSON, &prefs); err != nil {
			return nil, fmt.Errorf("decode preferences: %w", err)
		}
	}
	var vmPrefs map[string]string
	if len(vmPrefsJSON) > 0 {
		if err := json.Unmarshal(vmPrefsJSON, &vmPrefs); err != nil {
			return nil, fmt.Errorf("decode vm_preferences: %w", err)
		}
	}
	return buildVMPreferenceContext(prefs, vmPrefs), nil
}

// ClusterVMPreferencesSummary is a lightweight catalog summary for API responses.
type ClusterVMPreferencesSummary struct {
	PreferenceCount   int
	VMPreferenceCount int
	HasPreferences    bool
}

// QueryClusterVMPreferencesSummary returns counts for the instance-types API.
func QueryClusterVMPreferencesSummary(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUID uuid.UUID) (ClusterVMPreferencesSummary, error) {
	var prefsJSON, vmPrefsJSON []byte
	err := pool.QueryRow(ctx, `
		SELECT preferences, vm_preferences
		FROM cluster_vm_preferences_meta
		WHERE org_id = $1 AND cluster_uuid = $2
	`, orgID, clusterUUID).Scan(&prefsJSON, &vmPrefsJSON)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ClusterVMPreferencesSummary{}, nil
		}
		return ClusterVMPreferencesSummary{}, fmt.Errorf("query cluster vm preferences summary: %w", err)
	}
	var prefs []ClusterPreferenceRecord
	_ = json.Unmarshal(prefsJSON, &prefs)
	var vmPrefs map[string]string
	_ = json.Unmarshal(vmPrefsJSON, &vmPrefs)
	return ClusterVMPreferencesSummary{
		PreferenceCount:   len(prefs),
		VMPreferenceCount: len(vmPrefs),
		HasPreferences:    len(prefs) > 0 || len(vmPrefs) > 0,
	}, nil
}
