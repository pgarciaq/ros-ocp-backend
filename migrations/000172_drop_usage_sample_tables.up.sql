-- Phase 2 of #258: drop vestigial raw usage sample tables.
-- Writes were disabled in Phase 1; these tables have no remaining read paths
-- (ADR-0055 superseded by ADR-0292: digest-based percentile-band plots).

DELETE FROM ros_partitioned_parent_registry
 WHERE match_kind = 'exact'
   AND pattern IN ('container_usage_samples', 'namespace_usage_samples');

DROP TABLE IF EXISTS container_usage_samples CASCADE;
DROP TABLE IF EXISTS namespace_usage_samples CASCADE;
