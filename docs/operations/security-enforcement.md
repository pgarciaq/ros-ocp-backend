# Security Enforcement at Startup

ROS-OCP Backend validates security-critical configuration settings at process
startup. The enforcement behavior depends on the deployment context, following
the FedRAMP Shared Responsibility Model.

**Last updated:** 2026-07-05

---

## Enforcement Tiers

The system uses a three-tier graduated enforcement model:

| Tier | Condition | Behavior | Use Case |
|------|-----------|----------|----------|
| **None** | `DEVELOPMENT=true` | All checks skipped | Local development |
| **Warn** | Non-development, no Clowder, no explicit enforce | Findings logged as `SECURITY WARNING` at startup; process continues | On-prem production (default) |
| **Fatal** | Clowder detected (`ACG_CONFIG` set) OR `ROS_SECURITY_ENFORCE=true` | Process exits with descriptive error listing all violations | SaaS (console.redhat.com) or hardened on-prem |

### Tier Resolution Logic

```
DEVELOPMENT=true?
  → None (skip all)

ACG_CONFIG present? (Clowder/SaaS deployment)
  → Fatal (FedRAMP authorization boundary — no bypass)

ROS_SECURITY_ENFORCE=true?
  → Fatal (on-prem opt-in to strict mode)

Otherwise:
  → Warn (on-prem default — log findings, continue running)
```

### Special Case: Clowder + Development

If `DEVELOPMENT=true` is set alongside `ACG_CONFIG` (indicating a Clowder/SaaS
environment), the process **always fatals** regardless of enforcement tier. This
prevents accidentally deploying development mode inside the FedRAMP authorization
boundary.

---

## Security Checks Performed

| Finding Code | NIST Control | What Is Checked | Remediation |
|-------------|-------------|-----------------|-------------|
| `RBAC_DISABLED` | AC-3 | `RBAC_ENABLE=false` — authorization bypassed | Set `RBAC_ENABLE=true` and configure RBAC service |
| `DB_TLS_DISABLED` | SC-8 | `DB_SSL` is empty or `"disable"` | Set `DB_SSL=require`, `verify-ca`, or `verify-full` |
| `KAFKA_TLS_MISSING` | SC-8 | `KafkaSecurityProtocol` is empty, `PLAINTEXT`, or `SASL_PLAINTEXT` | Use `SASL_SSL` or `SSL` |
| `DEV_TOKEN_PRESENT` | IA-3 | `ROS_TAGS_DEV_TOKEN` is set | Remove the env var in production |
| `CSV_ALLOWLIST_EMPTY` | SI-10 | `ROS_CSV_ALLOWED_HOSTS` is empty | Set an explicit host allowlist for CSV downloads |
| `INTERNAL_AUTH_DISABLED` | AC-3 | `ROS_INTERNAL_TAGS_AUTH_REQUIRED=false` | Set to `true` to require auth on internal endpoints |

---

## Configuration Reference

| Variable | Default | Purpose |
|----------|---------|---------|
| `DEVELOPMENT` | `false` | Enables development mode; skips all security checks |
| `ROS_SECURITY_ENFORCE` | (unset) | Set to `true` to make security warnings fatal in on-prem deployments |
| `ACG_CONFIG` | (injected by Clowder) | When present, implies SaaS deployment; forces fatal enforcement |

---

## Deployment Scenarios

### Local Development

```bash
# .env file
DEVELOPMENT=true
# All security checks skipped — no warnings, no fatals.
# This is the standard developer workflow.
```

No additional configuration needed. Developers can run with insecure defaults
(no TLS, no RBAC, dev tokens present) without friction.

### On-Prem Production (Default)

```bash
# No DEVELOPMENT flag (defaults to false)
# No ROS_SECURITY_ENFORCE (defaults to warn)
RBAC_ENABLE=false
DB_SSL=disable
```

At startup, the process logs:

```
SECURITY WARNING [AC-3/RBAC_DISABLED]: RBAC_ENABLE=false — authorization is bypassed; all API requests are permitted without access checks
SECURITY WARNING [SC-8/DB_TLS_DISABLED]: DB_SSL="disable" — database traffic is unencrypted; set to 'require', 'verify-ca', or 'verify-full'
SECURITY WARNING [SC-8/KAFKA_TLS_MISSING]: KafkaSecurityProtocol="" — Kafka traffic may be unencrypted; use SASL_SSL or SSL
SECURITY: 3 finding(s) detected; set ROS_SECURITY_ENFORCE=true to make these fatal, or DEVELOPMENT=true for local development
```

