# FIPS 199 Security Categorization

**System:** ROS-OCP-Backend (Resource Optimization Service for OpenShift)  
**Date:** 2026-07-05  
**Prepared by:** Security Engineering  
**NIST Control:** RA-2 (Security Categorization)  
**Reference:** [FIPS 199](https://csrc.nist.gov/publications/detail/fips/199/final), [NIST SP 800-60 Vol 1](https://csrc.nist.gov/publications/detail/sp/800-60/vol-1-rev-1/final)

---

## 1. System Description

ROS-OCP-Backend is a REST API service that processes OpenShift cluster
telemetry (CPU, memory, storage, GPU usage metrics) and produces resource
optimization recommendations for containers, namespaces, PVCs, nodes,
MachineSet scaling, cluster quotas, and VM snapshots.

The service operates within the Red Hat Hybrid Cloud Console platform (SaaS)
and as an on-premise deployment within customer-controlled OpenShift clusters.

---

## 2. Information Types

The following information types are processed, stored, or transmitted by the
system. Classification follows NIST SP 800-60 Volume II guidance.

| # | Information Type | SP 800-60 Mapping | Description |
|---|-----------------|-------------------|-------------|
| 1 | Resource optimization recommendations | C.3.5.1 System Development | Right-sizing recommendations for CPU, memory, storage per container/namespace |
| 2 | Cluster topology metadata | D.14 General Information | Node names, namespace names, workload names, cluster UUIDs, MachineSet names |
| 3 | Usage telemetry metrics | C.3.5.1 System Development | CPU/memory request/limit/usage values, storage capacity, GPU utilization |
| 4 | Organization identifiers | C.2.8.7 General Government | org_id, cluster_alias (user-defined display name) |
| 5 | Estimated cost/savings data | C.3.1.4 Goods Acquisition | Derived monthly savings/waste estimates from usage-to-cost mapping |
| 6 | Recommendation quality metrics | C.3.5.1 System Development | Stability percentages, adoption detection, OOM event correlation |

---

## 3. Impact Assessment

### 3.1 Per-Information-Type Impact

| Information Type | Confidentiality | Integrity | Availability |
|-----------------|----------------|-----------|--------------|
| Resource optimization recommendations | **Moderate** | **Moderate** | Low |
| Cluster topology metadata | **Moderate** | Low | Low |
| Usage telemetry metrics | Low | **Moderate** | Low |
| Organization identifiers | **Moderate** | Low | Low |
| Estimated cost/savings data | **Moderate** | Low | Low |
| Recommendation quality metrics | Low | Low | Low |

### 3.2 Impact Justification

**Confidentiality — Moderate:**
- Recommendations and topology expose infrastructure sizing, which could allow
  competitors or adversaries to infer capacity planning and budget allocation.
- Organization identifiers combined with cluster topology could identify specific
  customers and their deployment patterns.
- Cost/savings estimates reveal financial optimization targets.
- Data is not PII, PHI, or classified — impact is limited to competitive/
  operational sensitivity. HIGH is not warranted.

**Integrity — Moderate:**
- Corrupted optimization recommendations could lead to over-provisioning (wasted
  resources / inflated costs) or under-provisioning (application instability).
- Impact is operational and financial, not safety-critical. Workloads have their
  own resource limits independent of recommendations.
- Corrupted usage metrics would cascade to incorrect recommendations, but
  self-correcting on next ingestion cycle (metrics are recomputed hourly).

**Availability — Low:**
- The service is advisory — workloads continue running unaffected if
  recommendations are unavailable.
- No real-time control plane dependency. Customers can operate their clusters
  normally without ROS.
- Outages impact cost optimization visibility only, not service availability.

### 3.3 Overall System Categorization

Per FIPS 199 §4, the overall information system security category is the
**high watermark** across all information types:

```
SC ros-ocp-backend = {
    (confidentiality, MODERATE),
    (integrity, MODERATE),
    (availability, LOW)
}
```

**Overall categorization: MODERATE**

---

## 4. FedRAMP Certification Class

Under CR26 (FedRAMP Consolidated Rules for 2026), a Moderate categorization
maps to **Certification Class C** (Moderate baseline).

This requires implementation of NIST SP 800-53 Rev 5 Moderate controls and
authorization through a 3PAO assessment.

---

## 5. Data Not Processed

The following data types are explicitly **out of scope** — ros-ocp-backend
does not process, store, or transmit:

- Personally Identifiable Information (PII)
- Protected Health Information (PHI)
- Payment Card Industry (PCI) data
- Authentication credentials (passwords, tokens, keys)
- Billing invoices or actual financial transactions
- Source code or intellectual property
- Classified or export-controlled information

Organization identifiers (`org_id`) are opaque numeric identifiers that cannot
be reverse-mapped to organization names without access to the separate IAM
system.

---

## 6. Boundary Considerations

| Boundary | Responsibility |
|----------|---------------|
| Identity verification (authentication) | Inherited from platform gateway (3scale/Akamai) |
| TLS termination | Inherited from OpenShift ingress controller |
| Encryption at rest (database) | Inherited from PostgreSQL/platform storage |
| Encryption at rest (object storage) | Inherited from S3/MinIO encryption configuration |
| Network segmentation | Inherited from OpenShift NetworkPolicy / SDN |
| Physical security | Inherited from cloud provider (AWS/GCP) or customer datacenter |

---

## 7. Review Schedule

This categorization shall be reviewed:
- Annually (minimum)
- When new information types are added to the system
- When the system boundary changes (new integrations, new data sources)
- When the threat environment changes materially

---

## 8. Approval

| Role | Name | Date |
|------|------|------|
| System Owner | ___________________ | __________ |
| Information Security Officer | ___________________ | __________ |
| Authorizing Official | ___________________ | __________ |
