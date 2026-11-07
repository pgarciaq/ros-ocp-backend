-- #763 rollback: restore the 3-column key. Multi-farm rows (same org, hc,
-- window, different management clusters) must collapse back to one row per
-- old key first — keep the row with the greatest (expires_at,
-- management_cluster_uuid) — so re-adding the narrower key cannot fail on
-- duplicates.
DELETE FROM hcp_correlation_advisories a
    USING hcp_correlation_advisories b
    WHERE a.org_id = b.org_id
      AND a.hc_cluster_id = b.hc_cluster_id
      AND a.window_start = b.window_start
      AND a.management_cluster_uuid <> b.management_cluster_uuid
      AND (a.expires_at, a.management_cluster_uuid) < (b.expires_at, b.management_cluster_uuid);
ALTER TABLE hcp_correlation_advisories
    DROP CONSTRAINT hcp_correlation_advisories_pkey;
ALTER TABLE hcp_correlation_advisories
    ADD PRIMARY KEY (org_id, hc_cluster_id, window_start);
