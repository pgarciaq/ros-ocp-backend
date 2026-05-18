package api

import (
	"context"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

// NativeContainerEnrichmentInput is passed to APIEnricher implementations after native container queries.
type NativeContainerEnrichmentInput struct {
	OrgID   string
	Results []model.NativeContainerResult
}

// EnrichNativeContainerResults invokes enabled APIEnricher plugins for container list/detail payloads.
func EnrichNativeContainerResults(ctx context.Context, in *NativeContainerEnrichmentInput) {
	if in == nil || len(in.Results) == 0 {
		return
	}
	for _, e := range plugin.ByTrait[plugin.APIEnricher]() {
		if err := e.EnrichResponse(ctx, in); err != nil {
			log.Warnf("API enricher %s: %v", e.Name(), err)
		}
	}
}
