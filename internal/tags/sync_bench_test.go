package tags_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// BenchmarkSyncOrgTags is #814's committed write-path benchmark (the D1
// spike's write measurement, made permanent): a full-replace sync of 10k
// namespace rows through the real SyncOrgTags — the reset-to-'{}' plus the
// arrayed upsert. This is the status-quo write cost the D1 decision
// recorded; a future skip-unchanged optimization would be measured against
// this baseline.
func BenchmarkSyncOrgTags(b *testing.B) {
	const rows = 10000
	pool := testutil.SetupTestDB(b)
	ctx := context.Background()

	// Seed the rows the sync updates (one container key per namespace).
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name)
		SELECT $1, $2, 'bench-sync-ns-' || n, 'w', 'Deployment', 'c'
		FROM generate_series(1, %d) n
		ON CONFLICT DO NOTHING
	`, rows), testutil.TestOrgID, testutil.TestClusterUUID); err != nil {
		b.Fatal(err)
	}

	// The payload: three tag keys per namespace, mixed shapes arriving
	// (scalars and arrays) — normalization runs on every value.
	namespaceTags := make([]tags.NamespaceTags, 0, rows)
	for n := 1; n <= rows; n++ {
		shape := n % 3
		var t map[string]tags.TagValue
		switch shape {
		case 0:
			t = map[string]tags.TagValue{
				"environment": {"production"},
				"team":        {"platform"},
				"tier":        {"silver", "gold"},
			}
		case 1:
			t = map[string]tags.TagValue{
				"environment": {"staging", "production"},
				"team":        {"billing"},
			}
		default:
			t = map[string]tags.TagValue{"environment": {"dev"}}
		}
		namespaceTags = append(namespaceTags, tags.NamespaceTags{
			ClusterUUID: testutil.TestClusterUUID,
			Namespace:   fmt.Sprintf("bench-sync-ns-%d", n),
			Tags:        t,
		})
	}

	svc := tags.NewSyncService(pool)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		updated, err := svc.SyncOrgTags(ctx, tags.SyncRequest{
			OrgID:         testutil.TestOrgID,
			SyncedAt:      "2026-11-18T12:00:00Z",
			NamespaceTags: namespaceTags,
		})
		require.NoError(b, err)
		require.Equal(b, rows, updated)
	}
}
