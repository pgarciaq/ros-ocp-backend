# Contributing to ros-ocp-backend

## Development Setup

### Prerequisites

- Go 1.25+ (see `go.mod`)
- PostgreSQL 16 (for integration tests)
- Docker or Podman (for testcontainers-based tests)

### Running Tests

```bash
# Unit tests (fast, no external dependencies)
go test -short ./...

# Unit tests with race detector
go test -short -race ./...

# Full tests (requires PostgreSQL on localhost:5432)
go test ./...

# Specific package
go test ./internal/engine/ -run TestClassify

# Fuzz tests (run until interrupted or failure)
go test ./internal/ingestion/ -fuzz=FuzzParseCSVRows -fuzztime=30s
```

### Using Podman for Integration Tests

testcontainers-go works with Podman via rootless socket:

```bash
export DOCKER_HOST=unix:///run/user/$(id -u)/podman/podman.sock
export TESTCONTAINERS_RYUK_DISABLED=true
go test ./internal/engine/ -run Integration
```

Ryuk (the cleanup sidecar) is incompatible with rootless Podman. Disabling it
is safe because test containers are cleaned up via `t.Cleanup()`.

---

## Testing Conventions

### Parallel Tests

All tests that are **pure computation** (no shared mutable state, no database,
no filesystem) MUST use `t.Parallel()`:

```go
func TestSomething(t *testing.T) {
    t.Parallel()
    // ...
}
```

### Configurable Thresholds (GPUThresholds Pattern)

The GPU classification engine uses a `GPUThresholds` struct instead of
package-level global variables. This enables safe parallel testing:

```go
// Production code calls ClassifyGPUWorkload() which uses defaultThresholds.
// Tests that need custom thresholds create their own instance:
func TestCustomThreshold(t *testing.T) {
    t.Parallel()  // safe — no global mutation
    th := engine.GPUThresholds{
        IdleThreshold:       0.05,
        UnderutilizedSM:     0.25,
        UnderutilizedTensor: 0.15,
        MemBoundDRAM:        0.60,
        MemBoundTensor:      0.15,
        FBHeadroomFactor:    1.20,
    }
    cls, _ := th.Classify(digests)
    assert.Equal(t, engine.GPUClassIdle, cls)
}
```

**Never** mutate package-level globals in tests. If production code uses
globals (e.g. `InitGPUEngine`), tests that verify that function should NOT
be marked `t.Parallel()` and should only verify that `InitGPUEngine` updates
`defaultThresholds` correctly.

### Environment Variables in Tests

Use `t.Setenv()` instead of `os.Setenv()`:

```go
// Good — automatic cleanup, test isolation
t.Setenv("ROS_GPU_IDLE_THRESHOLD", "0.05")

// Bad — leaks to other tests, requires manual cleanup
os.Setenv("ROS_GPU_IDLE_THRESHOLD", "0.05")
defer os.Unsetenv("ROS_GPU_IDLE_THRESHOLD")
```

### Assertions

Use `require` for preconditions that would cause nil-pointer panics if failed,
`assert` for everything else:

```go
rec := RecommendGPU(digests)
require.NotNil(t, rec, "expected recommendation for idle workload")
assert.Equal(t, GPUClassIdle, rec.Classification)
assert.Greater(t, rec.Confidence, float32(0))
```

### Error Path Testing

All external service integrations must have error-path tests covering:
- Timeouts (server takes longer than client timeout)
- Server errors (5xx)
- Authentication failures (401/403)
- Malformed responses (invalid JSON, unexpected schema)
- Context cancellation

See `internal/costdata/provider_contract_test.go` for the reference pattern.

### Fuzz Tests

Add fuzz tests for any function that parses external input (CSV, JSON, query
parameters). Fuzz targets must not panic on arbitrary input:

```go
func FuzzParseCSVRows(f *testing.F) {
    f.Add("valid,csv,header\nrow,data,here\n")
    f.Add("")
    f.Fuzz(func(t *testing.T, data string) {
        ParseCSVRows(strings.NewReader(data)) //nolint:errcheck
    })
}
```

Fuzz tests run in CI only when explicitly triggered (`go test -fuzz=...`).
They are NOT included in standard `go test ./...` runs.

### Integration Tests

Tests requiring PostgreSQL or external containers:
- Use build tag `//go:build integration` or check `testing.Short()`
- Use `testutil.SetupTestDB(t)` for database access
- Containers are managed via testcontainers-go (Docker or Podman)

```go
func TestPersistRecommendations(t *testing.T) {
    if testing.Short() {
        t.Skip("requires PostgreSQL")
    }
    pool := testutil.SetupTestDB(t)
    // ...
}
```

---

## Code Style

### Imports

Standard library, then external packages, then internal packages (separated
by blank lines):

```go
import (
    "context"
    "fmt"

    "github.com/labstack/echo/v4"
    "github.com/sirupsen/logrus"

    "github.com/redhatinsights/ros-ocp-backend/internal/config"
    "github.com/redhatinsights/ros-ocp-backend/internal/logging"
)
```

### Logging

Use the `internal/logging` package for structured logs with context:

```go
logging.ForOrg(orgID).WithField("cluster", clusterUUID).Info("processing complete")
logging.ForRequest(c).Warn("invalid parameter")
```

### Error Handling

Wrap errors with context. Never swallow errors silently:

```go
if err := db.Query(ctx, sql); err != nil {
    return fmt.Errorf("querying node recommendations for org %s: %w", orgID, err)
}
```

---

## Commit Messages

Use imperative mood, reference issue numbers:

```
Fix GPU threshold race in parallel tests (#428)

Introduce GPUThresholds struct with Classify/SelectMIGProfile methods.
Tests create local instances instead of mutating package globals.
```

## Pull Requests

- One logical change per PR
- Include test coverage for new/modified code
- Run `go vet ./...` and `go test -race -short ./...` before pushing
