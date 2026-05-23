package reship

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

// ProviderResolver maps an OpenShift cluster UUID to a Koku provider UUID.
type ProviderResolver interface {
	ResolveProviderUUID(ctx context.Context, orgID string, clusterUUID uuid.UUID) (uuid.UUID, error)
}

// HTTPEffectiveRatesResolver uses masu effective_rates to resolve cluster_id → provider_uuid.
type HTTPEffectiveRatesResolver struct {
	provider costdata.CostDataProvider
}

// NewHTTPEffectiveRatesResolver builds a resolver backed by the masu effective_rates endpoint.
func NewHTTPEffectiveRatesResolver(masuURL string) *HTTPEffectiveRatesResolver {
	return &HTTPEffectiveRatesResolver{
		provider: costdata.NewHTTPCostDataProvider(masuURL, 30*time.Second),
	}
}

// ResolveProviderUUID returns the Koku provider UUID for the given cluster.
func (r *HTTPEffectiveRatesResolver) ResolveProviderUUID(
	ctx context.Context,
	orgID string,
	clusterUUID uuid.UUID,
) (uuid.UUID, error) {
	now := time.Now().UTC()
	data, err := r.provider.GetEffectiveRates(ctx, orgID, clusterUUID.String(), now, now)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve provider_uuid for cluster %s: %w", clusterUUID, err)
	}
	if data.ProviderUUID == "" {
		return uuid.Nil, fmt.Errorf("empty provider_uuid for cluster %s", clusterUUID)
	}
	parsed, err := uuid.Parse(data.ProviderUUID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid provider_uuid %q: %w", data.ProviderUUID, err)
	}
	return parsed, nil
}
