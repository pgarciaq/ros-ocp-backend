# SCAP/STIG Baseline Mapping

**System:** ROS-OCP-Backend (Resource Optimization Service for OpenShift)  
**Date:** 2026-07-05  
**NIST Control:** CM-2 (Baseline Configuration), CM-6 (Configuration Settings)  
**Base Image:** `registry.access.redhat.com/ubi9/ubi-minimal:latest`  
**Build Image:** `registry.access.redhat.com/ubi10/go-toolset:1.25`

---

## 1. Purpose

This document maps the container base image and runtime configuration against
applicable STIG and CIS benchmarks, documenting compliance status and any
deviations.

---

## 2. Applicable Benchmarks

| Benchmark | Version | Applicability |
|-----------|---------|---------------|
| DISA RHEL 9 STIG | V2R2+ | UBI9-minimal is a subset of RHEL 9 |
| CIS Red Hat Enterprise Linux 9 Benchmark | v2.0.0 | Container-relevant controls |
| DISA Container Platform SRG | V2R1 | Container runtime controls |
| NIST SP 800-190 | — | Application container security |

---

## 3. Base Image Compliance: UBI9-Minimal

Red Hat Universal Base Image 9 Minimal (`ubi9/ubi-minimal`) inherits RHEL 9's
security posture with a minimal package set. The following STIG controls are
addressed by the base image:

### 3.1 Controls Satisfied by UBI9-Minimal

| STIG ID | Title | Status | Evidence |
|---------|-------|--------|----------|
| RHEL-09-211010 | FIPS mode must be enabled | **Conditional** | UBI9 supports FIPS when host kernel has `fips=1`; crypto libraries link to FIPS-validated OpenSSL |
| RHEL-09-211015 | Must use DOD-approved PKI | Inherited | Red Hat CA certificates in `/etc/pki/tls/` |
| RHEL-09-212010 | Must implement ASLR | Satisfied | Kernel-level ASLR; Go PIE binaries by default |
| RHEL-09-213010 | Must not have unnecessary services | Satisfied | Minimal image: no systemd, no SSH, no cron, no NFS |
| RHEL-09-214010 | Must configure firewall | N/A | Container networking managed by OpenShift SDN/OVN |
| RHEL-09-215010 | Must enable SELinux | Inherited | OpenShift enforces SELinux on container runtime |
| RHEL-09-231010 | Must use encrypted communications | Satisfied | TLS at ingress; FIPS-validated crypto in runtime |
| RHEL-09-252010 | Must configure audit system | N/A | No auditd in container; logging via stdout/stderr to platform |
| RHEL-09-271010 | Must use centralized authentication | N/A | Service does not provide interactive login |

### 3.2 Controls Addressed by Application Configuration

| STIG ID | Title | Status | Implementation |
|---------|-------|--------|----------------|
| RHEL-09-232010 | Must use TLS 1.2+ | Satisfied | Go `crypto/tls` defaults to TLS 1.2 minimum; FIPS mode enforces approved ciphers |
| RHEL-09-255010 | Must configure session timeouts | N/A | Stateless REST API; no sessions |
| RHEL-09-611010 | Must enforce password complexity | N/A | No local authentication; identity from platform gateway |
| RHEL-09-672010 | Must restrict access to configuration | Satisfied | Container filesystem is read-only (`readOnlyRootFilesystem: true` in deployment) |

### 3.3 Controls Not Applicable to Containers

| STIG ID | Title | Reason |
|---------|-------|--------|
| RHEL-09-241* | Mail, NFS, FTP services | Not installed in minimal image |
| RHEL-09-251* | DNS, DHCP configuration | Managed by OpenShift cluster |
| RHEL-09-261* | GUI/X11 settings | No GUI in container |
| RHEL-09-291* | Physical security, USB | No physical access to container |
| RHEL-09-631* | PAM configuration | No interactive login |

---

## 4. Container Platform SRG Compliance

| SRG ID | Title | Status | Evidence |
|--------|-------|--------|----------|
| SRG-APP-000014 | Access enforcement | Satisfied | RBAC middleware with role-based permission checks |
| SRG-APP-000023 | Audit content | Satisfied | Structured JSON logs with org_id, request_id, method, path, status |
| SRG-APP-000033 | Least privilege | Satisfied | Non-root container (UID 1001); minimal capabilities |
| SRG-APP-000065 | Session controls | N/A | Stateless REST API |
| SRG-APP-000089 | Encryption in transit | Satisfied | TLS at ingress; DB TLS configurable; Kafka TLS configurable |
| SRG-APP-000092 | Input validation | Satisfied | RFC 1123 charset allowlist; SQL injection prevention via parameterized queries |
| SRG-APP-000118 | Error handling | Satisfied | No stack traces or internal details in HTTP responses |
| SRG-APP-000131 | Configuration lockdown | Satisfied | Immutable container image; config via environment only |
| SRG-APP-000141 | Vulnerability scanning | Satisfied | govulncheck + CodeQL + Snyk + golangci-lint per-PR |
| SRG-APP-000148 | FIPS cryptography | Conditional | FIPS-validated when deployed on FIPS-enabled OpenShift nodes |
| SRG-APP-000176 | Software integrity | Satisfied | Signed container images via Cosign (downstream Konflux pipeline) |
| SRG-APP-000190 | Resource limits | Satisfied | Kubernetes resource requests/limits enforced |
| SRG-APP-000225 | Health monitoring | Satisfied | `/healthz`, `/readyz` endpoints; Prometheus metrics |