The process **continues running**. The operator can triage and address findings
on their own schedule.

### On-Prem Production (Hardened)

```bash
ROS_SECURITY_ENFORCE=true
RBAC_ENABLE=true
DB_SSL=verify-full
# Kafka configured with SASL_SSL via Helm values
# No ROS_TAGS_DEV_TOKEN
ROS_CSV_ALLOWED_HOSTS=s3.internal.example.com
ROS_INTERNAL_TAGS_AUTH_REQUIRED=true
```

All checks pass → process starts normally. If any check fails, the process exits
with a descriptive error listing all violations.

### SaaS (Clowder / console.redhat.com)

Clowder injects `ACG_CONFIG` and configures TLS-enabled connections automatically.
The enforcement level is always Fatal. If Clowder misconfigures something (unlikely
but possible), the process exits immediately rather than running insecurely within
the FedRAMP authorization boundary.

---

## FedRAMP Compliance Rationale

This design satisfies FedRAMP requirements through the **Shared Responsibility Model**:

### Within Red Hat's Authorization Boundary (SaaS)

- **CM-6 (Configuration Settings):** Fatal enforcement ensures the most restrictive
  configuration consistent with operational requirements. No bypass possible.
- **AC-3 (Access Enforcement):** RBAC must be enabled; process won't start without it.
- **SC-8 (Transmission Confidentiality):** DB and Kafka TLS enforced at startup.
- **IA-3 (Device Identification and Authentication):** Dev tokens cannot exist in production.

### Customer's Authorization Boundary (On-Prem)

Red Hat provides:
1. **Secure warnings** — every insecure setting is flagged at startup
2. **Enforcement mechanism** — `ROS_SECURITY_ENFORCE=true` for customers ready to harden
3. **Documentation** — this guide and the FedRAMP audit report
4. **Defense-in-depth** — even without enforcement, individual features have their own guards

The customer, as system owner in their own ATO boundary, decides their risk posture.
This is standard FedRAMP practice and is documented in the SSP's Customer
Responsibility Matrix.

### Why Not Fatal Everywhere?

On-prem deployments legitimately operate without some controls:
- **DB TLS:** PostgreSQL sidecar in same pod (SDN + NetworkPolicy isolation)
- **Kafka TLS:** Single-node deployments with localhost-only Kafka
- **RBAC:** Single-user lab environments

Fataling on these would make the software unusable for valid deployment patterns.
The graduated model provides visibility (warnings) without blocking operations.

---

## Startup Output Examples

### Clean Start (No Findings)

```
config: initialized
Plugin registry: enabled plugins: [container, namespace, node, gpu, pvc, quota, vm, snapshot]
```

### Warn Level (On-Prem Default)

```
config: initialized
SECURITY WARNING [AC-3/RBAC_DISABLED]: RBAC_ENABLE=false — authorization is bypassed
SECURITY WARNING [SC-8/DB_TLS_DISABLED]: DB_SSL="disable" — database traffic is unencrypted
SECURITY: 2 finding(s) detected; set ROS_SECURITY_ENFORCE=true to make these fatal, or DEVELOPMENT=true for local development
Plugin registry: enabled plugins: [container, namespace, node, gpu, pvc, quota, vm, snapshot]
```

### Fatal Level (Enforcement Active)

```
config: initialized
config: FATAL: 2 security violation(s) detected in production mode:
  [AC-3/RBAC_DISABLED] RBAC_ENABLE=false — authorization is bypassed; all API requests are permitted without access checks
  [SC-8/DB_TLS_DISABLED] DB_SSL="disable" — database traffic is unencrypted; set to 'require', 'verify-ca', or 'verify-full'
Set DEVELOPMENT=true for local development or fix the configuration
```

---

## Relationship to Other Configuration

Security enforcement integrates with the existing configuration validation system:

| Component | File | Purpose |
|-----------|------|---------|
| Security enforcement (this) | `internal/config/security.go` | Fatal/warn on security misconfig |
| Config validation warnings | `internal/config/config_validation.go` | Non-fatal operational warnings (pool sizing, CORS, etc.) |
| Config value validation | `internal/config/config.go` (`validateLoadedConfig`) | Corrects invalid numeric values to safe defaults |

All three run during `runServiceStartup()` in `cmd/start.go` before any service
(API, processor, housekeeper) begins accepting traffic.
