# FedRAMP Compliance Documentation

This directory contains compliance artifacts supporting FedRAMP authorization
for ros-ocp-backend (Resource Optimization Service for OpenShift).

**Overall System Categorization:** Moderate (FIPS 199)  
**Target Certification Class:** C (CR26 Moderate baseline)

---

## Documents

| Document | NIST Control | Purpose |
|----------|-------------|---------|
| [Gap Assessment (Audit v1)](fedramp-audit-v1.md) | Multiple | Comprehensive gap analysis against NIST 800-53 Rev 5 |
| [Security Categorization](security-categorization.md) | RA-2 | FIPS 199 information type classification and impact levels |
| [Risk Assessment](risk-assessment.md) | RA-3 | NIST 800-30 threat/vulnerability analysis with risk matrix |
| [STIG Baseline Mapping](stig-baseline-mapping.md) | CM-2, CM-6 | SCAP/STIG/CIS compliance mapping for container base image |
| [Security Enforcement](../operations/security-enforcement.md) | AC-3, SC-8, CM-6, IA-3 | Graduated startup enforcement model (None/Warn/Fatal) |

---

## Quick Reference

- **Information types:** Optimization recommendations, cluster topology, usage metrics, org identifiers, cost estimates
- **Confidentiality:** Moderate | **Integrity:** Moderate | **Availability:** Low
- **Residual risk (post-mitigation):** 0 High, 0 Moderate, 5 Low (all accepted)
- **Base image:** UBI9-minimal (RHEL 9 STIG-aligned, FIPS-capable)
- **Build image:** UBI10 go-toolset with golang-fips (FIPS-validated crypto)

---

## Related Resources

- [Adversarial Security Reviews](../audit/) — 7 rounds, 162+ findings, 100% resolution
- [Architecture Decision Records](../architecture/adr/) — 302 ADRs documenting security trade-offs
- [Operations Guide](../operations/) — Runtime configuration and security settings
- [GitHub Issues (fedramp label)](https://github.com/pgarciaq/ros-ocp-backend/issues?q=label%3Afedramp) — Tracking remaining remediation items
