package engine

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	containerHistoryWrite = WriteRecommendationHistory
	containerQualityWrite   = WriteRecommendationQuality
	namespaceHistoryWrite   = WriteNamespaceRecommendationHistory
)

// AnalyticsWriteHooks overrides analytics writers used during ingestion. Nil fields keep defaults.
type AnalyticsWriteHooks struct {
	ContainerHistory func(context.Context, *pgxpool.Pool, []ContainerRec, string) error
	ContainerQuality func(
		context.Context, *pgxpool.Pool, []ContainerRec,
		map[string]map[containerKey]OldRecommendation, map[containerKey]int64,
	) error
	NamespaceHistory func(context.Context, *pgxpool.Pool, []NamespaceRec) error
}

// SetAnalyticsWriteHooksForTest swaps analytics writers. Pass nil to restore production defaults.
func SetAnalyticsWriteHooksForTest(hooks *AnalyticsWriteHooks) {
	containerHistoryWrite = WriteRecommendationHistory
	containerQualityWrite = WriteRecommendationQuality
	namespaceHistoryWrite = WriteNamespaceRecommendationHistory
	if hooks == nil {
		return
	}
	if hooks.ContainerHistory != nil {
		containerHistoryWrite = hooks.ContainerHistory
	}
	if hooks.ContainerQuality != nil {
		containerQualityWrite = hooks.ContainerQuality
	}
	if hooks.NamespaceHistory != nil {
		namespaceHistoryWrite = hooks.NamespaceHistory
	}
}

// WriteContainerHistory runs the swappable container history writer.
func WriteContainerHistory(ctx context.Context, pool *pgxpool.Pool, batch []ContainerRec, sourceBinary string) error {
	return containerHistoryWrite(ctx, pool, batch, sourceBinary)
}

// WriteContainerQuality runs the swappable container quality writer.
func WriteContainerQuality(
	ctx context.Context,
	pool *pgxpool.Pool,
	batch []ContainerRec,
	oldRecsByEngine map[string]map[containerKey]OldRecommendation,
	oomCounts map[containerKey]int64,
) error {
	return containerQualityWrite(ctx, pool, batch, oldRecsByEngine, oomCounts)
}

// WriteNamespaceHistory runs the swappable namespace history writer.
func WriteNamespaceHistory(ctx context.Context, pool *pgxpool.Pool, recs []NamespaceRec) error {
	return namespaceHistoryWrite(ctx, pool, recs)
}

// WriteNamespaceRecommendationHistories writes business-hours then all-hours namespace history.
// Transient failures return a retryable error; permanent failures set degraded and continue.
func WriteNamespaceRecommendationHistories(
	ctx context.Context,
	pool *pgxpool.Pool,
	allHours, bhHours []NamespaceRec,
	isTransient func(error) bool,
) (degraded bool, err error) {
	if len(bhHours) > 0 {
		if histErr := namespaceHistoryWrite(ctx, pool, bhHours); histErr != nil {
			if isTransient(histErr) {
				return false, histErr
			}
			degraded = true
		}
	}
	if len(allHours) > 0 {
		if histErr := namespaceHistoryWrite(ctx, pool, allHours); histErr != nil {
			if isTransient(histErr) {
				return degraded, histErr
			}
			degraded = true
		}
	}
	return degraded, nil
}
