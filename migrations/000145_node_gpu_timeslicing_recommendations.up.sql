CREATE TABLE IF NOT EXISTS node_gpu_timeslicing_recommendations (
    org_id                    TEXT NOT NULL,
    cluster_uuid              UUID NOT NULL,
    node_name                 TEXT NOT NULL,
    gpu_model                 TEXT NOT NULL DEFAULT '',
    term                      TEXT NOT NULL,

    recommended_replicas      INTEGER NOT NULL,
    confidence                REAL NOT NULL DEFAULT 0,
    confidence_level          REAL NOT NULL DEFAULT 0,

    candidate_count           INTEGER NOT NULL DEFAULT 0,
    impacted_count            INTEGER NOT NULL DEFAULT 0,

    candidate_containers      JSONB NOT NULL DEFAULT '[]',
    impacted_containers       JSONB NOT NULL DEFAULT '[]',

    notification_codes        SMALLINT[] NOT NULL DEFAULT '{}',

    estimated_savings_cents   BIGINT,
    savings_per_gpu_cents     BIGINT,

    last_seen_at              TIMESTAMPTZ,

    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (org_id, cluster_uuid, node_name, gpu_model, term)
);

CREATE INDEX IF NOT EXISTS idx_node_gpu_ts_org_cluster
    ON node_gpu_timeslicing_recommendations (org_id, cluster_uuid);

CREATE INDEX IF NOT EXISTS idx_node_gpu_ts_list_sort
    ON node_gpu_timeslicing_recommendations (org_id, cluster_uuid, term, recommended_replicas);

CREATE TABLE IF NOT EXISTS node_gpu_timeslicing_recommendation_history (
    id                      BIGSERIAL PRIMARY KEY,
    org_id                  TEXT NOT NULL,
    cluster_uuid            UUID NOT NULL,
    node_name               TEXT NOT NULL,
    gpu_model               TEXT NOT NULL,
    term                    TEXT NOT NULL,
    recommended_replicas    INTEGER NOT NULL,
    confidence              REAL NOT NULL,
    candidate_count         INTEGER NOT NULL,
    impacted_count          INTEGER NOT NULL,
    estimated_savings_cents BIGINT,
    recorded_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_node_gpu_ts_hist_lookup
    ON node_gpu_timeslicing_recommendation_history (org_id, cluster_uuid, node_name, gpu_model, term, recorded_at DESC);

ALTER TABLE recommendation_sets
    ADD COLUMN IF NOT EXISTS time_slicing_node TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS time_slicing_replicas INTEGER NOT NULL DEFAULT 0;
