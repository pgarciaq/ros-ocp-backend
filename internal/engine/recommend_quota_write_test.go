package engine

import (
	"context"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteQuotaRecommendations_PersistsQuotaID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const (
		org       = "org-write-quota-test"
		cluster   = "550e8400-e29b-41d4-a716-446655440001"
		namespace = "write-ns"
		quotaName = "write-budget"
	)

	rec := QuotaRec{
		OrgID:              org,
		ClusterUUID:        cluster,
		Namespace:          namespace,
		QuotaName:          quotaName,
		HeadroomBP:         11000,
		RecommendationType: QuotaRecTypeTighten,
		RiskLevel:          QuotaRiskLow,
		Currency:           "USD",
		NotificationCodes:  []int16{},
		Snapshot: NamespaceQuotaSnapshot{
			CPURequestHardMC: 100000,
			CPURequestUsedMC: 25000,
			LastObservedAt:   time.Now().UTC().Truncate(time.Second),
		},
		Recommended: QuotaResourceBundle{
			CPURequestMillicores: 36000,
		},
		CapacityFreed: QuotaCapacityFreed{CPUMillicores: 64000},
	}

	err := WriteQuotaRecommendations(ctx, pool, []QuotaRec{rec})
	require.NoError(t, err)

	var gotQuotaID *string
	err = pool.QueryRow(ctx, `
		SELECT quota_id FROM quota_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = $3 AND quota_name = $4`,
		org, cluster, namespace, quotaName,
	).Scan(&gotQuotaID)
	require.NoError(t, err)

	expectedID := model.NativeQuotaID(cluster, namespace, quotaName)
	require.NotNil(t, gotQuotaID, "quota_id should be populated by WriteQuotaRecommendations")
	assert.Equal(t, expectedID, *gotQuotaID)
}

func TestWriteQuotaRecommendations_UpsertUpdatesQuotaID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const (
		org       = "org-write-quota-upsert"
		cluster   = "550e8400-e29b-41d4-a716-446655440002"
		namespace = "upsert-ns"
		quotaName = "upsert-budget"
	)

	// Pre-insert a row WITHOUT quota_id (simulates pre-migration data).
	_, err := pool.Exec(ctx, `
		INSERT INTO quota_recommendation_sets (
			org_id, cluster_uuid, namespace, quota_name,
			recommendation_type, risk_level, last_observed_at
		) VALUES ($1, $2::uuid, $3, $4, 'optimal', 'low', NOW())`,
		org, cluster, namespace, quotaName,
	)
	require.NoError(t, err)

	var preID *string
	err = pool.QueryRow(ctx, `
		SELECT quota_id FROM quota_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = $3 AND quota_name = $4`,
		org, cluster, namespace, quotaName,
	).Scan(&preID)
	require.NoError(t, err)
	assert.Nil(t, preID, "quota_id should be NULL before UPSERT")

	rec := QuotaRec{
		OrgID:              org,
		ClusterUUID:        cluster,
		Namespace:          namespace,
		QuotaName:          quotaName,
		HeadroomBP:         11000,
		RecommendationType: QuotaRecTypeTighten,
		RiskLevel:          QuotaRiskLow,
		Currency:           "USD",
		NotificationCodes:  []int16{},
		Snapshot: NamespaceQuotaSnapshot{
			CPURequestHardMC: 100000,
			CPURequestUsedMC: 25000,
			LastObservedAt:   time.Now().UTC().Truncate(time.Second),
		},
		Recommended: QuotaResourceBundle{CPURequestMillicores: 36000},
		CapacityFreed: QuotaCapacityFreed{CPUMillicores: 64000},
	}

	err = WriteQuotaRecommendations(ctx, pool, []QuotaRec{rec})
	require.NoError(t, err)

	var postID *string
	err = pool.QueryRow(ctx, `
		SELECT quota_id FROM quota_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = $3 AND quota_name = $4`,
		org, cluster, namespace, quotaName,
	).Scan(&postID)
	require.NoError(t, err)

	expectedID := model.NativeQuotaID(cluster, namespace, quotaName)
	require.NotNil(t, postID, "UPSERT should populate quota_id on existing rows")
	assert.Equal(t, expectedID, *postID)
}
