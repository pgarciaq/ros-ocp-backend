-- W8 non-prod sleep (#715/#716, ADR-0348): the HostedCluster's
-- insights.cost-management/hc-tier label value rides the topology
-- snapshots. Empty string = no declaration (operator emits '' when the
-- label is absent); default '' keeps pre-#715 reports valid.
ALTER TABLE manifest_hcp_snapshots ADD COLUMN IF NOT EXISTS hc_tier TEXT NOT NULL DEFAULT '';
