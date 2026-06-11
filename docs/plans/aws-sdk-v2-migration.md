# AWS SDK v1 → v2 Migration Plan

**Status:** Planned (not scheduled)  
**Related finding:** Adversarial review #60  
**Urgency:** Low — `govulncheck` CI now flags known CVEs in dependencies including `aws-sdk-go` v1

## Current State

`go.mod` declares a direct dependency on `github.com/aws/aws-sdk-go v1.55.8` (AWS SDK v1, maintenance mode).

### Direct imports in this repository

| Package | File | Functionality |
|---------|------|---------------|
| `github.com/aws/aws-sdk-go/aws` | `internal/health/readyz.go` | S3 client configuration (region, endpoint, path-style) |
| `github.com/aws/aws-sdk-go/aws/credentials` | `internal/health/readyz.go`, `internal/logging/logging.go` | Static credentials for S3 readiness and CloudWatch logging |
| `github.com/aws/aws-sdk-go/aws/session` | `internal/health/readyz.go` | AWS session for S3 client |
| `github.com/aws/aws-sdk-go/service/s3` | `internal/health/readyz.go` | `HeadBucketWithContext` for optional deep readiness (`ROS_READINESS_CHECK_S3`) |
| `github.com/aws/aws-sdk-go/aws` | `internal/logging/logging.go` | CloudWatch log hook configuration (`WithRegion`, `WithCredentials`) |

### Indirect (transitive) usage

| Dependency | Usage |
|------------|-------|
| `github.com/redhatinsights/platform-go-middlewares/logging/cloudwatch` | CloudWatch Logs batching hook via AWS SDK v1 session/config |

No other first-party packages import AWS SDK v1. Ingestion and report processing do **not** use the AWS SDK directly — CSV fetch uses HTTP with SSRF controls.

## v2 Target Packages

| v1 import | v2 replacement |
|-----------|----------------|
| `aws`, `credentials`, `session` | `github.com/aws/aws-sdk-go-v2/aws`, `config`, `credentials` |
| `service/s3` | `github.com/aws/aws-sdk-go-v2/service/s3` |
| CloudWatch hook config | Evaluate `platform-go-middlewares` v2 support or replace hook with v2 CloudWatch Logs client |

## Migration Approach

1. **Add v2 modules** alongside v1 (`aws-sdk-go-v2`, `config`, `credentials`, `service/s3`).
2. **Migrate `internal/health/readyz.go`** — smallest, isolated surface:
   - Replace session + `HeadBucketWithContext` with v2 `s3.NewFromConfig` + `HeadBucket`.
   - Preserve path-style endpoint behavior for MinIO/NooBaa (`UsePathStyle`).
3. **Migrate `internal/logging/logging.go`** — depends on middleware compatibility:
   - Check whether `platform-go-middlewares` exposes a v2-compatible CloudWatch hook.
   - If not, either fork/wrap the hook with v2 CloudWatch Logs client or contribute upstream.
4. **Remove v1 from `go.mod`** once no direct or required transitive imports remain.
5. **Run `govulncheck ./...` and full test suite** including readiness integration tests (`internal/health/readyz_test.go` if present).

## Estimated Effort

| Phase | Scope | Effort |
|-------|-------|--------|
| Readiness S3 check | `readyz.go` only | S (1–2 hours) |
| CloudWatch logging | `logging.go` + middleware dependency | M (4–8 hours; may need upstream change) |
| Cleanup + verification | Remove v1, update docs, CI green | S (1 hour) |
| **Total** | | **M–L (1–2 days)** |

## Risk Assessment

- **Security:** Mitigated by new `govulncheck` CI workflow (PR + weekly). Migration is not urgent unless a CVE is reported.
- **Functional:** S3 readiness must continue to work with custom endpoints (on-prem object storage). v2 requires explicit path-style and custom endpoint configuration — well documented in v2.
- **Breaking:** No API contract changes; internal operational behavior only.

## Decision

Defer full migration until either:

1. `govulncheck` reports an actionable CVE in `aws-sdk-go` v1, or
2. `platform-go-middlewares` adds official v2 support, reducing CloudWatch migration cost.

Until then, monitor weekly `govulncheck` results and track this plan for the next maintenance window.
