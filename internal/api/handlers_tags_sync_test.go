package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestPostTagsSync_FeatureGateAndAuth(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	e := echo.New()
	e.POST("/internal/tags/sync", api.PostTagsSync)

	body, err := json.Marshal(tags.SyncRequest{
		OrgID:    testutil.TestOrgID,
		SyncedAt: "2026-05-25T12:00:00Z",
		TagKeys: []tags.TagKeyCatalog{
			{Key: "environment", Values: []string{"prod"}},
		},
		NamespaceTags: []tags.NamespaceTags{
			{
				ClusterUUID: testutil.TestClusterUUID,
				Namespace:   "ns",
				Tags:        map[string]tags.TagValue{"environment": {"prod"}},
			},
		},
	})
	require.NoError(t, err)

	t.Run("disabled returns 404", func(t *testing.T) {
		config.ResetTagsForTest()
		t.Setenv("ROS_TAGS_ENABLED", "false")

		req := httptest.NewRequest(http.MethodPost, "/internal/tags/sync", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("enabled without token returns 401", func(t *testing.T) {
		config.ResetTagsForTest()
		tags.ResetProviderForTest()
		t.Setenv("ROS_TAGS_ENABLED", "true")
		t.Setenv("ROS_TAGS_SOURCE", "api")

		req := httptest.NewRequest(http.MethodPost, "/internal/tags/sync", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("enabled with dev token updates rows", func(t *testing.T) {
		config.ResetTagsForTest()
		tags.ResetProviderForTest()
		t.Setenv("ROS_TAGS_ENABLED", "true")
		t.Setenv("ROS_TAGS_SOURCE", "api")
		t.Setenv("ROS_TAGS_DEV_TOKEN", "dev-token")

		_, err := pool.Exec(t.Context(), `
			INSERT INTO org_container_keys (
				org_id, cluster_uuid, namespace, workload, workload_type, container_name
			) VALUES ($1, $2, 'ns', 'wl', 'Deployment', 'ctr')
			ON CONFLICT DO NOTHING`,
			testutil.TestOrgID, testutil.TestClusterUUID,
		)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/internal/tags/sync", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set(echo.HeaderAuthorization, "Bearer dev-token")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp tags.SyncResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, 1, resp.Updated)
	})

	t.Run("malformed JSON returns 400", func(t *testing.T) {
		config.ResetTagsForTest()
		tags.ResetProviderForTest()
		t.Setenv("ROS_TAGS_ENABLED", "true")
		t.Setenv("ROS_TAGS_SOURCE", "api")
		t.Setenv("ROS_TAGS_DEV_TOKEN", "dev-token")

		req := httptest.NewRequest(http.MethodPost, "/internal/tags/sync", bytes.NewReader([]byte("{not json")))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set(echo.HeaderAuthorization, "Bearer dev-token")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("empty org_id returns 400", func(t *testing.T) {
		config.ResetTagsForTest()
		tags.ResetProviderForTest()
		t.Setenv("ROS_TAGS_ENABLED", "true")
		t.Setenv("ROS_TAGS_SOURCE", "api")
		t.Setenv("ROS_TAGS_DEV_TOKEN", "dev-token")

		payload, err := json.Marshal(tags.SyncRequest{OrgID: "  ", SyncedAt: "2026-05-25T12:00:00Z"})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/internal/tags/sync", bytes.NewReader(payload))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set(echo.HeaderAuthorization, "Bearer dev-token")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("invalid synced_at returns 400", func(t *testing.T) {
		config.ResetTagsForTest()
		tags.ResetProviderForTest()
		t.Setenv("ROS_TAGS_ENABLED", "true")
		t.Setenv("ROS_TAGS_SOURCE", "api")
		t.Setenv("ROS_TAGS_DEV_TOKEN", "dev-token")

		payload, err := json.Marshal(tags.SyncRequest{
			OrgID:    testutil.TestOrgID,
			SyncedAt: "yesterday",
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/internal/tags/sync", bytes.NewReader(payload))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set(echo.HeaderAuthorization, "Bearer dev-token")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("db source returns 404 for push", func(t *testing.T) {
		config.ResetTagsForTest()
		tags.ResetProviderForTest()
		t.Setenv("ROS_TAGS_ENABLED", "true")
		t.Setenv("ROS_TAGS_SOURCE", "db")
		t.Setenv("ROS_TAGS_DEV_TOKEN", "dev-token")

		req := httptest.NewRequest(http.MethodPost, "/internal/tags/sync", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set(echo.HeaderAuthorization, "Bearer dev-token")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

// postTagsSyncRawJSON posts a raw JSON body to the sync endpoint with the
// dev-token auth the handler requires. #814: the payload is deliberately a
// raw string, not a marshaled tags.SyncRequest, so the fixture pins the wire
// contract (what koku's builder sends) rather than this repo's Go types.
func postTagsSyncRawJSON(t *testing.T, e *echo.Echo, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/internal/tags/sync", bytes.NewReader([]byte(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set(echo.HeaderAuthorization, "Bearer dev-token")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func setupTagsSyncEcho(t *testing.T) *echo.Echo {
	t.Helper()
	config.ResetTagsForTest()
	tags.ResetProviderForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "api")
	t.Setenv("ROS_TAGS_DEV_TOKEN", "dev-token")
	e := echo.New()
	e.POST("/internal/tags/sync", api.PostTagsSync)
	return e
}

// TestPostTagsSync_MultiValuePayloadAccepted is #814 cycle 1's RED: a
// multi-value tag payload (koku's cycle-2 builder shape) must be accepted,
// not rejected by the unmarshal. Baseline: the whole request 400s because
// one array value fails the map[string]string unmarshal.
func TestPostTagsSync_MultiValuePayloadAccepted(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(context.Background(), `
		INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name)
		VALUES ($1, $2, $3, 'w1', 'Deployment', 'c1')
		ON CONFLICT DO NOTHING`,
		testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace)
	require.NoError(t, err)

	e := setupTagsSyncEcho(t)
	rec := postTagsSyncRawJSON(t, e, `{
		"org_id": "`+testutil.TestOrgID+`",
		"synced_at": "2026-11-18T12:00:00Z",
		"tag_keys": [{"key": "environment", "values": ["production", "staging"]}],
		"namespace_tags": [{
			"cluster_uuid": "`+testutil.TestClusterUUID+`",
			"namespace": "`+testutil.TestNamespace+`",
			"tags": {"environment": ["production", "staging"], "team": "platform"}
		}]
	}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp tags.SyncResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Updated)

	var stored string
	err = pool.QueryRow(context.Background(), `
		SELECT resolved_tags::text FROM org_container_keys
		WHERE org_id = $1 AND namespace = $2`,
		testutil.TestOrgID, testutil.TestNamespace).Scan(&stored)
	require.NoError(t, err)
	// The write contract: sorted-unique arrays, scalar payloads normalized too.
	assert.JSONEq(t, `{"environment":["production","staging"],"team":["platform"]}`, stored)
}

// TestPostTagsSync_NormalizesSortedUniqueArrays pins the D1/D3 write
// contract: whatever shape arrives — unsorted, duplicated, empty-only
// values, scalars — storage is always sorted-unique arrays with empty
// values dropped.
func TestPostTagsSync_NormalizesSortedUniqueArrays(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(context.Background(), `
		INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name)
		VALUES ($1, $2, $3, 'w1', 'Deployment', 'c1')
		ON CONFLICT DO NOTHING`,
		testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace)
	require.NoError(t, err)

	e := setupTagsSyncEcho(t)
	rec := postTagsSyncRawJSON(t, e, `{
		"org_id": "`+testutil.TestOrgID+`",
		"synced_at": "2026-11-18T12:00:00Z",
		"tag_keys": [
			{"key": "environment", "values": ["staging", "production", "production", " staging "]},
			{"key": "empty", "values": ["", "   "]},
			{"key": "  ", "values": ["x"]}
		],
		"namespace_tags": [{
			"cluster_uuid": "`+testutil.TestClusterUUID+`",
			"namespace": "`+testutil.TestNamespace+`",
			"tags": {
				"environment": ["staging", "production", "production", " staging "],
				"team": "platform",
				"empty": ["", "   "]
			}
		}]
	}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var stored string
	err = pool.QueryRow(context.Background(), `
		SELECT resolved_tags::text FROM org_container_keys
		WHERE org_id = $1 AND namespace = $2`,
		testutil.TestOrgID, testutil.TestNamespace).Scan(&stored)
	require.NoError(t, err)
	assert.JSONEq(t, `{"environment":["production","staging"],"team":["platform"]}`, stored,
		"unsorted/duplicated/whitespace values must collapse to sorted-unique; empty-only keys dropped")

	// The catalog half gets the same D3 defense: sorted-unique values,
	// empty values and blank keys dropped.
	var catalog string
	err = pool.QueryRow(context.Background(), `
		SELECT tag_keys::text FROM org_tag_sync_metadata WHERE org_id = $1`,
		testutil.TestOrgID).Scan(&catalog)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"key":"environment","values":["production","staging"]},{"key":"empty","values":[]}]`, catalog,
		"the catalog values must be sorted-unique with empty values dropped and blank keys dropped")
}
