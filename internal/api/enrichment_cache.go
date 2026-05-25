package api

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

type enrichmentCacheKey struct{}

// EnrichmentCache holds request-scoped enrichment data (cost rates, currency, GPU thresholds).
// Lives only for the duration of one HTTP request.
type EnrichmentCache struct {
	orgID string

	mu            sync.Mutex
	costRates     map[string]*costdata.ClusterCostData
	currency      string
	currencySet   bool
	gpuThresholds *engine.GPUThresholdSettings
	gpuThreshSet  bool
}

// WithEnrichmentCache attaches a request-scoped enrichment cache to ctx.
func WithEnrichmentCache(ctx context.Context, orgID string) context.Context {
	if ctx.Value(enrichmentCacheKey{}) != nil {
		return ctx
	}
	return context.WithValue(ctx, enrichmentCacheKey{}, &EnrichmentCache{
		orgID:     orgID,
		costRates: make(map[string]*costdata.ClusterCostData),
	})
}

func enrichmentCacheFromContext(ctx context.Context) *EnrichmentCache {
	if c, ok := ctx.Value(enrichmentCacheKey{}).(*EnrichmentCache); ok {
		return c
	}
	return nil
}

// GetCachedCostRates returns effective rates for clusterUUID, fetching from Koku on miss.
func GetCachedCostRates(ctx context.Context, orgID, clusterUUID string, start, end time.Time) *costdata.ClusterCostData {
	provider := getGPUCostProvider()
	if provider == nil || clusterUUID == "" {
		return nil
	}

	cache := enrichmentCacheFromContext(ctx)
	if cache == nil {
		kokuOrgID := strings.TrimPrefix(orgID, "org")
		cd, err := provider.GetEffectiveRates(ctx, kokuOrgID, clusterUUID, start, end)
		if err != nil {
			log.Warnf("GetCachedCostRates: fetch failed for cluster %s: %v", clusterUUID, err)
			return nil
		}
		return cd
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cd, ok := cache.costRates[clusterUUID]; ok {
		return cd
	}
	kokuOrgID := strings.TrimPrefix(orgID, "org")
	cd, err := provider.GetEffectiveRates(ctx, kokuOrgID, clusterUUID, start, end)
	if err != nil {
		log.Warnf("GetCachedCostRates: fetch failed for cluster %s: %v", clusterUUID, err)
		cache.costRates[clusterUUID] = nil
		return nil
	}
	cache.costRates[clusterUUID] = cd
	return cd
}

// GetCachedCurrency resolves org-level currency once per request.
func GetCachedCurrency(ctx context.Context, orgID, sampleClusterUUID string) string {
	cache := enrichmentCacheFromContext(ctx)
	if cache != nil {
		cache.mu.Lock()
		if cache.currencySet {
			cur := cache.currency
			cache.mu.Unlock()
			return cur
		}
		cache.mu.Unlock()
	}

	currency := costdata.DefaultCurrency
	if sampleClusterUUID != "" {
		now := time.Now().UTC()
		start := now.AddDate(0, 0, -30)
		if cd := GetCachedCostRates(ctx, orgID, sampleClusterUUID, start, now); cd != nil {
			currency = costdata.ResolveCurrency(cd)
		}
	}

	if cache != nil {
		cache.mu.Lock()
		cache.currency = currency
		cache.currencySet = true
		cache.mu.Unlock()
	}
	return currency
}

// GetCachedGPUThresholds resolves GPU threshold settings once per request.
func GetCachedGPUThresholds(ctx context.Context, pool *pgxpool.Pool, orgID string) (engine.GPUThresholdSettings, error) {
	cache := enrichmentCacheFromContext(ctx)
	if cache != nil {
		cache.mu.Lock()
		if cache.gpuThreshSet && cache.gpuThresholds != nil {
			s := *cache.gpuThresholds
			cache.mu.Unlock()
			return s, nil
		}
		cache.mu.Unlock()
	}

	settings, err := engine.ResolveGPUThresholdSettings(ctx, pool, orgID)
	if err != nil {
		return settings, err
	}

	if cache != nil {
		cache.mu.Lock()
		s := settings
		cache.gpuThresholds = &s
		cache.gpuThreshSet = true
		cache.mu.Unlock()
	}
	return settings, nil
}
