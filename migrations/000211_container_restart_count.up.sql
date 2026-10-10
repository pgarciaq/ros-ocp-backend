-- Container crash-loop quality gate: persist per-day container restart sums
-- beside oom_count_sum (created in 000024) for crash-loop detection.
ALTER TABLE daily_container_digests ADD COLUMN IF NOT EXISTS restart_count_sum BIGINT DEFAULT 0;
