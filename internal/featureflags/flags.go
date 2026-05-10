package featureflags

import (
	"sync"

	"github.com/Unleash/unleash-go-sdk/v5"
	"github.com/Unleash/unleash-go-sdk/v5/context"
)

var namespaceRecommendationDisabledOnce sync.Once

func IsNamespaceEnabled(org_id string) bool {
	if cfg.DisableNamespaceRecommendation {
		// warn once per pod lifecycle
		namespaceRecommendationDisabledOnce.Do(func() {
			log.Warn("namespace recommendation feature disabled application-wide")
		})
		return false
	}

	flag := "rosocp.namespace_enabled"
// IsNamespaceEnabled returns true unless the Unleash kill switch
// "rosocp.namespace_disabled" is active for the given org.
//
// Design intent: namespace recommendations are ON by default so that
// on-prem deployments (which lack Unleash) get them without extra
// configuration. Cloud deployments can disable per-org via Unleash.
func IsNamespaceEnabled(org_id string) bool {
	killSwitch := "rosocp.namespace_disabled"

	var disabled bool
	if org_id == "" {
		disabled = unleash.IsEnabled(killSwitch)
	} else {
		ctx := context.Context{
			Properties: map[string]string{
				"orgId": org_id,
			},
		}
		disabled = unleash.IsEnabled(killSwitch, unleash.WithContext(ctx))
	}

	if disabled {
		log.WithField("org_id", org_id).Debug("namespace recommendations disabled via Unleash kill switch")
	}
	return !disabled
}
