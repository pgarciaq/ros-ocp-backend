-- #874: a staleness clock separate from the report clock.
--
-- `updated_at` is the report time: the container list projects it as
-- `last_reported` (internal/model/common.go:49). The only writer that marks a
-- row stale (MarkUnreportedContainersStale) bumped it, so a container that
-- merely missed a cycle claimed to have reported just now, and a cluster-wide
-- sweep reusing that writer would make a dead cluster look alive.
--
-- `staled_at` records when a row was MARKED stale. Retention deletes on it, so
-- the visible stale window is a function of ROS_STALE_CLEANUP_DAYS alone and
-- cannot be collapsed by a long liveness window. It is cleared whenever a row
-- reports again (both upserts set stale = EXCLUDED.stale).
--
-- No backfill: the purge reads COALESCE(staled_at, updated_at), so rows that
-- were already stale keep the deadline they have today. A backfill statement
-- could not be exercised by the shared test harness, which migrates an empty
-- database, and would be the only untested code in the change.
ALTER TABLE recommendation_sets ADD COLUMN IF NOT EXISTS staled_at TIMESTAMPTZ;

ALTER TABLE namespace_recommendation_sets ADD COLUMN IF NOT EXISTS staled_at TIMESTAMPTZ;
