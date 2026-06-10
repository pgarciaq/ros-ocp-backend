package engine

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StubContainerHistoryHook returns a test hook that always returns err (nil uses default writer).
func StubContainerHistoryHook(err error) func(context.Context, *pgxpool.Pool, []ContainerRec, string) error {
	if err == nil {
		return WriteRecommendationHistory
	}
	return func(context.Context, *pgxpool.Pool, []ContainerRec, string) error {
		return err
	}
}

// StubContainerQualityHook returns a test hook that always returns err (nil uses default writer).
func StubContainerQualityHook(err error) func(
	context.Context, *pgxpool.Pool, []ContainerRec,
	map[string]map[containerKey]OldRecommendation, map[containerKey]int64,
) error {
	if err == nil {
		return WriteRecommendationQuality
	}
	return func(
		context.Context, *pgxpool.Pool, []ContainerRec,
		map[string]map[containerKey]OldRecommendation, map[containerKey]int64,
	) error {
		return err
	}
}

var errAnalyticsStub = errors.New("stub analytics failure")

// ErrAnalyticsStub is the shared error returned by stub analytics hooks in tests.
func ErrAnalyticsStub() error { return errAnalyticsStub }
