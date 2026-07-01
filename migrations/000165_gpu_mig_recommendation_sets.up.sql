-- Persisted GPU MIG recommendation sets (one row per container × term).
-- Replaces the per-request enrichment loop in the API handler.
CREATE TABLE IF NOT EXISTS gpu_mig_recommendation_sets (
    id                      BIGSERIAL       NOT NULL,
    org_id                  TEXT            NOT NULL,
    cluster_uuid            UUID            NOT NULL,
    namespace               TEXT            NOT NULL,
    workload                TEXT            NOT NULL,
    workload_type           TEXT            NOT NULL DEFAULT '',
    container_name          TEXT            NOT NULL,
    node_name               TEXT            NOT NULL DEFAULT '',
    gpu_model_name          TEXT            NOT NULL DEFAULT '',
    term                    TEXT            NOT NULL DEFAULT 'short',
    recommended_gpu_profile TEXT            NOT NULL DEFAULT '',
    current_gpu_profile     TEXT            NOT NULL DEFAULT '',
    gpu_classification      TEXT            NOT NULL DEFAULT '',
    confidence              REAL            NOT NULL DEFAULT 0,
    fb_usage_max_mib        REAL            NOT NULL DEFAULT 0,
    total_fb_mib            BIGINT,
    gpu_idle_state          TEXT            NOT NULL DEFAULT 'active',
    gpu_idle_since          DATE,
    gpu_idle_duration_days  INTEGER         NOT NULL DEFAULT 0,
    savings_micro_cents     BIGINT          NOT NULL DEFAULT 0,
    waste_micro_cents       BIGINT          NOT NULL DEFAULT 0,
    category                TEXT            NOT NULL DEFAULT '',
    idle_state              TEXT            NOT NULL DEFAULT '',
    notification_codes      SMALLINT[]      DEFAULT '{}',
    last_reported           TIMESTAMPTZ     NOT NULL DEFAULT now(),
    created_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, cluster_uuid, namespace, workload, container_name, term)
);

CREATE INDEX IF NOT EXISTS idx_gpu_mig_rec_sets_org_cluster
    ON gpu_mig_recommendation_sets (org_id, cluster_uuid);

CREATE INDEX IF NOT EXISTS idx_gpu_mig_rec_sets_org_idle
    ON gpu_mig_recommendation_sets (org_id, gpu_idle_state);

CREATE INDEX IF NOT EXISTS idx_gpu_mig_rec_sets_keyset_cluster
    ON gpu_mig_recommendation_sets (
        org_id,
        cluster_uuid ASC,
        namespace ASC,
        container_name ASC,
        gpu_model_name ASC,
        term ASC
    );

CREATE INDEX IF NOT EXISTS idx_gpu_mig_rec_sets_keyset_namespace
    ON gpu_mig_recommendation_sets (
        org_id,
        namespace ASC,
        cluster_uuid ASC,
        container_name ASC,
        gpu_model_name ASC,
        term ASC
    );
