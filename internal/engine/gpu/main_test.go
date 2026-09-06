package gpu

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	if ResolveGPUThresholdSettings == nil {
		ResolveGPUThresholdSettings = func(_ context.Context, _ *pgxpool.Pool, _ string) (GPUThresholdSettings, error) {
			return DefaultGPUThresholdSettings(), nil
		}
	}
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("github.com/jackc/pgx/v5/pgxpool.(*Pool).backgroundHealthCheck"),
		goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener"),
		goleak.IgnoreTopFunction("github.com/testcontainers/testcontainers-go.(*Reaper).connect.func1"),
		goleak.IgnoreTopFunction("github.com/hashicorp/golang-lru/v2/expirable.NewLRU[...].func1"),
	)
}
