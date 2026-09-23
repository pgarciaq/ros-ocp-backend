package kruize

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

func notificationDefinitionsForTest() map[string]struct{} {
	out := make(map[string]struct{}, len(notifications.Definitions))
	for code := range notifications.Definitions {
		out[fmt.Sprintf("%d", code)] = struct{}{}
	}
	return out
}

// TestFilterNotifications_NativeCodesSurvive pins Phase 2(a): native codes
// (from MapToKruizeFormat-synthesized engine blocks) must pass the
// allowlist alongside legacy Kruize codes. Unknown codes still drop.
// Pre-growth, "1"/"77" are dropped — this test names that.
func TestFilterNotifications_NativeCodesSurvive(t *testing.T) {
	// Legacy semantic (pinned, unchanged): ONE unknown key wipes its whole
	// section — hence two engine blocks below. Native keys must survive
	// alongside legacy allowlisted ones.
	blob := map[string]interface{}{
		"recommendation_terms": map[string]interface{}{
			"short_term": map[string]interface{}{
				"recommendation_engines": map[string]interface{}{
					"cost": map[string]interface{}{
						"notifications": map[string]interface{}{
							"323004": map[string]interface{}{"type": "NOTICE"},
							"1":      map[string]interface{}{"type": "WARNING"},
							"77":     map[string]interface{}{"type": "INFO"},
						},
					},
					"performance": map[string]interface{}{
						"notifications": map[string]interface{}{
							"999999": map[string]interface{}{"type": "BOGUS"},
						},
					},
				},
			},
		},
	}

	out := filterNotifications("test-id", "test-cluster", blob)
	engines := out["recommendation_terms"].(map[string]interface{})["short_term"].(map[string]interface{})["recommendation_engines"].(map[string]interface{})
	got := engines["cost"].(map[string]interface{})["notifications"]
	require.NotNil(t, got, "notifications section must survive filtering")
	n := got.(map[string]interface{})
	assert.Contains(t, n, "323004", "legacy allowlisted code survives")
	assert.Contains(t, n, "1", "native code survives")
	assert.Contains(t, n, "77", "native code survives")
	_, wiped := engines["performance"].(map[string]interface{})["notifications"]
	assert.False(t, wiped, "unknown key still wipes its section (legacy semantic)")
}

// TestNotificationsToShowCoversDefinitions is the staleness guard: every
// native code must stay allowlisted (mirrors
// TestNotificationDefinitionsComplete). A newly added Definition without
// allowlist cover fails here, not in production filtering.
func TestNotificationsToShowCoversDefinitions(t *testing.T) {
	for code := range notificationDefinitionsForTest() {
		assert.Contains(t, NotificationsToShow, code, "native code %s must stay allowlisted", code)
	}
}
