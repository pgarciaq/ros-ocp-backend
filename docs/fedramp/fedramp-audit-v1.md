# FedRAMP Gap Assessment — ros-ocp-backend

**Version:** 1.0
**Date:** 2026-07-05
**Target Certification Class:** C (Moderate) — NIST SP 800-53 Rev 5 (323 controls)
**Service:** Resource Optimization Service (ROS) backend
**Repository:** `ros-ocp-backend`
**Assessor:** Internal (AI-assisted adversarial review + codebase inspection)
**Status:** Initial assessment — no ATO package submitted

---

## Executive Summary

This document assesses `ros-ocp-backend` against FedRAMP Moderate (Class C under
CR26) requirements. The service is a Go microservice deployed on OpenShift that
provides resource optimization recommendations for Red Hat Cost Management.

**Overall readiness: Partial — 4 of 8 assessed control families have notable gaps.**

The service has strong foundations in input validation (SI family), automated
vulnerability detection, and FIPS-compliant cryptography (via Red Hat's golang-fips
toolchain). Key gaps exist in production security enforcement for on-prem defaults,
audit log completeness, and the absence of OSCAL documentation.

### Key Statistics


| Metric                                    | Value                                                       |
| ----------------------------------------- | ----------------------------------------------------------- |
| Control families assessed                 | 8 of 20 (technical families relevant to microservice layer) |
| Findings — Critical                       | 0                                                           |
| Findings — High                           | 6                                                           |
| Findings — Medium                         | 12                                                          |
| Findings — Low                            | 4                                                           |
| Controls fully satisfied at service layer | ~50% of assessed scope                                      |
| Controls partially satisfied              | ~40%                                                        |
| Controls with significant gaps            | ~10%                                                        |


---

## Scope and Methodology

### In Scope

- `ros-ocp-backend` Go application code (`internal/`, `cmd/`)
- Dockerfile and build pipeline (`.github/workflows/`, `.tekton/`)
- Configuration management (`internal/config/`)
- API middleware (authentication, authorization, rate limiting)
- External service integrations (PostgreSQL, Kafka, S3, RBAC, Kruize)
- Operational documentation (`docs/operations/`, `docs/audit/`)

### Out of Scope (Platform-Level)

The following are managed at the OpenShift platform / Red Hat SRE layer and are
not assessed here:

- Physical and environmental protection (PE family)
- Personnel security (PS family)
- Media protection at datacenter level (MP family)
- Network infrastructure (routers, firewalls, load baloancers)
- TLS termination at ingress/route level
- Kubernetes node-level encryption at rest
- Identity provider (SSO/Keycloak) configuration
- Platform-level SIEM and alerting (Splunk, PagerDuty)

### Assessment Methodology

1. Direct code inspection of all `internal/` packages
2. Review of Dockerfile, CI/CD workflows, and Clowder deployment config
3. Cross-reference with 7 prior adversarial security reviews (`docs/audit/`)
4. Comparison against NIST SP 800-53 Rev 5 control requirements
5. Evaluation of both SaaS (console.redhat.com) and on-prem deployment modes

---

## Control Family Assessments

### AC — Access Control

**Rating: ⚠️ Partial**

#### Current Implementation


| Control                            | Status      | Evidence                                                                               |
| ---------------------------------- | ----------- | -------------------------------------------------------------------------------------- |
| AC-2 (Account Management)          | Partial     | Org-scoped identity via `X-Rh-Identity` header; no local account management            |
| AC-3 (Access Enforcement)          | Partial     | RBAC middleware calls external RBAC service per-request; org-scoped resource filtering |
| AC-4 (Information Flow)            | Implemented | Schema-per-tenant isolation; RBAC filters query results by permission scope            |
| AC-6 (Least Privilege)             | Partial     | Service accounts have scoped permissions; S2S auth via TokenReview API                 |
| AC-7 (Unsuccessful Logon)          | N/A         | Authentication handled by upstream gateway (3scale)                                    |
| AC-8 (System Use Notification)     | N/A         | Not applicable to API service                                                          |
| AC-10 (Concurrent Sessions)        | N/A         | Stateless API — no session concept                                                     |
| AC-14 (Permitted Actions w/o Auth) | Implemented | Only `/healthz`, `/readyz`, `/status` are unauthenticated                              |
| AC-17 (Remote Access)              | Partial     | Rate limiting per-org; no IP allowlisting capability                                   |


#### Implementation Details

- **Identity extraction** (`[internal/api/middleware/identity.go](../../internal/api/middleware/identity.go)`):
Decodes `X-Rh-Identity` base64 header, validates structure, extracts `org_id`
- **Entitlement check** (`[internal/api/middleware/entitlement.go](../../internal/api/middleware/entitlement.go)`):
Enforces `cost_management.is_entitled == true` on all `/v1` routes
- **RBAC** (`[internal/api/middleware/rbac.go](../../internal/api/middleware/rbac.go)`):
Per-request call to RBAC service; parses `openshift.cluster`, `openshift.node`,
`openshift.project`, and `settings.*` permissions
- **Service-to-service auth** (`[internal/tags/auth.go](../../internal/tags/auth.go)`):
Kubernetes TokenReview API with SA allowlist enforcement
- **Rate limiting** (`[internal/api/middleware/rate_limiter.go](../../internal/api/middleware/rate_limiter.go)`):
Per-org token bucket with configurable RPM, burst, and expiry

