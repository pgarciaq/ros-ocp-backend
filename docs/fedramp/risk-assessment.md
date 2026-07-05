# Risk Assessment

**System:** ROS-OCP-Backend (Resource Optimization Service for OpenShift)  
**Date:** 2026-07-05  
**Methodology:** NIST SP 800-30 Rev 1 (Guide for Conducting Risk Assessments)  
**NIST Control:** RA-3 (Risk Assessment)  
**Categorization:** Moderate (see [security-categorization.md](security-categorization.md))

---

## 1. Purpose

This document identifies threats, vulnerabilities, likelihood, and impact for
ros-ocp-backend per NIST SP 800-30 Rev 1. It supports the FedRAMP authorization
package and informs security control selection.

---

## 2. System Context

ROS-OCP-Backend is a stateless REST API service that:
- Receives cluster telemetry via Kafka (from koku-metrics-operator)
- Stores processed recommendations in PostgreSQL
- Serves recommendations via REST API to authenticated users
- Exports data as CSV for offline analysis

**Deployment models:** Red Hat Hybrid Cloud Console (SaaS) and customer on-premise
OpenShift clusters.

**Trust boundaries:**
1. External → API Gateway (3scale/Akamai) — TLS termination, identity injection
2. API Gateway → ros-ocp-backend — HTTP within cluster network
3. ros-ocp-backend → PostgreSQL — Configurable TLS
4. ros-ocp-backend ← Kafka — Configurable SASL/TLS
5. ros-ocp-backend → Kruize (recommendation engine) — Internal HTTP

---

## 3. Threat Sources

| ID | Threat Source | Type | Capability | Intent | Targeting |
|----|--------------|------|-----------|--------|-----------|
| TS-1 | External attacker (internet) | Adversarial | Moderate | High | Opportunistic |
| TS-2 | Malicious insider (org admin) | Adversarial | High | Moderate | Targeted |
| TS-3 | Compromised upstream dependency | Adversarial | High | High | Targeted |
| TS-4 | Misconfigured deployment | Non-adversarial | N/A | N/A | N/A |
| TS-5 | Platform infrastructure failure | Non-adversarial | N/A | N/A | N/A |
| TS-6 | Compromised peer service (Kafka, Kruize) | Adversarial | Moderate | Moderate | Targeted |

---

## 4. Vulnerability Inventory

Derived from 7 rounds of adversarial security review (162+ findings) and
automated scanning (govulncheck, CodeQL, Snyk, golangci-lint).

