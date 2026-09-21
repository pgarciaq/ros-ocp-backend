package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// hostedMasterNarrativePhrases must never appear in node API output for
// hosted-topology clusters (#402 suppress lock). Each phrase names a
// narrative that is definitionally false where masters don't exist; the
// test pins their absence so future copy can't reintroduce them silently.
var hostedMasterNarrativePhrases = []string{
	"master node",
	"master nodes",
	"missing master",
	"control plane capacity",
	"control-plane capacity",
	"master capacity",
}

func TestNodeDetailHostedHasNoMasterNarratives(t *testing.T) {
	rec := model.NodeUtilizationRec{
		Node:           "worker-1",
		ClusterUUID:    "cluster-uuid",
		InstanceType:   "m5.xlarge",
		MachineSetName: "worker-us-east-1a",
		Classification: model.NodeUtilizationClassification{
			Category: "stranded_cpu",
		},
		Metrics: model.NodeUtilizationMetrics{CPUUtilP95: 0.2, MemUtilP95: 0.8},
		RecommendationTerms: map[string]model.NodeUtilizationTermRec{
			"medium_term": {
				RecommendationEngines: &model.NodeUtilizationEngines{
					Cost: &model.NodeUtilizationEngineRec{
						Notifications: map[string]notifications.NotificationEntry{
							"hosted_scope": {Code: 83},
						},
					},
				},
			},
		},
	}

	detail := nodeUtilizationDetailFromRec(rec)
	body, err := json.Marshal(detail)
	require.NoError(t, err)
	text := strings.ToLower(string(body))
	for _, phrase := range hostedMasterNarrativePhrases {
		assert.NotContains(t, text, phrase,
			"hosted node detail must not carry master/CP-capacity narratives")
	}
	// The hosted-scope annotation itself must survive: suppression removes
	// misleading copy, never the topology signal.
	assert.Contains(t, text, "83")
}
