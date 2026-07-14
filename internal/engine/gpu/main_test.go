package gpu

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMain(m *testing.M) {
	if ResolveGPUThresholdSettings == nil {
		ResolveGPUThresholdSettings = func(_ context.Context, _ *pgxpool.Pool, _ string) (GPUThresholdSettings, error) {
			return DefaultGPUThresholdSettings(), nil
		}
	}
	os.Exit(m.Run())
}
