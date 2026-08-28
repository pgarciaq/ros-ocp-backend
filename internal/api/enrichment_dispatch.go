package api

import (
	"context"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
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
	ctx = WithEnrichmentCache(ctx, in.OrgID)
	for _, e := range plugin.ByTrait[plugin.APIEnricher]() {
		if err := e.EnrichResponse(ctx, in); err != nil {
			log.Warnf("API enricher %s: %v", e.Name(), err)
		}
	}
	enrichBusinessHoursContainers(ctx, in.OrgID, in.Results)
	enrichContainerCurrency(ctx, in.OrgID, in.Results)
	enrichContainerTags(ctx, in.OrgID, in.Results)
}

func enrichBusinessHoursContainers(ctx context.Context, orgID string, results []model.NativeContainerResult) {
	pool := db.GetPool()
	if pool == nil {
		return
	}
	if err := engine.EnrichNativeContainerResultsWithBusinessHours(ctx, pool, orgID, results); err != nil {
		log.Warnf("business hours container enrichment: %v", err)
	}
}

// EnrichNativeNamespaceResults attaches currency, tags, and business-hours
// recommendations. Use this for detail. List handlers must call
// EnrichNativeNamespaceListResults so nested business_hours stays detail-only (#497).
func EnrichNativeNamespaceResults(ctx context.Context, orgID string, results []model.NativeNamespaceResult) {
	enrichNativeNamespaceResults(ctx, orgID, results, true)
}

// EnrichNativeNamespaceListResults attaches currency and tags only. Nested
// business_hours is detail-only; unfiltered list still uses NamespaceDetailResponse.
func EnrichNativeNamespaceListResults(ctx context.Context, orgID string, results []model.NativeNamespaceResult) {
	enrichNativeNamespaceResults(ctx, orgID, results, false)
}

func enrichNativeNamespaceResults(ctx context.Context, orgID string, results []model.NativeNamespaceResult, includeBusinessHours bool) {
	if len(results) == 0 {
		return
	}
	pool := db.GetPool()
	if pool == nil {
		return
	}
	if includeBusinessHours {
		if err := engine.EnrichNativeNamespaceResultsWithBusinessHours(ctx, pool, orgID, results); err != nil {
			log.Warnf("business hours namespace enrichment: %v", err)
		}
	}
	enrichNamespaceCurrency(ctx, orgID, results)
	enrichNamespaceTags(ctx, orgID, results)
}