| ID | Vulnerability | Threat Source | Current Mitigation | Residual Risk |
|----|--------------|---------------|-------------------|---------------|
| V-1 | Identity header not cryptographically signed | TS-1, TS-6 | Network perimeter trust (gateway strips/replaces header); entitlement check | Low — requires network access to inject |
| V-2 | RBAC disabled by default in on-prem mode | TS-2, TS-4 | Startup validation warns; production enforcement planned (#168) | Medium — until #168 implemented |
| V-3 | DB TLS disabled by default in on-prem mode | TS-1, TS-4 | Configurable via `DBssl`; enforcement planned (#168) | Medium — until #168 implemented |
| V-4 | In-memory rate limiter (per-replica, no cross-replica state) | TS-1 | Per-org token bucket; burst limits; monitoring | Low — accepted risk; distributed attack limited |
| V-5 | CSV exports could contain formula injection | TS-2 | Sanitized with single-quote prefix (resolved #170) | **Resolved** |
| V-6 | S3 endpoint could be SSRF vector | TS-4, TS-6 | SSRF filter: private IP blocking, DNS resolution, HTTPS enforcement | **Resolved** |
| V-7 | Kafka consumer panic could block partition | TS-6 | Panic recovery with commit-skip; Prometheus monitoring | **Resolved** |
| V-8 | CloudWatch credentials in process environment | TS-4 | Standard practice for containerized apps; env vars not logged | Low — accepted |
| V-9 | No mutual TLS between microservices | TS-6 | NetworkPolicy isolation; Kubernetes RBAC on service accounts | Low — inherited from platform |
| V-10 | DEVELOPMENT flag disables security controls | TS-4 | Startup validation planned (#168); not set in Clowder | Medium — until #168 implemented |

---

## 5. Risk Determination

### 5.1 Likelihood Scale

| Level | Description |
|-------|-------------|
| Very Low | Unlikely to occur; requires highly sophisticated attack |
| Low | Possible but improbable given existing controls |
| Moderate | Could reasonably occur; partial controls exist |
| High | Likely to occur; insufficient controls |
| Very High | Almost certain; no controls in place |

### 5.2 Impact Scale

| Level | Description |
|-------|-------------|
| Very Low | Negligible effect on operations |
| Low | Minor degradation; quickly recoverable |
| Moderate | Significant degradation; financial or reputational impact |
| High | Major damage; extended outage or data breach |
| Very High | Catastrophic; complete system compromise or large-scale data loss |

### 5.3 Risk Matrix

| Risk ID | Vulnerability | Likelihood | Impact | Risk Level | Response |
|---------|--------------|-----------|--------|-----------|----------|
| R-1 | V-1: Unsigned identity header | Very Low | Moderate | **Low** | Accept — inherited control from gateway |
| R-2 | V-2: RBAC disabled by default | Moderate | High | **High** | Mitigate — startup enforcement (#168) |
| R-3 | V-3: DB TLS disabled by default | Moderate | Moderate | **Moderate** | Mitigate — startup enforcement (#168) |
| R-4 | V-4: Per-replica rate limiter | Low | Low | **Low** | Accept — monitor; document operational guidance |
| R-5 | V-8: Credentials in environment | Very Low | Moderate | **Low** | Accept — standard container practice |
| R-6 | V-9: No mTLS between services | Low | Moderate | **Low** | Accept — inherited from platform NetworkPolicy |
| R-7 | V-10: DEVELOPMENT flag weakness | Moderate | High | **High** | Mitigate — startup enforcement (#168) |

### 5.4 Risk Summary

| Risk Level | Count | Percentage |
|-----------|-------|------------|
| High | 2 | 29% |
| Moderate | 1 | 14% |
| Low | 4 | 57% |

---

## 6. Risk Response Plan

| Risk ID | Response | Action | Target Date | Owner |
|---------|----------|--------|-------------|-------|
| R-2 | Mitigate | Implement startup security validation (#168) | Q3 2026 | Engineering |
| R-3 | Mitigate | Include DB TLS in startup validation (#168) | Q3 2026 | Engineering |
| R-7 | Mitigate | Include DEVELOPMENT flag guard in startup validation (#168) | Q3 2026 | Engineering |
| R-1 | Accept | Document trust boundary model in SSP | Q3 2026 | Compliance |
| R-4 | Accept | Document in operations guide; add Prometheus alert for rate limit exhaustion | Done | Engineering |
| R-5 | Accept | Standard container credential injection pattern | N/A | — |
| R-6 | Accept | Inherited from OpenShift platform; document in SSP | Q3 2026 | Compliance |

---

## 7. Controls Effectiveness Summary

Based on 7 adversarial review rounds (v1–v7) with 162+ findings:

| Category | Findings | Resolved | Resolution Rate |
|----------|----------|----------|-----------------|
| Critical/Warning | 23 | 23 | 100% |
| Notes/Informational | 12 | 12 | 100% |
| Total (latest round v7) | 10 | 10 | 100% |

**Automated scanning coverage:**

| Tool | Scope | Frequency | Last Run |
|------|-------|-----------|----------|
| govulncheck | Go vulnerability database | Per-PR + weekly | Continuous |
| CodeQL | SAST (Go) | Per-PR + weekly | Continuous |
| golangci-lint | Static analysis (110+ linters) | Per-PR | Continuous |
| Snyk | Supply chain (downstream) | Per-build | Continuous |
| Renovate | Dependency freshness | Daily | Continuous |

---

## 8. Residual Risk Statement

After implementation of the planned mitigations (startup security enforcement,
#168), the residual risk profile will be:

- **High risks:** 0
- **Moderate risks:** 0
- **Low risks:** 5 (all accepted with documented justification)

The system's residual risk is consistent with a Moderate categorization and
acceptable for FedRAMP Certification Class C authorization.

---

## 9. Review Schedule

| Activity | Frequency | Next Due |
|----------|-----------|----------|
| Full risk assessment update | Annual | 2027-07-05 |
| Adversarial security review | Quarterly | 2026-10-05 |
| Automated scan review | Monthly | 2026-08-05 |
| Threat landscape review | Semi-annual | 2027-01-05 |
| Post-incident risk reassessment | Event-driven | As needed |

---

## 10. References

- [NIST SP 800-30 Rev 1](https://csrc.nist.gov/publications/detail/sp/800-30/rev-1/final)
- [FIPS 199 Security Categorization](security-categorization.md)
- [FedRAMP Gap Assessment v1](fedramp-audit-v1.md)
- [Adversarial Review v7](../audit/adversarial-review-v6-2026-07-04.md)
- [GitHub Issue #168: Startup Security Enforcement](https://github.com/pgarciaq/ros-ocp-backend/issues/168)

---

## 11. Approval

| Role | Name | Date |
|------|------|------|
| Risk Assessment Lead | ___________________ | __________ |
| System Owner | ___________________ | __________ |
| Authorizing Official | ___________________ | __________ |
