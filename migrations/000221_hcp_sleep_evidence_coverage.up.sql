-- #827 (V16-03): sleep evidence gains per-leg observation coverage.
-- A leg was "measured" as soon as ANY sample existed: two observed hours
-- per day (28 observations across a 14-day window) produced high-confidence
-- sleep advice over 22 unobserved hours per day. The predicate now requires
-- the leg's DISTINCT observed hours to meet a floor fraction of the window
-- (the H-leg minimum-observation precedent, HMinDataMinutes) — unknown on
-- shortfall, never zero-activity.
--
-- Per container-day, per leg: the count of distinct UTC hour buckets with
-- at least one sample. The workload-day aggregate takes MIN over containers
-- (a fully-observed sibling must not mask an under-observed one — the
-- #826 partial-migration lesson), gated by the same COUNT = COUNT(*)
-- completeness rule.
--
-- NULL = unknown (cold-start round 3, the accepted trade): pre-#827 rows
-- read coverage-unmeasured; the p95 rule of #826 already makes those rows
-- off-unmeasured, so the day reads unknown either way — no behavioral
-- conflict between the two migrations.
ALTER TABLE hcp_sleep_evidence ADD COLUMN bh_observed_hours SMALLINT;
ALTER TABLE hcp_sleep_evidence ADD COLUMN off_observed_hours SMALLINT;
