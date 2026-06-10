package services

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

func testKafkaMsg() types.KafkaMsg {
	msg := types.KafkaMsg{
		Request_id:   "req-1",
		B64_identity: "dGVzdA==",
		Files:        []string{"https://example.com/ocp_ros_usage.csv"},
	}
	msg.Metadata.Org_id = "1234567"
	msg.Metadata.Source_id = "src-1"
	msg.Metadata.Cluster_uuid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	msg.Metadata.Cluster_alias = "test-cluster"
	return msg
}

func TestSynthesizeManifestID_DeterministicFromDateInObjectKey(t *testing.T) {
	msg := testKafkaMsg()
	msg.Object_keys = []string{"org1234567/source=abc/date=2026-06-11/ocp_ros_usage.csv"}

	first := synthesizeManifestID(msg)
	second := synthesizeManifestID(msg)

	assert.True(t, strings.HasPrefix(first, synthesizedManifestPrefix))
	assert.Equal(t, first, second)
}

func TestSynthesizeManifestID_SameDaySameClusterGroupsLegacyMessages(t *testing.T) {
	msgA := testKafkaMsg()
	msgA.Object_keys = []string{"org1234567/source=abc/date=2026-06-11/ocp_ros_usage.csv"}
	msgA.Files = []string{"https://example.com/a"}

	msgB := testKafkaMsg()
	msgB.Object_keys = []string{"org1234567/source=abc/date=2026-06-11/ocp_ros_namespace_usage.csv"}
	msgB.Files = []string{"https://example.com/b"}

	assert.Equal(t, synthesizeManifestID(msgA), synthesizeManifestID(msgB))
}

func TestSynthesizeManifestID_DifferentDatesProduceDifferentIDs(t *testing.T) {
	msgA := testKafkaMsg()
	msgA.Object_keys = []string{"org1234567/source=abc/date=2026-06-10/ocp_ros_usage.csv"}

	msgB := testKafkaMsg()
	msgB.Object_keys = []string{"org1234567/source=abc/date=2026-06-11/ocp_ros_usage.csv"}

	assert.NotEqual(t, synthesizeManifestID(msgA), synthesizeManifestID(msgB))
}

func TestSynthesizeManifestID_FallsBackToPayloadFingerprint(t *testing.T) {
	msgA := testKafkaMsg()
	msgA.Files = []string{"https://example.com/file-a.csv"}

	msgB := testKafkaMsg()
	msgB.Files = []string{"https://example.com/file-b.csv"}

	idA := synthesizeManifestID(msgA)
	idB := synthesizeManifestID(msgB)
	assert.NotEqual(t, idA, idB)

	msgA.Files = append([]string(nil), msgA.Files...)
	assert.Equal(t, idA, synthesizeManifestID(msgA))
}

func TestManifestIDFromMsg_PreservesPublisherProvidedID(t *testing.T) {
	msg := testKafkaMsg()
	msg.Metadata.Manifest_id = "  real-manifest-uuid  "

	assert.Equal(t, "real-manifest-uuid", manifestIDFromMsg(msg))
}

func TestResolveManifestID_SynthesizesAndIncrementsMetric(t *testing.T) {
	before := testutil.ToFloat64(IngestManifestIDSynthesized)

	msg := testKafkaMsg()
	msg.Object_keys = []string{"org1234567/source=abc/date=2026-06-11/ocp_ros_usage.csv"}

	log := logrus.NewEntry(logrus.New())
	log.Logger.SetOutput(io.Discard)

	id := resolveManifestID(&msg, log)
	require.True(t, strings.HasPrefix(id, synthesizedManifestPrefix))
	assert.Equal(t, id, msg.Metadata.Manifest_id)
	assert.Equal(t, id, manifestIDFromMsg(msg))
	assert.Equal(t, before+1, testutil.ToFloat64(IngestManifestIDSynthesized))
}

func TestResolveManifestID_DoesNotIncrementMetricWhenPresent(t *testing.T) {
	before := testutil.ToFloat64(IngestManifestIDSynthesized)

	msg := testKafkaMsg()
	msg.Metadata.Manifest_id = "publisher-manifest-id"

	log := logrus.NewEntry(logrus.New())
	log.Logger.SetOutput(io.Discard)

	id := resolveManifestID(&msg, log)
	assert.Equal(t, "publisher-manifest-id", id)
	assert.Equal(t, before, testutil.ToFloat64(IngestManifestIDSynthesized))
}

func TestExtractDateFromObjectKey(t *testing.T) {
	assert.Equal(t, "2026-06-11", extractDateFromObjectKey("org123/source=abc/date=2026-06-11/file.csv"))
	assert.Equal(t, "", extractDateFromObjectKey("org123/source=abc/file.csv"))
}

func TestResolveManifestID_LogsWarningOnSynthesis(t *testing.T) {
	msg := testKafkaMsg()
	msg.Object_keys = []string{"org1234567/source=abc/date=2026-06-11/ocp_ros_usage.csv"}

	buf := &bytes.Buffer{}
	logger := logrus.New()
	logger.SetOutput(buf)
	logger.SetLevel(logrus.WarnLevel)

	resolveManifestID(&msg, logrus.NewEntry(logger))
	output := buf.String()
	assert.Contains(t, output, "omitted metadata.manifest_id")
	assert.Contains(t, output, synthesizedManifestPrefix)
}
