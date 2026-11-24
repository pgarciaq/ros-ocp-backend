package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
)

// #926: filter[node] on the VM list must narrow, not 503. node_name is
// carried into vm_recommendations (the digests always had it — the rec
// struct dropped it); the response serves the field.
func TestVMRecommendations_FilterNode(t *testing.T) {
	orgID := "org-vm-node-filter-" + uuid.New().String()[:8]
	e := setupVMRecommendationsHandler(t, orgID)
	ctx := context.Background()

	clusterUUID := "00000000-0000-0000-0000-000000000099"
	_, err := database.Pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = database.Pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'vm-node-filter', 1, NOW()) ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)

	// Two VMs on different nodes; the filter must keep only worker-a's.
	_, err = database.Pool.Exec(ctx, `
		INSERT INTO vm_recommendations (
			org_id, cluster_uuid, vm_name, namespace, node_name, guest_os,
			current_vcpu, current_memory_gib, recommended_vcpu, recommended_memory_gib,
			guest_agent_detected, confidence, term, engine,
			category, estimated_savings_cents, savings_currency, last_recommended_at
		) VALUES (
			$1, $2, 'vm-on-a', 'prod', 'worker-a', 'linux',
			4, 16, 2, 8,
			true, 'high', 'medium_term', 'cost',
			'optimized', 10.00, 'USD', now()),
			($1, $2, 'vm-on-b', 'prod', 'worker-b', 'linux',
			4, 16, 2, 8,
			true, 'high', 'medium_term', 'cost',
			'optimized', 20.00, 'USD', now())`,
		orgID, clusterUUID)
	require.NoError(t, err, "seeding vm_recommendations with node_name — a failure here means the #926 migration has not run")

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vm?filter[node]=worker-a&limit=10", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code,
		"filter[node] must not 503 — node_name must be carried into vm_recommendations (#926): %s", rec.Body.String())

	var resp VMRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count, "filter[node]=worker-a must narrow to worker-a's VMs only (#926)")
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "vm-on-a", resp.Data[0].VMName)
	assert.Equal(t, "worker-a", resp.Data[0].NodeName,
		"the VM list response must serve node_name (#926)")
}
