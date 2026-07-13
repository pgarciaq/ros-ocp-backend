package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ThresholdSettingsResolverFunc resolves settings for a recommendation type and
// returns them as an any (for JSON serialization in hash computation).
type ThresholdSettingsResolverFunc func(ctx context.Context, pool *pgxpool.Pool, orgID string) (any, error)

var (
	settingsResolversMu sync.RWMutex
	settingsResolvers   = map[string]ThresholdSettingsResolverFunc{}
)

// RegisterSettingsResolver registers a settings resolver for a recommendation type.
// Sub-packages call this during init() to avoid import cycles.
func RegisterSettingsResolver(recType string, fn ThresholdSettingsResolverFunc) {
	settingsResolversMu.Lock()
	settingsResolvers[recType] = fn
	settingsResolversMu.Unlock()
}

func computeThresholdSettingsHash(ctx context.Context, pool *pgxpool.Pool, orgID, recType string) (string, error) {
	var payload any
	var err error
	switch recType {
	case "container":
		var s SizingThresholdSettings
		s, err = ResolveContainerSizingThresholds(ctx, pool, orgID)
		payload = s
	case "namespace":
		var s SizingThresholdSettings
		s, err = ResolveNamespaceSizingThresholds(ctx, pool, orgID)
		payload = s
	case "node":
		var s NodeThresholdSettings
		s, err = ResolveNodeThresholdSettings(ctx, pool, orgID)
		payload = s
	case "gpu":
		var s GPUThresholdSettings
		s, err = ResolveGPUThresholdSettings(ctx, pool, orgID)
		payload = s
	case "pvc":
		var s PVCThresholdSettings
		s, err = ResolvePVCThresholdSettings(ctx, pool, orgID)
		payload = s
	case "quota":
		var s QuotaSettings
		s, err = ResolveQuotaSettings(ctx, pool, orgID)
		payload = s
	case "cluster-quota":
		var s ClusterQuotaSettings
		s, err = ResolveClusterQuotaSettings(ctx, pool, orgID)
		payload = s
	default:
		settingsResolversMu.RLock()
		fn := settingsResolvers[recType]
		settingsResolversMu.RUnlock()
		if fn != nil {
			payload, err = fn(ctx, pool, orgID)
		} else {
			return "", fmt.Errorf("unsupported recommendation_type %q", recType)
		}
	}
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal threshold settings: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func getClusterRecalcHash(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, recType string) (string, bool, error) {
	var hash string
	err := pool.QueryRow(ctx, `
		SELECT thresholds_hash FROM cluster_threshold_recalc_state
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND recommendation_type = $3`,
		orgID, clusterUUID, recType,
	).Scan(&hash)
	if err == pgx.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get cluster recalc hash: %w", err)
	}
	return hash, true, nil
}

func setClusterRecalcHash(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, recType, hash string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO cluster_threshold_recalc_state (org_id, cluster_uuid, recommendation_type, thresholds_hash, updated_at)
		VALUES ($1, $2::uuid, $3, $4, NOW())
		ON CONFLICT (org_id, cluster_uuid, recommendation_type)
		DO UPDATE SET thresholds_hash = EXCLUDED.thresholds_hash, updated_at = NOW()`,
		orgID, clusterUUID, recType, hash,
	)
	if err != nil {
		return fmt.Errorf("set cluster recalc hash: %w", err)
	}
	return nil
}

func shouldSkipClusterThresholdRecalc(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, recType, currentHash string) (bool, error) {
	stored, ok, err := getClusterRecalcHash(ctx, pool, orgID, clusterUUID, recType)
	if err != nil || !ok {
		return false, err
	}
	return stored == currentHash, nil
}