---

## 5. CIS Benchmark Mapping (Container-Relevant)

| CIS Control | Title | Status | Notes |
|-------------|-------|--------|-------|
| 1.1.1 | Ensure separate partition for /tmp | N/A | Ephemeral container filesystem |
| 1.4.1 | Ensure bootloader password | N/A | No bootloader in container |
| 3.4.1 | Ensure firewall is active | Inherited | OpenShift NetworkPolicy |
| 4.1.1 | Ensure auditd is enabled | N/A | Platform-level audit (Falco/AuditD on host) |
| 5.2.1 | Ensure SSH config is correct | N/A | No SSH in container |
| 5.4.1 | Ensure password policies | N/A | No local accounts |
| 6.1.1 | Ensure permissions on system files | Satisfied | Read-only root filesystem; files owned by non-root UID |
| 6.2.1 | Ensure no duplicate UIDs | Satisfied | Single UID (1001) in container |

---

## 6. Build Image Security (go-toolset:1.25)

The build stage uses `ubi10/go-toolset:1.25` which is:
- Based on UBI10 (RHEL 10 derivative)
- Uses Red Hat's `golang-fips` fork that dynamically links to FIPS-validated OpenSSL
- Produces statically-linked binaries (with dynamic crypto linkage for FIPS)
- Build artifacts do not ship in the runtime image (multi-stage build)

**Build security controls:**
- No build tools in runtime image (compiler, linker, etc.)
- Vendored dependencies (`vendor/` directory) — no network fetches during build
- Reproducible builds via pinned Go version and vendored modules

---

## 7. Runtime Configuration Hardening

The following Kubernetes security context settings are recommended/required:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 1001
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop: ["ALL"]
  seccompProfile:
    type: RuntimeDefault
```

**Current status:** These are set in the Helm chart (`cost-onprem-chart`) and
Clowder deployment templates. The application does not require any elevated
capabilities.

---

## 8. Deviations and Compensating Controls

| Deviation | Justification | Compensating Control |
|-----------|---------------|---------------------|
| No auditd in container | Standard container practice; audit at platform level | Structured application logs to stdout; platform-level AuditD on OpenShift nodes |
| No host firewall | Container networking is SDN-managed | OpenShift NetworkPolicy restricts pod-to-pod traffic |
| FIPS conditional on host | Go crypto links dynamically to host OpenSSL | Operational requirement: deploy on FIPS-enabled nodes |
| No ClamAV/antivirus | No user-uploaded executables processed | Input is structured CSV/JSON only; validated at ingestion |

---

## 9. Scanning Evidence

| Tool | Scope | Schedule | Last Clean Run |
|------|-------|----------|----------------|
| OpenSCAP (via UBI rebuild) | Base image CVEs | Per Red Hat rebuild cycle | Inherited from UBI9 |
| govulncheck | Go module vulnerabilities | Per-PR + weekly | 2026-07-05 |
| CodeQL | Source code SAST | Per-PR + weekly | 2026-07-05 |
| golangci-lint | 110+ static analysis rules | Per-PR | 2026-07-05 |
| Snyk Container | Runtime image CVEs | Per-build (downstream) | Continuous |
| Red Hat Container Certification | Certification pipeline | Per-release (downstream) | — |

---

## 10. References

- [DISA RHEL 9 STIG](https://public.cyber.mil/stigs/downloads/?_dl_facet_stigs=operating-systems%2Cunix-linux)
- [CIS Red Hat Enterprise Linux 9 Benchmark](https://www.cisecurity.org/benchmark/red_hat_linux)
- [DISA Container Platform SRG](https://public.cyber.mil/stigs/downloads/?_dl_facet_stigs=application-servers)
- [NIST SP 800-190: Application Container Security Guide](https://csrc.nist.gov/publications/detail/sp/800-190/final)
- [Red Hat UBI9 Security Content](https://access.redhat.com/articles/4238681)
- [FIPS 199 Security Categorization](security-categorization.md)

---

## 11. Review Schedule

This mapping shall be reviewed:
- When the base image major version changes (e.g., UBI9 → UBI10)
- When new STIG versions are released for RHEL 9
- When significant application architecture changes occur
- Annually as part of the continuous monitoring program
