-- #407 W0.2: persist cluster topology classification for HCP/fleet
-- recommendations on the clusters row.
--
-- cluster_topology vocabulary is closed by CHECK to the ADR-0328 classes:
-- dedicated (standalone), hosted (control plane elsewhere), management
-- (hosts control planes for others), unknown (facts absent, e.g. pre-#406
-- payloads). Existing rows backfill to unknown; reprocessed payloads refresh
-- the value on conflict update (see pgrec.UpdateClusterTopology).
-- clusters is small. Plain ALTER is fine (see migrations/README.md
-- large-table policy); golang-migrate wraps this file in one transaction.
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS cluster_topology TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE clusters ADD CONSTRAINT chk_clusters_topology CHECK (cluster_topology IN ('dedicated','hosted','management','unknown'));
