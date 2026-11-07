-- #763 (V15-13): the advisories key must distinguish management clusters
-- (farms). api-tax rows use hc_cluster_id='' (shared-plane evidence names no
-- single HC); with the 3-column key, two farms in one org/window hit the
-- same row and the DO UPDATE replaced the evidence while keeping the first
-- farm's identity — one farm's UUID served with another farm's webhook
-- facts, visible to the wrong farm's #791 scope grant. Extending the key
-- with management_cluster_uuid gives each farm its own row. For per-HC rows
-- the management cluster is functionally determined by the HC, so
-- ADR-0349 §3's at-most-one-advisory-per-HC invariant holds semantically as
-- before. Tiny unpartitioned self-cleaning table — the swap is cheap.
--
-- Pre-fix shared-plane rows may carry one farm's identity with another
-- farm's evidence (V15-13's mixing); that is undetectable post-hoc — the
-- evidence records no originating farm. The table is fires-only and
-- hourly-refreshed, so purge the shared-plane rows rather than serve a
-- possibly-mixed row to expiry: farms with live evidence re-fire within
-- the hour. Per-HC rows are kept (their identity and evidence come from
-- the same per-HC evaluation).
DELETE FROM hcp_correlation_advisories WHERE hc_cluster_id = '';
ALTER TABLE hcp_correlation_advisories
    DROP CONSTRAINT hcp_correlation_advisories_pkey;
ALTER TABLE hcp_correlation_advisories
    ADD PRIMARY KEY (org_id, hc_cluster_id, window_start, management_cluster_uuid);