#### Findings


| ID    | Finding                                                                                    | Control  | Priority   | Remediation                                                                                                                                                                                                                                                               |
| ----- | ------------------------------------------------------------------------------------------ | -------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| AC-01 | RBAC defaults to disabled (`RBAC_ENABLE=false`) in non-Clowder (on-prem) mode              | AC-3     | **High**   | ~~Add startup validation~~ **Resolved:** Graduated security enforcement ([#168](https://github.com/pgarciaq/ros-ocp-backend/issues/168)); fatal in Clowder, warn on-prem. See `[security-enforcement.md](../operations/security-enforcement.md)`                          |
| AC-02 | MFA cannot be verified or required at the service level — depends on upstream gateway      | AC-2(11) | **High**   | ~~Document in SSP as inherited~~ **Resolved:** Documented as inherited control with trust model and deployment-specific MFA enforcement in [`system-boundary.md`](system-boundary.md#multi-factor-authentication-mfa)                                                      |
| AC-03 | `DEVELOPMENT=true` bypasses entitlement check — no enforcement preventing prod use         | AC-3     | **High**   | ~~Add runtime guard~~ **Resolved:** Graduated security enforcement ([#168](https://github.com/pgarciaq/ros-ocp-backend/issues/168)); fatal if `DEVELOPMENT=true` alongside `ACG_CONFIG` (Clowder). See `[security-enforcement.md](../operations/security-enforcement.md)` |
| AC-04 | RBAC cache (`ROS_RBAC_CACHE_TTL`, default 60s) can serve stale permissions post-revocation | AC-3     | **Medium** | Document as accepted risk; reduce default TTL or add cache invalidation hook                                                                                                                                                                                              |
| AC-05 | No IP-based access restriction capability for administrative endpoints                     | AC-17    | **Low**    | Consider NetworkPolicy or ingress-level IP allowlisting                                                                                                                                                                                                                   |


---

### AU — Audit & Accountability

**Rating: ⚠️ Partial**

#### Current Implementation


| Control                         | Status         | Evidence                                                                                  |
| ------------------------------- | -------------- | ----------------------------------------------------------------------------------------- |
| AU-2 (Event Logging)            | Partial        | All state-changing operations logged; read operations partially logged                    |
| AU-3 (Content of Audit Records) | Partial        | `org_id`, `request_id`, timestamp, method, URI — but user identity absent from access log |
| AU-6 (Audit Record Review)      | N/A at service | Delegated to CloudWatch/SIEM at platform level                                            |
| AU-8 (Time Stamps)              | Implemented    | UTC timestamps from OS; NTP managed by OpenShift node                                     |
| AU-9 (Protection of Audit Info) | Partial        | Logs shipped to CloudWatch; no local tamper protection                                    |
| AU-11 (Audit Record Retention)  | N/A at service | Retention managed by CloudWatch configuration                                             |
| AU-12 (Audit Record Generation) | Partial        | Missing user identity correlation in HTTP access logs                                     |


#### Implementation Details

- **Structured logging**: `logrus` with JSON formatter, `ReportCaller: true`
- **CloudWatch integration**: Batching hook via `platform-go-middlewares/logging/cloudwatch`
- **Request correlation**: `requestLogger()` attaches `org_id` and `request_id` to handler logs
- **Kafka audit trail**: `Set_request_details()` attaches full context to processing logs
- **Internal endpoint audit**: `auditInternalEndpoint()` logs caller SA, action, target org

#### Findings


| ID    | Finding                                                                                        | Control | Priority   | Remediation                                                                                       |
| ----- | ---------------------------------------------------------------------------------------------- | ------- | ---------- | ------------------------------------------------------------------------------------------------- |
| AU-01 | HTTP access log does not include `org_id` or user identity — only method, URI, status, latency | AU-3    | **High**   | Configure `middleware.RequestLoggerWithConfig` with custom `LogValuesFunc` that includes `org_id` |
| AU-02 | No log integrity/immutability controls at service layer                                        | AU-9    | **High**   | ~~Document as inherited~~ **Resolved:** Documented as inherited control from CloudWatch/Splunk in [`system-boundary.md`](system-boundary.md#inherited-controls) |
| AU-03 | Successful read operations (GET recommendations) not individually audit-logged                 | AU-12   | **Medium** | Add opt-in verbose access logging mode                                                            |
| AU-04 | Log entries contain `org_id` but not `user_id`/`username` from identity header                 | AU-3    | **Medium** | Extract and include username in structured log context                                            |
| AU-05 | No centralized SIEM integration documented                                                     | AU-6    | **Medium** | ~~Document platform-level SIEM~~ **Resolved:** Documented as inherited control in [`system-boundary.md`](system-boundary.md#inherited-controls)                |


---

### IA — Identification & Authentication

**Rating: ⚠️ Partial**

#### Current Implementation


| Control                           | Status    | Evidence                                                        |
| --------------------------------- | --------- | --------------------------------------------------------------- |
| IA-2 (User Identification & Auth) | Partial   | Identity from `X-Rh-Identity` header; no local auth             |
| IA-2(1) (MFA for Privileged)      | Inherited | MFA enforced by upstream IdP (Keycloak/SSO)                     |
| IA-2(2) (MFA for Non-Privileged)  | Inherited | Same as above                                                   |
| IA-3 (Device Identification)      | N/A       | API service — no device-level auth                              |
| IA-4 (Identifier Management)      | Inherited | `org_id` managed by platform identity service                   |
| IA-5 (Authenticator Management)   | Partial   | S2S uses Kubernetes SA tokens (short-lived, rotated by kubelet) |
| IA-8 (Non-Organizational Users)   | N/A       | Service not accessible to non-org users                         |


#### Implementation Details

- **Identity trust model**: The service operates within a zero-trust-adjacent
architecture where the API gateway (3scale/Akamai) validates and injects the
`X-Rh-Identity` header. The service trusts the header's presence as proof of
authentication — it does not perform cryptographic signature verification.
- **S2S authentication**: Internal endpoints use Kubernetes TokenReview API with
an explicit SA allowlist (`ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS`).

#### Findings


| ID    | Finding                                                                                  | Control | Priority   | Remediation                                                                                                                                                                                                                              |
| ----- | ---------------------------------------------------------------------------------------- | ------- | ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| IA-01 | No cryptographic signature verification on `X-Rh-Identity` — relies on network perimeter | IA-2    | **High**   | ~~Document trust boundary model~~ **Resolved:** Trust model documented with perimeter defense rationale and accepted risk in [`system-boundary.md`](system-boundary.md#identity-header-x-rh-identity)                                    |
| IA-02 | MFA enforcement entirely gateway-dependent; service cannot verify MFA claim              | IA-2(1) | **High**   | ~~Document as inherited~~ **Resolved:** Documented as inherited control with deployment-specific MFA providers in [`system-boundary.md`](system-boundary.md#multi-factor-authentication-mfa)                                             |
| IA-03 | `ROS_TAGS_DEV_TOKEN` static secret mechanism must be provably absent in production       | IA-5    | **Medium** | ~~Add startup fatal~~ **Resolved:** Graduated security enforcement ([#168](https://github.com/pgarciaq/ros-ocp-backend/issues/168)); `DEV_TOKEN_PRESENT` finding. See `[security-enforcement.md](../operations/security-enforcement.md)` |
| IA-04 | No mutual TLS between microservices (peer auth via service mesh / NetworkPolicy)         | IA-3    | **Medium** | ~~Document as inherited~~ **Resolved:** Documented as accepted risk with NetworkPolicy mitigation and mesh compatibility in [`system-boundary.md`](system-boundary.md#mutual-tls-between-microservices)                                  |


---

### SC — System & Communications Protection

**Rating: ⚠️ Partial**

#### Current Implementation


| Control                             | Status      | Evidence                                                                                                                |
| ----------------------------------- | ----------- | ----------------------------------------------------------------------------------------------------------------------- |
| SC-7 (Boundary Protection)          | Partial     | SSRF controls block private IPs; CSV host allowlisting                                                                  |
| SC-8 (Transmission Confidentiality) | Implemented | TLS termination at ingress (inherited); DB/Kafka TLS validated at startup via graduated security enforcement            |
| SC-12 (Crypto Key Management)       | Implemented | Keys from environment variables; Red Hat go-toolset uses FIPS-validated OpenSSL                                         |
| SC-13 (Cryptographic Protection)    | Implemented | Red Hat `go-toolset:1.25` (golang-fips fork) + `ubi9/ubi-minimal` with OpenSSL 3; FIPS active when OS FIPS mode enabled |
| SC-23 (Session Authenticity)        | Implemented | Stateless API with per-request identity verification                                                                    |
| SC-28 (Protection at Rest)          | Partial     | Delegated to PostgreSQL volume encryption; no application-layer encryption                                              |


#### Implementation Details

- **TLS termination**: The API server runs plain HTTP; OpenShift route terminates TLS.
This is standard for containerized services but requires SSP documentation.
- **SSRF protections**:
  - `[internal/csv/csv_security.go](../../internal/csv/csv_security.go)`: Host allowlist,
  DNS rebinding check, private IP blocking, redirect blocking
  - `[internal/health/readyz.go](../../internal/health/readyz.go)`: S3 endpoint validated
  for HTTPS-only in production; private/loopback/link-local IP blocking
- **DB TLS**: `DBssl` configurable via `sslmode`; Clowder mode reads from Clowder config;
non-Clowder defaults to `"disable"`
- **Kafka TLS**: SASL mechanism and security protocol validated at startup; `PLAINTEXT`/`SASL_PLAINTEXT` triggers warn/fatal depending on enforcement level

#### Findings


| ID    | Finding                                                                            | Control | Priority   | Remediation                                                                                                                                                                                                                                                   |
| ----- | ---------------------------------------------------------------------------------- | ------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| SC-02 | DB `sslmode` defaults to `"disable"` in non-Clowder (on-prem) mode                 | SC-8    | **High**   | ~~Add startup validation~~ **Resolved:** Graduated security enforcement ([#168](https://github.com/pgarciaq/ros-ocp-backend/issues/168)); fatal in Clowder/enforce mode, warn on-prem. See `[security-enforcement.md](../operations/security-enforcement.md)` |
| SC-03 | API server does not terminate TLS natively — TLS boundary at ingress               | SC-8    | **High**   | ~~Document in SSP~~ **Resolved:** TLS termination boundary, pod-to-pod encryption, and deployment-specific mechanisms documented in [`system-boundary.md`](system-boundary.md#tls-boundary)                                                                   |
| SC-04 | No validation that Kafka connection uses TLS in production                         | SC-8    | **Medium** | ~~Add startup check~~ **Resolved:** Graduated security enforcement ([#168](https://github.com/pgarciaq/ros-ocp-backend/issues/168)); warns/fatals on `PLAINTEXT`/`SASL_PLAINTEXT`. See `[security-enforcement.md](../operations/security-enforcement.md)`     |
| SC-05 | FIPS activation depends on deployment cluster having FIPS mode enabled at OS level | SC-13   | **Medium** | ~~Document operational requirement~~ **Resolved:** FIPS activation requirements, verification steps, and deployment responsibility documented in [`system-boundary.md`](system-boundary.md#fips-cryptographic-protection)                                      |
| SC-06 | Encryption at rest entirely delegated to platform — no application-layer controls  | SC-28   | **Medium** | ~~Document as inherited~~ **Resolved:** Encryption at rest documented as inherited control (LUKS, EBS, etcd) in [`system-boundary.md`](system-boundary.md#inherited-controls)                                                                                 |


---

### SI — System & Information Integrity

**Rating: ✅ Strong**

#### Current Implementation


| Control                              | Status      | Evidence                                                                |
| ------------------------------------ | ----------- | ----------------------------------------------------------------------- |
| SI-2 (Flaw Remediation)              | Implemented | govulncheck weekly, CodeQL, Snyk SAST, renovate dependency updates      |
| SI-3 (Malicious Code Protection)     | N/A         | API service — no file upload/execution; container image scanned by Quay |
| SI-4 (System Monitoring)             | Implemented | Prometheus metrics, CloudWatch logging, health endpoints                |
| SI-5 (Security Alerts)               | Partial     | GitHub Dependabot/renovate alerts; no runtime security alerting         |
| SI-7 (Software Integrity)            | Implemented | `go.sum` hash verification; vendor directory with pinned deps           |
| SI-10 (Information Input Validation) | Implemented | RFC 1123 allowlisting, parameterized SQL, SSRF prevention, size limits  |
| SI-11 (Error Handling)               | Implemented | Structured error responses; no stack traces leaked to clients           |


#### Implementation Details

- **Input validation** (`[internal/api/handlers_utils.go](../../internal/api/handlers_utils.go)`):
`sanitizeParamValue()` enforces RFC 1123 charset allowlisting; `escapeILIKE()` for GORM wildcards
- **SQL injection prevention**: All queries use GORM parameterized (`?`) or pgxpool (`$N`) placeholders
- **Size limits**: `MaxBytesReader` on CSV downloads; `BodyLimit` on internal routes;
Kafka message slice bounds checked
- **Vulnerability scanning pipeline**:
  - `govulncheck` — weekly + per-PR (`.github/workflows/govulncheck.yml`)
  - CodeQL (Go) — push/PR + weekly (`.github/workflows/codeql.yml`)
  - Snyk SAST — downstream Konflux pipeline (`.tekton/`)
  - `golangci-lint` — per-PR (`.github/workflows/build.yml`)
  - `renovate.json` — automated dependency update PRs

#### Findings


| ID    | Finding                                                                                                      | Control | Priority   | Remediation                                                                 |
| ----- | ------------------------------------------------------------------------------------------------------------ | ------- | ---------- | --------------------------------------------------------------------------- |
| SI-01 | CSV export does not sanitize against spreadsheet formula injection (values starting with `=`, `+`, `-`, `@`) | SI-10   | **Medium** | Prefix formula-triggering characters with `'` or tab in exported CSV values |
| SI-02 | No runtime security alerting (RASP) — depends on external monitoring                                         | SI-5    | **Low**    | Document as inherited from platform monitoring                              |
| SI-03 | No SBOM generated in upstream build (downstream Konflux may produce one)                                     | SI-7    | **Low**    | Add `syft` or `ko` SBOM generation to CI pipeline                           |


---

### CM — Configuration Management

**Rating: ⚠️ Partial**

#### Current Implementation


| Control                             | Status      | Evidence                                                                      |
| ----------------------------------- | ----------- | ----------------------------------------------------------------------------- |
| CM-2 (Baseline Configuration)       | Partial     | `clowdapp.yaml` defines deployment baseline; 302 ADRs document decisions      |
| CM-3 (Configuration Change Control) | Implemented | Git-based change control; PR reviews required; CI gates                       |
| CM-6 (Configuration Settings)       | Partial     | Startup validation enforces some security settings; others have weak defaults |
| CM-7 (Least Functionality)          | Implemented | Minimal container image (UBI-minimal); no unnecessary services                |
| CM-8 (System Component Inventory)   | Partial     | `go.mod`/`go.sum` provide dependency inventory; no full system inventory      |


#### Implementation Details

- **Startup validation** (`[internal/config/config.go](../../internal/config/config.go)`):
`ValidateSecurityConfig()` enforces CSV host allowlist and internal auth requirement
in non-development mode. `ValidateConfig()` warns on insecure configurations.
- **Secure defaults in Clowder mode**: RBAC enabled, SSL from Clowder, Kafka
credentials from Clowder secret injection.
- **Secrets management**: No hardcoded secrets. All credentials from environment
variables (Clowder-injected or operator-provided).
- **CORS** (`[internal/api/server.go](../../internal/api/server.go)`): Restricted in
production (deny-all if no origins configured); methods limited to GET, PUT, DELETE.

#### Findings


| ID    | Finding                                                                                                         | Control | Priority   | Remediation                                                                                                                                                                                                                                                     |
| ----- | --------------------------------------------------------------------------------------------------------------- | ------- | ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| CM-01 | On-prem non-Clowder defaults are insecure (`RBAC_ENABLE=false`, `DBssl=disable`) with no production enforcement | CM-6    | **High**   | ~~Extend `ValidateSecurityConfig()`~~ **Resolved:** Graduated security enforcement ([#168](https://github.com/pgarciaq/ros-ocp-backend/issues/168)); three-tier model (None/Warn/Fatal). See `[security-enforcement.md](../operations/security-enforcement.md)` |
| CM-02 | No secrets rotation mechanism documented or enforced                                                            | CM-6    | **Medium** | Document rotation procedures; add credential age monitoring                                                                                                                                                                                                     |
| CM-03 | No formal SCAP/STIG/CCE baseline mapping for the container image                                                | CM-2    | **Medium** | ~~Use UBI STIG-hardened base image~~ **Resolved:** `[stig-baseline-mapping.md](stig-baseline-mapping.md)`                                                                                                                                                       |
| CM-04 | `DEVELOPMENT=true` flag disables multiple security controls with no prod guard                                  | CM-6    | **Medium** | ~~Covered by AC-03~~ **Resolved:** Graduated security enforcement ([#168](https://github.com/pgarciaq/ros-ocp-backend/issues/168)); Clowder + DEVELOPMENT always fatals                                                                                         |
| CM-05 | No SBOM generated in upstream CI                                                                                | CM-8    | **Low**    | Covered by SI-03                                                                                                                                                                                                                                                |


---

### IR — Incident Response

**Rating: ⚠️ Partial**

#### Current Implementation


| Control                       | Status         | Evidence                                                          |
| ----------------------------- | -------------- | ----------------------------------------------------------------- |
| IR-1 (IR Policy & Procedures) | Gap            | No IR documentation in repository                                 |
| IR-4 (Incident Handling)      | Partial        | Prometheus metrics for anomaly detection; DLQ for failed messages |
| IR-5 (Incident Monitoring)    | Partial        | Health endpoints; Prometheus metrics; CloudWatch logs             |
| IR-6 (Incident Reporting)     | Gap            | No reporting mechanism documented                                 |
| IR-7 (IR Assistance)          | N/A at service | Platform SRE responsibility                                       |
| IR-8 (IR Plan)                | Gap            | No plan in repository                                             |


#### Implementation Details

- **Health endpoints**: `/healthz` (liveness), `/readyz` (DB + optional Kafka/S3 checks)
- **Prometheus metrics**: HTTP latency/counts, rate limiting counter, DB pool metrics,
internal endpoint anomaly counter, Kafka panic counter
- **Graceful shutdown**: `signal.NotifyContext` for SIGTERM/SIGINT
- **Dead letter queue**: Failed Kafka messages routed to DLQ topic for investigation

#### Findings


| ID    | Finding                                                                              | Control    | Priority   | Remediation                                                                                       |
| ----- | ------------------------------------------------------------------------------------ | ---------- | ---------- | ------------------------------------------------------------------------------------------------- |
| IR-01 | No incident response plan or runbook in repository                                   | IR-1, IR-8 | **High**   | Create `docs/operations/incident-response-runbook.md` with security event taxonomy and escalation |
| IR-02 | No AlertManager rules or PagerDuty integration in codebase                           | IR-5       | **High**   | Document as inherited from platform SRE; or create `deploy/alerts.yaml` with critical alert rules |
| IR-03 | No security event classification (which log patterns = security incident)            | IR-4       | **High**   | Define in IR runbook: auth failures > threshold, rate limit exhaustion, SSRF attempts             |
| IR-04 | In-memory rate limiter is per-replica — no cross-replica blocking for a specific org | IR-4       | **Medium** | Document as accepted risk with monitoring; recommend Redis-backed limiter for production          |


---

### RA — Risk Assessment

**Rating: ⚠️ Partial**

#### Current Implementation


| Control                         | Status      | Evidence                                                        |
| ------------------------------- | ----------- | --------------------------------------------------------------- |
| RA-2 (Security Categorization)  | Implemented | `[security-categorization.md](security-categorization.md)`      |
| RA-3 (Risk Assessment)          | Implemented | `[risk-assessment.md](risk-assessment.md)`                      |
| RA-5 (Vulnerability Monitoring) | Implemented | govulncheck, CodeQL, Snyk, golangci-lint — weekly + per-PR      |
| RA-7 (Risk Response)            | Partial     | Findings tracked in GitHub issues; ADRs document risk decisions |
| RA-9 (Criticality Analysis)     | Gap         | No formal business impact analysis                              |


#### Implementation Details

- **Adversarial reviews**: 7 rounds documented in `docs/audit/`, 162+ findings tracked
and resolved. Latest review (v7.0, 2026-07-05) achieved 100% resolution.
- **Automated scanning**: Multi-layer: govulncheck (Go vulns), CodeQL (SAST), Snyk
(supply chain), golangci-lint (code quality).
- **ADR governance**: 302 ADRs documenting all architectural decisions, including
security trade-offs with explicit threat models.
- **Dependency management**: Vendor directory with pinned versions; renovate for updates.

#### Findings


| ID    | Finding                                                                    | Control | Priority   | Remediation                                                                                         |
| ----- | -------------------------------------------------------------------------- | ------- | ---------- | --------------------------------------------------------------------------------------------------- |
| RA-01 | No formal NIST 800-30 risk assessment document                             | RA-3    | **High**   | ~~Produce a risk assessment~~ **Resolved:** `[risk-assessment.md](risk-assessment.md)`              |
| RA-02 | Adversarial reviews are AI-assisted but not third-party penetration tested | RA-5    | **High**   | Commission an independent 3PAO or pen test firm for annual assessment                               |
| RA-03 | No FIPS 199 security categorization document                               | RA-2    | **Medium** | ~~Produce categorization~~ **Resolved:** `[security-categorization.md](security-categorization.md)` |
| RA-04 | No formal annual review cadence (reviews driven by feature sprints)        | RA-3    | **Medium** | Establish calendar-based quarterly review + annual full assessment                                  |


---

## Cross-Cutting Gaps

### System Boundary Documentation

**Status: Not documented**

The service connects to the following external systems. A formal boundary diagram
is required for the SSP.


| External System               | Direction       | Protocol                         | Data Classification               |
| ----------------------------- | --------------- | -------------------------------- | --------------------------------- |
| PostgreSQL                    | Bidirectional   | TCP/5432 (TLS configurable)      | CUI — optimization data, org IDs  |
| Apache Kafka (AMQ Streams)    | Bidirectional   | TCP/9092 (SASL+TLS configurable) | CUI — usage metrics, cluster data |
| RBAC Service                  | Outbound        | HTTPS                            | Metadata — permission scopes      |
| Kruize/Autotune               | Outbound        | HTTP/HTTPS                       | CUI — workload recommendations    |
| Koku/Masu (Cost Mgmt backend) | Inbound (Kafka) | N/A (message bus)                | CUI — cost/usage data             |
| AWS S3                        | Outbound        | HTTPS                            | Readiness check only (no data)    |
| AWS CloudWatch                | Outbound        | HTTPS                            | Logs — may contain org IDs        |
| Kubernetes API Server         | Outbound        | HTTPS (mTLS via SA token)        | Metadata — TokenReview            |


### OSCAL Documentation

**Status: Absent**

No OSCAL artifacts exist. RFC-0024 mandates machine-readable OSCAL packages for
all new ATO submissions by September 30, 2026. Required artifacts:

- [ ] `oscal/component-definition.json` — control-to-implementation mapping
- [ ] `oscal/system-security-plan.json` — full SSP in OSCAL format
- [ ] `oscal/assessment-results.json` — scan results and findings
- [ ] `oscal/plan-of-action-and-milestones.json` — POA&M entries

### FIPS 140-2/3 Compliance

**Status: Compliant by design — activation depends on deployment environment**

The build and runtime images provide FIPS-validated cryptography through Red Hat's
standard approach:


| Component                         | FIPS Status           | Notes                                                                                                                                            |
| --------------------------------- | --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| Go crypto (TLS, hashing, etc.)    | ✅ FIPS-validated      | Red Hat `go-toolset:1.25` is the [golang-fips fork](https://github.com/golang-fips/go); routes crypto to FIPS-validated OpenSSL via CGO + dlopen |
| `confluent-kafka-go` (librdkafka) | ✅ Uses system OpenSSL | CGO dependency links against system libcrypto.so                                                                                                 |
| Container OS (UBI9-minimal)       | ✅ FIPS-capable        | Ships OpenSSL 3 (FIPS-validated); activated when node in FIPS mode                                                                               |
| PostgreSQL `lib/pq` driver        | ✅ Uses Go crypto      | Go TLS (routed to OpenSSL by golang-fips when FIPS active)                                                                                       |
| AWS SDK for Go                    | ✅ Uses Go crypto      | Same routing to OpenSSL; optionally enable `UseFIPSEndpoint` for AWS FIPS endpoints                                                              |


**How it works:**

1. Red Hat's `ubi10/go-toolset:1.25` compiles Go with the golang-fips patches
2. `CGO_ENABLED=1` (implicit, required by confluent-kafka-go) enables dynamic linking
3. `-ldflags="-s -w"` strips debug symbols but does NOT affect dynamic linking
4. At runtime, the binary detects `/proc/sys/crypto/fips_enabled` on the host
5. When FIPS mode is active, all Go crypto operations route to the system's
  FIPS-validated OpenSSL 3 via dlopen

**Operational requirement:** The target OpenShift cluster nodes must have FIPS mode
enabled (`fips=1` kernel parameter on RHCOS). This is a standard platform configuration
for FedRAMP deployments and should be documented in the SSP as a deployment prerequisite.

**Additionally available (Go 1.25):** The native `GOFIPS140` module (upstream Go's
own FIPS 140-3 certified crypto, no OpenSSL dependency). This can be used as an
alternative path via `GOFIPS140=latest` at build time, though Red Hat's go-toolset
approach is already sufficient.

---

## Consolidated Findings Table


| ID    | Finding                                                        | Family | Priority   | Status                                    |
| ----- | -------------------------------------------------------------- | ------ | ---------- | ----------------------------------------- |
| AC-01 | RBAC defaults to disabled in on-prem mode                      | AC     | **High**   | **Resolved**                              |
| AC-02 | MFA not verifiable at service level                            | AC     | **High**   | **Resolved** — inherited control          |
| AC-03 | `DEVELOPMENT=true` bypasses security with no prod guard        | AC     | **High**   | **Resolved**                              |
| AU-01 | User identity absent from HTTP access logs                     | AU     | **High**   | Open                                      |
| AU-02 | No log integrity controls at service layer                     | AU     | **High**   | **Resolved** — inherited control          |
| IA-01 | No cryptographic signature verification on identity header     | IA     | **High**   | **Resolved** — trust model documented     |
| SC-02 | DB TLS defaults to disabled in on-prem mode                    | SC     | **High**   | **Resolved**                              |
| SC-03 | Service does not terminate TLS natively                        | SC     | **High**   | **Resolved** — boundary documented        |
| IR-01 | No IR plan/runbook in repository                               | IR     | **High**   | Open                                      |
| IR-02 | No alerting rules in codebase                                  | IR     | **High**   | Open                                      |
| IR-03 | No security event classification                               | IR     | **High**   | Open                                      |
| RA-01 | No formal NIST 800-30 risk assessment                          | RA     | **High**   | **Resolved**                              |
| RA-02 | No third-party penetration test                                | RA     | **High**   | Open                                      |
| CM-01 | Insecure on-prem defaults with no enforcement                  | CM     | **High**   | **Resolved**                              |
| AC-04 | RBAC cache can serve stale permissions (60s)                   | AC     | **Medium** | Open — accepted risk                      |
| AU-03 | Successful reads not individually logged                       | AU     | **Medium** | Open                                      |
| AU-04 | Username not included in structured logs                       | AU     | **Medium** | Open                                      |
| AU-05 | No SIEM integration documented                                 | AU     | **Medium** | **Resolved** — inherited control          |
| IA-03 | Dev token mechanism must be absent in prod                     | IA     | **Medium** | **Resolved**                              |
| IA-04 | No mutual TLS between microservices                            | IA     | **Medium** | **Resolved** — accepted risk documented   |
| SC-04 | Kafka TLS not enforced in production                           | SC     | **Medium** | **Resolved**                              |
| SC-05 | FIPS activation requires OS-level FIPS mode on OpenShift nodes | SC     | **Medium** | **Resolved** — operational req documented |
| SC-06 | Encryption at rest delegated to platform                       | SC     | **Medium** | **Resolved** — inherited control          |
| SI-01 | CSV formula injection in exports                               | SI     | **Medium** | **Resolved**                              |
| CM-02 | No secrets rotation mechanism                                  | CM     | **Medium** | Open                                      |
| CM-03 | No SCAP/STIG baseline mapping                                  | CM     | **Medium** | **Resolved**                              |
| IR-04 | Per-replica rate limiter (no cross-replica blocking)           | IR     | **Medium** | Accepted risk                             |
| RA-03 | No FIPS 199 security categorization                            | RA     | **Medium** | **Resolved**                              |
| RA-04 | No formal annual review cadence                                | RA     | **Medium** | Open                                      |
| AC-05 | No IP-based access restriction                                 | AC     | **Low**    | Open                                      |
| SI-02 | No runtime security alerting (RASP)                            | SI     | **Low**    | Open — platform                           |
| SI-03 | No SBOM in upstream CI                                         | SI     | **Low**    | Open                                      |
| CM-05 | No SBOM in upstream CI (duplicate)                             | CM     | **Low**    | Open                                      |


---

## Remediation Roadmap

### Phase 1 — High Priority (Target: 30 days)


| Priority | Action                                                                       | Owner        | Effort |
| -------- | ---------------------------------------------------------------------------- | ------------ | ------ |
| 1        | Enforce RBAC + DB TLS in non-development mode via `ValidateSecurityConfig()` | Backend      | 2 days |
| 2        | Add `org_id` and username to HTTP access log middleware                      | Backend      | 1 day  |
| 3        | Create incident response runbook with security event taxonomy                | Operations   | 1 week |
| 4        | Document trust boundary model (identity header, TLS termination)             | Architecture | 3 days |
| 5        | Add production guard for `DEVELOPMENT=true` and `ROS_TAGS_DEV_TOKEN`         | Backend      | 1 day  |
| 6        | Document FIPS deployment requirement (OpenShift nodes need `fips=1`) in SSP  | Compliance   | 1 day  |


### Phase 2 — Medium (Target: 90 days)


| Priority | Action                                                             | Owner        | Effort  |
| -------- | ------------------------------------------------------------------ | ------------ | ------- |
| 7        | Create OSCAL component definition                                  | Compliance   | 2 weeks |
| 8        | Produce system boundary diagram and external connections inventory | Architecture | 1 week  |
| 9        | Implement CSV formula injection sanitization                       | Backend      | 2 days  |
| 10       | Document secrets rotation procedures                               | Operations   | 1 week  |
| 11       | ~~Enforce Kafka TLS in production startup validation~~             | Backend      | Done    |
| 12       | Produce FIPS 199 security categorization document                  | Compliance   | 3 days  |
| 13       | Establish quarterly review cadence                                 | Security     | 1 day   |


### Phase 3 — Low + Documentation (Target: 180 days)


| Priority | Action                                                | Owner         | Effort    |
| -------- | ----------------------------------------------------- | ------------- | --------- |
| 14       | Generate SBOM in CI pipeline (syft or ko)             | Build/Release | 2 days    |
| 15       | Commission third-party penetration test               | Security      | 4-6 weeks |
| 16       | Produce formal NIST 800-30 risk assessment            | Compliance    | 2 weeks   |
| 17       | Create AlertManager rules for security events         | SRE           | 1 week    |
| 18       | Document SCAP/STIG compliance of container base image | Compliance    | 1 week    |


---

## Strengths to Highlight in SSP

The following areas demonstrate strong security posture and should be documented
as evidence of control implementation:

1. **FIPS 140-2/3 cryptography (SC-13)**: Red Hat `go-toolset:1.25` (golang-fips fork)
  automatically routes all Go crypto to FIPS-validated OpenSSL 3 when deployed on
   FIPS-enabled OpenShift nodes; `ubi9/ubi-minimal` runtime provides the validated library
2. **Input validation (SI-10)**: RFC 1123 charset allowlisting, parameterized SQL,
  SSRF prevention with DNS rebinding checks, size limits on all inputs
3. **Automated vulnerability detection (RA-5)**: Four independent scanning tools
  (govulncheck, CodeQL, Snyk, golangci-lint) running weekly and per-PR
4. **Security governance**: 302 ADRs, 7 adversarial review cycles with 162+ findings
  tracked and resolved, CHANGELOG discipline
5. **Least functionality (CM-7)**: Minimal container image (UBI-minimal), no
  unnecessary services, explicitly scoped API surface
6. **Rate limiting (AC-17)**: Per-org token bucket with configurable parameters,
  health endpoints exempted, Prometheus monitoring
7. **Service-to-service authentication (IA-5)**: Kubernetes TokenReview API with
  explicit SA allowlisting; validated at startup

---

## References

- [NIST SP 800-53 Rev 5](https://csrc.nist.gov/publications/detail/sp/800-53/rev-5/final)
- [FedRAMP CR26 Consolidated Rules](https://www.fedramp.gov/cr26/)
- [FedRAMP 20x](https://www.fedramp.gov/20x/)
- [OSCAL (Open Security Controls Assessment Language)](https://pages.nist.gov/OSCAL/)
- [FedRAMP Document Templates](https://www.fedramp.gov/documents-templates/)
- [NTC-0004: Certification Classes](https://www.fedramp.gov/updates/)
- [Adversarial Review v7 (2026-07-05)](../audit/adversarial-review-v6-2026-07-04.md)
- [Operations Configuration Guide](../operations/configuration.md)

---

*This assessment provides internal compliance guidance. It is not a formal 3PAO
assessment and does not constitute legal advice. Verify all requirements against
official FedRAMP sources. Engage a qualified assessor for formal authorization.*