-- #760 (V15-10): direct off-hours evidence for the W8 sleep predicate.
-- The predicate subtracted sums of percentiles (all-hours p50 - BH p50) as
-- if additive usage totals — a constant 300m workload reads outside=0 and
-- received safe-sleep advice while running all night. The complement of a
-- percentile is not the difference of percentiles, so the off-hours signal
-- must be measured directly.
--
-- hcp_sleep_evidence stores per (org, cluster, ns, workload, workload_type,
-- container, day) the p50 of in-window samples and the p50 of off-window
-- samples — exact complements, computed through the canonical weighted
-- digest machinery from the all-hours groups' complete samples. Written
-- only when a schedule is enabled for the namespace (same condition as the
-- business_hours digest stream). Thin by design: two nullable payload
-- columns instead of a third full digest stream — the sleep predicate is
-- the only consumer, and the hot daily_container_digests table (and its
-- unfiltered readers: the zombie rule, the correlate C-leg) is untouched.
--
-- Per-CONTAINER grain, not per-workload: the staged ingestion path flushes
-- in key chunks (a workload's containers can split across chunks), and
-- replacement upserts at container grain keep replayed manifests
-- idempotent — incremental workload sums would double on replay. The
-- predicate aggregates with SUM/COUNT per (day, ns, workload), the same
-- shape the digest query used. A NULL leg means the container had no
-- samples in that window that day (the #761 coverage shape).
--
-- Unpartitioned (the hcp_correlation_advisories precedent): thin rows,
-- scheduled scopes only, bounded by the digest-day range. Pruned
-- symmetrically with business_hours digests when schedules are removed.
CREATE TABLE IF NOT EXISTS hcp_sleep_evidence (
    org_id          TEXT NOT NULL,
    cluster_uuid    UUID NOT NULL,
    namespace       TEXT NOT NULL,
    workload        TEXT NOT NULL,
    workload_type   TEXT NOT NULL,
    container_name  TEXT NOT NULL,
    bucket_date     DATE NOT NULL,
    bh_cpu_p50_mc   BIGINT,
    off_cpu_p50_mc  BIGINT,
    PRIMARY KEY (org_id, cluster_uuid, namespace, workload, workload_type, container_name, bucket_date)
);

CREATE INDEX IF NOT EXISTS idx_hcp_sleep_evidence_window
    ON hcp_sleep_evidence (org_id, cluster_uuid, bucket_date);
