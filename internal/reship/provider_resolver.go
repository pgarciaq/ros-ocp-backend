package reship

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

// Provider resolution failure reasons for metrics and logs.
const (
	ReasonNoCostModel     = "no_cost_model"
	ReasonMasuUnavailable = "masu_unavailable"
	ReasonNotFound        = "not_found"
	ReasonTimeout         = "timeout"
)

var effectiveRatesStatusRE = regexp.MustCompile(`effective-rates returned (\d+)`)

// ProviderResolver maps an OpenShift cluster UUID to a Koku provider UUID.
type ProviderResolver interface {
	ResolveProviderUUID(ctx context.Context, orgID string, clusterUUID uuid.UUID) (uuid.UUID, error)
}

// ProviderResolutionError indicates failure to resolve cluster_uuid → provider_uuid via masu.
type ProviderResolutionError struct {
	Reason      string
	StatusCode  int
	ClusterUUID uuid.UUID
	Cause       error
}

func (e *ProviderResolutionError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("resolve provider_uuid for cluster %s (%s): %v", e.ClusterUUID, e.Reason, e.Cause)
	}
	return fmt.Sprintf("resolve provider_uuid for cluster %s (%s)", e.ClusterUUID, e.Reason)
}

func (e *ProviderResolutionError) Unwrap() error {
	return e.Cause
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
		reason, statusCode := categorizeEffectiveRatesError(err)
		return uuid.Nil, &ProviderResolutionError{
			Reason:      reason,
			StatusCode:  statusCode,
			ClusterUUID: clusterUUID,
			Cause:       err,
		}
	}
	if data.ProviderUUID == "" {
		return uuid.Nil, &ProviderResolutionError{
			Reason:      ReasonNotFound,
			StatusCode:  200,
			ClusterUUID: clusterUUID,
		}
	}
	parsed, err := uuid.Parse(data.ProviderUUID)
	if err != nil {
		return uuid.Nil, &ProviderResolutionError{
			Reason:      ReasonNotFound,
			StatusCode:  200,
			ClusterUUID: clusterUUID,
			Cause:       fmt.Errorf("invalid provider_uuid %q: %w", data.ProviderUUID, err),
		}
	}
	return parsed, nil
}

func categorizeEffectiveRatesError(err error) (reason string, statusCode int) {
	if err == nil {
		return ReasonMasuUnavailable, 0
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ReasonTimeout, 0
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ReasonTimeout, 0
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return ReasonTimeout, 0
		}
		if urlErr.Err != nil {
			return ReasonMasuUnavailable, 0
		}
	}

	msg := err.Error()
	if matches := effectiveRatesStatusRE.FindStringSubmatch(msg); len(matches) == 2 {
		code, parseErr := strconv.Atoi(matches[1])
		if parseErr == nil {
			statusCode = code
			switch {
			case code == 404:
				return ReasonNoCostModel, statusCode
			case code >= 500:
				return ReasonMasuUnavailable, statusCode
			default:
				return ReasonMasuUnavailable, statusCode
			}
		}
	}
	if strings.Contains(msg, "decode response") {
		return ReasonNotFound, 200
	}
	if strings.Contains(msg, "HTTP request to Koku:") {
		return ReasonMasuUnavailable, 0
	}
	return ReasonMasuUnavailable, 0
}

func resolutionFailureDetails(err error) (reason string, statusCode int) {
	var resErr *ProviderResolutionError
	if errors.As(err, &resErr) {
		return resErr.Reason, resErr.StatusCode
	}
	return ReasonMasuUnavailable, 0
}
