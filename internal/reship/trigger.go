package reship

import (
	"context"

	"github.com/google/uuid"

	"github.com/redhatinsights/ros-ocp-backend/internal/asyncjobs"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

const defaultOrgMaxConcurrent = 2

// orgMaxConcurrent returns the masu reship fan-out cap for one org.
func orgMaxConcurrent() int {
	if cfg := config.GetConfig(); cfg != nil && cfg.ReshipConcurrency > 0 {
		return cfg.ReshipConcurrency
	}
	return defaultOrgMaxConcurrent
}

// Triggerer triggers historical ROS CSV re-shipment via Koku masu (Phase 7).
type Triggerer interface {
	TriggerReship(ctx context.Context, orgID string, clusterUUID uuid.UUID) error
}

// NoopTriggerer is a no-op stub when masu URL is unset.
type NoopTriggerer struct{}

func (n *NoopTriggerer) TriggerReship(context.Context, string, uuid.UUID) error {
	return nil
}

// DefaultTriggerer returns the reship Service when configured, otherwise noop.
func DefaultTriggerer() Triggerer {
	if svc := DefaultService(); svc != nil {
		return svc
	}
	return &NoopTriggerer{}
}

// TriggerAsync fires reship in the background for one or more clusters (org fan-out capped).
func TriggerAsync(trigger Triggerer, orgID string, clusterUUIDs []uuid.UUID) {
	if trigger == nil || len(clusterUUIDs) == 0 {
		return
	}
	asyncjobs.Go(func(ctx context.Context) {
		triggerReshipCoalesced(ctx, trigger, orgID, clusterUUIDs)
	})
}
