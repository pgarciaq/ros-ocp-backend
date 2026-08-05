# Deployment Hardening Guide

> **Last verified:** 2026-08-06

This guide provides step-by-step instructions for deploying ROS-OCP Backend with
FedRAMP-equivalent security controls in on-premise environments. Following these
steps brings an on-prem deployment to the same security posture as the managed
SaaS offering.

---

## Prerequisites

Before starting, ensure your environment meets these requirements:

| Requirement | Purpose | How to Verify |
|-------------|---------|---------------|
| OpenShift 4.12+ | Platform with NetworkPolicy support | `oc version` |
| FIPS mode enabled on nodes | FIPS-validated cryptography | `cat /proc/sys/crypto/fips_enabled` → `1` |
| TLS certificates available | DB and Kafka encryption in transit | Certificate chain from your CA |
| Keycloak / RHBK deployed | Identity provider with MFA | Keycloak admin console accessible |
| cost-onprem Helm chart | Deployment vehicle | `helm list -n cost-onprem` |

---

## Step 1: Enable FIPS Mode on Cluster Nodes

FIPS mode must be enabled at the OpenShift installation time (it cannot be
toggled post-install on RHCOS).

**Verification:**

```bash
# From any cluster node:
cat /proc/sys/crypto/fips_enabled
# Expected output: 1

# From a running ROS pod:
oc exec deployment/cost-onprem-ros-api -n cost-onprem -- \
  cat /proc/sys/crypto/fips_enabled
```

