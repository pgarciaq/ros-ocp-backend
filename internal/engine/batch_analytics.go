package engine

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// batchWriteHistory, batchWriteQuality, and namespaceWriteHistory are swappable in tests
// to simulate analytics failures.
var (
	batchWriteHistory     = WriteRecommendationHistory
	batchWriteQuality     = WriteRecommendationQuality
	namespaceWriteHistory = WriteNamespaceRecommendationHistory
)

// BatchAnalyticsResult holds non-fatal history/quality write outcomes for one container batch.
type BatchAnalyticsResult struct {
	HistoryErr error
	QualityErr error
}

// Degraded reports whether either analytics write failed.
func (r BatchAnalyticsResult) Degraded() bool {
	return r.HistoryErr != nil || r.QualityErr != nil
}

// WriteContainerBatchAnalytics runs non-fatal history and quality writes for a batch whose
// recommendations are already persisted.
func WriteContainerBatchAnalytics(
	ctx context.Context,
	pool *pgxpool.Pool,
	batch []ContainerRec,
	oldRecsByEngine map[string]map[containerKey]OldRecommendation,
	sourceBinary string,
) BatchAnalyticsResult {
	var result BatchAnalyticsResult
	result.HistoryErr = batchWriteHistory(ctx, pool, batch, sourceBinary)
	if oldRecsByEngine != nil {
		oomCounts := OOMCountsByContainer(batch)
		result.QualityErr = batchWriteQuality(ctx, pool, batch, oldRecsByEngine, oomCounts)
	}
	return result
}

// ContainerBatchAnalyticsDegraded is a convenience wrapper around WriteContainerBatchAnalytics.
func ContainerBatchAnalyticsDegraded(
	ctx context.Context,
	pool *pgxpool.Pool,
	batch []ContainerRec,
	oldRecsByEngine map[string]map[containerKey]OldRecommendation,
	sourceBinary string,
) bool {
	return WriteContainerBatchAnalytics(ctx, pool, batch, oldRecsByEngine, sourceBinary).Degraded()
}

// SetBatchWriteHistoryForTest swaps the history writer used by ContainerBatchAnalyticsDegraded.
// Pass nil to restore the default WriteRecommendationHistory.
func SetBatchWriteHistoryForTest(fn func(context.Context, *pgxpool.Pool, []ContainerRec, string) error) {
	if fn == nil {
		batchWriteHistory = WriteRecommendationHistory
		return
	}
	batchWriteHistory = fn
}

// SetBatchWriteQualityForTest swaps the quality writer used by ContainerBatchAnalyticsDegraded.
// Pass nil to restore the default WriteRecommendationQuality.
func SetBatchWriteQualityForTest(fn func(
	context.Context, *pgxpool.Pool, []ContainerRec,
	map[string]map[containerKey]OldRecommendation, map[containerKey]int64,
) error) {
	if fn == nil {
		batchWriteQuality = WriteRecommendationQuality
		return
	}
	batchWriteQuality = fn
}

// WriteNamespaceBatchAnalytics runs non-fatal namespace history writes after recommendations
// are already persisted. Returns true when any history write failed (analytics degraded).
func WriteNamespaceBatchAnalytics(
	ctx context.Context,
	pool *pgxpool.Pool,
	allHours []NamespaceRec,
	bhHours []NamespaceRec,
) bool {
	degraded := false
	if len(bhHours) > 0 {
		if err := namespaceWriteHistory(ctx, pool, bhHours); err != nil {
			degraded = true
		}
	}
	if len(allHours) > 0 {
		if err := namespaceWriteHistory(ctx, pool, allHours); err != nil {
			degraded = true
		}
	}
	return degraded
}

// SetNamespaceWriteHistoryForTest swaps the history writer used by WriteNamespaceBatchAnalytics.
// Pass nil to restore the default WriteNamespaceRecommendationHistory.
func SetNamespaceWriteHistoryForTest(fn func(context.Context, *pgxpool.Pool, []NamespaceRec) error) {
	if fn == nil {
		namespaceWriteHistory = WriteNamespaceRecommendationHistory
		return
	}
	namespaceWriteHistory = fn
}
