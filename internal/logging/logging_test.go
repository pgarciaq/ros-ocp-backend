package logging

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

func TestGetLogger_NonNilAndSingleton(t *testing.T) {
	t.Parallel()

	l1 := GetLogger()
	require.NotNil(t, l1)

	l2 := GetLogger()
	require.NotNil(t, l2)
	assert.Same(t, l1, l2, "GetLogger should return the same *logrus.Entry after sync.Once")
}

func TestSet_request_details_FieldsAndDoesNotMutateGlobalLogger(t *testing.T) {
	t.Parallel()

	rootBeforeSvc := GetLogger().Data["service"]
	require.NotNil(t, rootBeforeSvc, "root logger should have service field from config")

	msg := types.KafkaMsg{
		Request_id: "req-123",
	}
	msg.Metadata.Account = "acct-1"
	msg.Metadata.Org_id = "1234567"
	msg.Metadata.Source_id = "42"
	msg.Metadata.Cluster_uuid = "550e8400-e29b-41d4-a716-446655440000"
	msg.Metadata.Cluster_alias = "my-cluster"

	entry := Set_request_details(msg)
	require.NotNil(t, entry)
	assert.Equal(t, "req-123", entry.Data["request_id"])
	assert.Equal(t, "acct-1", entry.Data["account"])
	assert.Equal(t, "1234567", entry.Data["org_id"])
	assert.Equal(t, "42", entry.Data["source_id"])
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", entry.Data["cluster_uuid"])
	assert.Equal(t, "my-cluster", entry.Data["cluster_alias"])

	root := GetLogger()
	assert.Equal(t, rootBeforeSvc, root.Data["service"])
	_, hasReq := root.Data["request_id"]
	assert.False(t, hasReq, "Set_request_details must not attach request fields to the global logger")
}

func TestSet_request_details_recommendations_Fields(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	msg := types.RecommendationKafkaMsg{
		Request_id: "poll-req-9",
		Metadata: types.RecommendationMetadata{
			Org_id:             "7654321",
			Workload_id:        99,
			Experiment_name:    "exp-a",
			Max_endtime_report: ts,
			ExperimentType:     types.PayloadTypeContainer,
		},
	}

	entry := Set_request_details_recommendations(msg)
	require.NotNil(t, entry)
	assert.Equal(t, "poll-req-9", entry.Data["request_id"])
	assert.Equal(t, "7654321", entry.Data["org_id"])
	assert.Equal(t, uint(99), entry.Data["workload_id"])
	assert.Equal(t, "exp-a", entry.Data["experiment_name"])
	assert.Equal(t, ts, entry.Data["max_endtime_report"])

	root := GetLogger()
	_, hasWorkload := root.Data["workload_id"]
	assert.False(t, hasWorkload, "recommendation fields must not leak onto global logger")
}