!!! warning "Cannot be enabled post-install"
    If your cluster was not installed with FIPS mode, you must reinstall.
    See [OpenShift FIPS documentation](https://docs.openshift.com/container-platform/latest/installing/installing-fips.html).

**What this achieves:** SC-13 (Cryptographic Protection) — all Go crypto operations
route to the FIPS-validated OpenSSL 3.0 module.

---

## Step 2: Configure Database TLS

Set PostgreSQL connection to use TLS with certificate verification.

**Helm values:**

```yaml
database:
  sslMode: "verify-full"
  sslRootCert: "/etc/ssl/certs/db-ca.crt"
```

**Environment variable (direct):**

```bash
ROS_DB_SSLMODE=verify-full
ROS_DB_SSLROOTCERT=/etc/ssl/certs/db-ca.crt
```

**Verification:**

```bash
# Check the startup log for DB TLS status:
oc logs deployment/cost-onprem-ros-api -n cost-onprem | grep -i "sslmode"
# Should NOT show: "SECURITY WARNING: DB_INSECURE"
```

**What this achieves:** SC-8 (Transmission Confidentiality) — database connections
encrypted with verified server certificates.

---

## Step 3: Configure Kafka TLS (SASL_SSL)

Ensure Kafka connections use SASL authentication over TLS.

**Helm values:**

```yaml
kafka:
  securityProtocol: "SASL_SSL"
  saslMechanism: "SCRAM-SHA-512"
  sslCaLocation: "/etc/ssl/certs/kafka-ca.crt"
```

**Environment variables (direct):**

```bash
KAFKA_SECURITY_PROTOCOL=SASL_SSL
KAFKA_SASL_MECHANISM=SCRAM-SHA-512
KAFKA_SSL_CA_LOCATION=/etc/ssl/certs/kafka-ca.crt
```

**Verification:**

```bash
# Check startup logs:
oc logs deployment/cost-onprem-ros-api -n cost-onprem | grep -i "kafka"
# Should NOT show: "SECURITY WARNING: KAFKA_INSECURE"
```

**What this achieves:** SC-8 (Transmission Confidentiality) — Kafka messages
encrypted in transit with authenticated producers/consumers.

---

## Step 4: Enable Fatal Security Enforcement

By default, on-prem deployments use **Warn** mode (logs security issues but
continues running). For regulated environments, enable **Fatal** mode:

```bash
ROS_SECURITY_ENFORCE=true
```

**Helm values:**

```yaml
env:
  ROS_SECURITY_ENFORCE: "true"
```

With fatal enforcement, the service will **refuse to start** if any of these
conditions are detected:

| Check | What It Validates |
|-------|-------------------|
| RBAC disabled | `RBAC_ENABLE` must be `true` |
| DB TLS disabled | `sslmode` must not be `disable` or empty |
| Kafka insecure | Security protocol must not be `PLAINTEXT` or `SASL_PLAINTEXT` |
| Dev token present | `ROS_TAGS_DEV_TOKEN` must be empty |
| CSV hosts unrestricted | `ROS_CSV_ALLOWED_HOSTS` must be set |
| Internal auth disabled | `ROS_INTERNAL_TAGS_AUTH_REQUIRED` must be `true` |

**Verification:**

```bash
# Pod should start successfully with no SECURITY WARNING lines:
oc logs deployment/cost-onprem-ros-api -n cost-onprem | grep "SECURITY"
# Expected: no output (all checks pass)

# If misconfigured, pod will be in CrashLoopBackOff:
oc get pods -n cost-onprem -l app.kubernetes.io/component=ros-api
```

**What this achieves:** CM-6 (Configuration Settings) — insecure configurations
prevented from reaching production.

---

## Step 5: Deploy with NetworkPolicy

The cost-onprem Helm chart includes NetworkPolicy by default. Verify it is active:

```bash
oc get networkpolicy -n cost-onprem
```

Expected policies:

- **deny-all-ingress** — blocks all inbound traffic by default
- **allow-from-ingress** — permits traffic from OpenShift Router only
- **allow-internal** — permits pod-to-pod within the namespace

If your cluster uses a CNI that does not support NetworkPolicy (rare), consider
deploying OpenShift Service Mesh for equivalent isolation.

**What this achieves:** SC-7 (Boundary Protection) — only authorized traffic
reaches ROS pods.

---

## Step 6: Configure Identity Provider with MFA

Deploy Keycloak (RHBK) as the OAuth2 proxy backend with multi-factor authentication:

1. **Create a realm** for cost management users
2. **Enable MFA** (TOTP or WebAuthn) as a required authentication step
3. **Configure the OAuth2 Proxy** sidecar on the UI deployment to use Keycloak
4. **Set `org_id`** as a user attribute (bare number, e.g., `1234567` — not `org1234567`)

**Verification:**

```bash
# Test login flow — should require MFA:
curl -v https://<ros-route>/api/cost-management/v1/status/
# Expected: 302 redirect to Keycloak login
```

**What this achieves:** IA-2(1) (Multi-Factor Authentication) — users must
present two factors before accessing the service.

---

## Step 7: Enable Audit Log Forwarding

Configure OpenShift's ClusterLogForwarder to send ROS logs to your SIEM:

```yaml
apiVersion: logging.openshift.io/v1
kind: ClusterLogForwarder
metadata:
  name: instance
spec:
  pipelines:
    - name: ros-to-siem
      inputRefs:
        - application
      outputRefs:
        - siem-endpoint
      filterRefs:
        - ros-namespace
  filters:
    - name: ros-namespace
      type: kubeAPIAudit
      kubeAPIAudit:
        namespaces:
          - cost-onprem
  outputs:
    - name: siem-endpoint
      type: syslog  # or splunk, elasticsearch, cloudwatch
      url: "tls://siem.example.com:6514"
```

**What this achieves:** AU-6 (Audit Review) — logs forwarded to a centralized
SIEM for correlation, alerting, and long-term retention.

---

## Step 8: Validate Deployment

Run through this checklist to confirm all controls are active:

| # | Check | Command | Expected |
|---|-------|---------|----------|
| 1 | FIPS mode active | `oc exec deploy/cost-onprem-ros-api -- cat /proc/sys/crypto/fips_enabled` | `1` |
| 2 | No security warnings | `oc logs deploy/cost-onprem-ros-api \| grep "SECURITY"` | No output |
| 3 | DB TLS active | `oc logs deploy/cost-onprem-ros-api \| grep sslmode` | `verify-full` |
| 4 | Kafka TLS active | `oc logs deploy/cost-onprem-ros-api \| grep protocol` | `SASL_SSL` |
| 5 | RBAC enabled | `oc logs deploy/cost-onprem-ros-api \| grep rbac` | `enabled` |
| 6 | NetworkPolicy active | `oc get netpol -n cost-onprem` | Policies listed |
| 7 | MFA required | Browser → ROS route → redirects to Keycloak | MFA prompt shown |
| 8 | Pod running (not CrashLoop) | `oc get pods -n cost-onprem -l app.kubernetes.io/component=ros-api` | `Running` |

---

## Security Configuration Reference

All security-related environment variables:

| Variable | Default (On-Prem) | Hardened Value | Purpose |
|----------|-------------------|---------------|---------|
| `ROS_SECURITY_ENFORCE` | `false` | `true` | Fatal enforcement mode |
| `RBAC_ENABLE` | `false` | `true` | Enable RBAC permission checks |
| `ROS_DB_SSLMODE` | `disable` | `verify-full` | PostgreSQL TLS mode |
| `KAFKA_SECURITY_PROTOCOL` | `PLAINTEXT` | `SASL_SSL` | Kafka transport security |
| `ROS_TAGS_DEV_TOKEN` | (empty) | (empty) | Must remain empty in production |
| `ROS_CSV_ALLOWED_HOSTS` | (empty) | Explicit allowlist | S3/MinIO hosts for CSV fetch |
| `ROS_INTERNAL_TAGS_AUTH_REQUIRED` | `false` | `true` | Require SA auth on internal endpoints |
| `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` | (empty) | Explicit SA list | Allowlisted service accounts |
| `DEVELOPMENT` | `false` | `false` | Must be false in production |
| `ROS_RBAC_CACHE_TTL` | `60s` | `60s` | Permission cache TTL |

---

## Common Mistakes

| Mistake | Symptom | Fix |
|---------|---------|-----|
| `DEVELOPMENT=true` in production | All security checks skipped | Set to `false` or remove entirely |
| `ROS_TAGS_DEV_TOKEN` set in prod | Static auth bypass available | Remove the variable |
| DB `sslmode=disable` with external DB | Credentials sent in cleartext | Set `verify-full` + provide CA cert |
| Kafka `PLAINTEXT` protocol | Messages unencrypted on wire | Switch to `SASL_SSL` |
| Reusing image tags | Old (possibly vulnerable) image cached | Always use unique tags (`imagePullPolicy: IfNotPresent`) |
| Missing NetworkPolicy | All cluster traffic can reach ROS | Deploy with Helm chart defaults (includes policies) |
| `org_id` set as `org1234567` in Keycloak | Schema becomes `orgorg1234567` | Use bare number: `1234567` |

---

## Mapping to NIST 800-53 Controls

| Step | Primary Control | Enhancement |
|------|----------------|-------------|
| 1. FIPS mode | SC-13 | Cryptographic Protection |
| 2. DB TLS | SC-8 | Transmission Confidentiality |
| 3. Kafka TLS | SC-8 | Transmission Confidentiality |
| 4. Fatal enforcement | CM-6 | Configuration Settings |
| 5. NetworkPolicy | SC-7 | Boundary Protection |
| 6. MFA | IA-2(1) | Multi-Factor Authentication |
| 7. Log forwarding | AU-6 | Audit Review, Analysis, Reporting |
| 8. Validation | CA-7 | Continuous Monitoring |
