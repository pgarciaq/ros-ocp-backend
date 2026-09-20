package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/types"
	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
)

// reportTopologyFacts extracts manifest topology facts from an enriched
// Kafka message (#580). Present = any field set: an all-empty topology
// object is meaningless, so absence and emptiness degrade identically.
func TestReportTopologyFacts(t *testing.T) {
	t.Run("absent facts degrade to zero", func(t *testing.T) {
		var msg types.KafkaMsg
		facts, present := reportTopologyFacts(msg)
		assert.False(t, present)
		assert.Equal(t, topology.TopologyFacts{}, facts)
	})

	t.Run("full facts preserved", func(t *testing.T) {
		var msg types.KafkaMsg
		msg.Metadata.Topology = topology.TopologyFacts{
			ControlPlaneTopology:         "HighlyAvailable",
			HostedClusterCount:           1,
			HostedControlPlaneNamespaces: []string{"hc01-infra-hc01"},
		}
		facts, present := reportTopologyFacts(msg)
		require.True(t, present)
		assert.Equal(t, "HighlyAvailable", facts.ControlPlaneTopology)
		assert.Equal(t, 1, facts.HostedClusterCount)
		assert.Equal(t, []string{"hc01-infra-hc01"}, facts.HostedControlPlaneNamespaces)
	})

	t.Run("partial facts preserved", func(t *testing.T) {
		// Namespaces without topology string still count: the HCP set is
		// independently useful for guardrail routing (#590 future).
		var msg types.KafkaMsg
		msg.Metadata.Topology = topology.TopologyFacts{
			HostedControlPlaneNamespaces: []string{"hc01-infra-hc01"},
		}
		facts, present := reportTopologyFacts(msg)
		require.True(t, present)
		assert.Equal(t, []string{"hc01-infra-hc01"}, facts.HostedControlPlaneNamespaces)
	})
}
