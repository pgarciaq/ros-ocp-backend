-- Snapshot recommendation quality metrics.
CREATE TABLE IF NOT EXISTS snapshot_recommendation_quality (
    measured_at              TIMESTAMPTZ NOT NULL,
    org_id                   TEXT NOT NULL,
    cluster_uuid             UUID NOT NULL,
    snapshot_name            TEXT NOT NULL,
    adoption_detected        BOOLEAN DEFAULT false,
    recommendation_age_hours BIGINT,
    PRIMARY KEY (org_id, cluster_uuid, snapshot_name, measured_at)
) PARTITION BY RANGE (measured_at);

DO $$
DECLARE
    month_start DATE;
    month_end   DATE;
    part_name   TEXT;
BEGIN
    FOR i IN 0..2 LOOP
        month_start := date_trunc('month', CURRENT_DATE) + (i || ' months')::interval;
        month_end   := month_start + '1 month'::interval;
        part_name   := 'snapshot_recommendation_quality_' || to_char(month_start, 'YYYYMM');
        IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF snapshot_recommendation_quality FOR VALUES FROM (%L) TO (%L)',
                part_name, month_start, month_end
            );
        END IF;
    END LOOP;
END $$;

CREATE INDEX IF NOT EXISTS idx_snapshot_quality_org_measured
    ON snapshot_recommendation_quality (org_id, measured_at DESC);
