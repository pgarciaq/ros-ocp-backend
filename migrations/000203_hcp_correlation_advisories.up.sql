-- #646 thin correlator advisories (implements #625): batch-written,
-- fires-only rows. Silence (no H/C/N proof, stale, skewed, unknown) is the
-- absence of a row, never a stored negative. Tiny volume: unpartitioned with
-- an expiry index; each hourly run deletes expired rows (self-cleaning, no
-- housekeeper sweep needed).
CREATE TABLE IF NOT EXISTS hcp_correlation_advisories (
    org_id                  TEXT NOT NULL,
    hc_cluster_id           TEXT NOT NULL,
    management_cluster_uuid UUID NOT NULL,
    window_start            TIMESTAMPTZ NOT NULL,
    window_end              TIMESTAMPTZ NOT NULL,
    verdict                 TEXT NOT NULL DEFAULT 'do_not_add_workers_first',
    confidence              TEXT NOT NULL DEFAULT 'high',
    h_p99_s                 REAL,
    h_threshold_s           REAL,
    c_ratio                 REAL,
    n_signals               TEXT[] NOT NULL DEFAULT '{}',
    expires_at              TIMESTAMPTZ NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, hc_cluster_id, window_start)
);

CREATE INDEX IF NOT EXISTS idx_hcp_correlation_advisories_expiry
    ON hcp_correlation_advisories (expires_at);
