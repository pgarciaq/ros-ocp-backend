-- daily_pvc_digests: daily aggregated PVC usage/capacity metrics.
-- One row per PVC per day per cluster.
CREATE TABLE IF NOT EXISTS daily_pvc_digests (
    id                  BIGSERIAL,
    bucket_date         DATE NOT NULL,
    org_id              TEXT NOT NULL,
    cluster_uuid        UUID NOT NULL,
    namespace           TEXT NOT NULL,
    persistentvolumeclaim TEXT NOT NULL,
    persistentvolume    TEXT NOT NULL DEFAULT '',
    storageclass        TEXT NOT NULL DEFAULT '',
    capacity_bytes      BIGINT NOT NULL DEFAULT 0,
    request_bytes       BIGINT NOT NULL DEFAULT 0,
    usage_bytes_min     BIGINT NOT NULL DEFAULT 0,
    usage_bytes_max     BIGINT NOT NULL DEFAULT 0,
    usage_bytes_avg     BIGINT NOT NULL DEFAULT 0,
    sample_count        INT NOT NULL DEFAULT 0,
    PRIMARY KEY (id, bucket_date)
) PARTITION BY RANGE (bucket_date);

CREATE UNIQUE INDEX IF NOT EXISTS ux_daily_pvc_digests_key
    ON daily_pvc_digests (cluster_uuid, namespace, persistentvolumeclaim, bucket_date);

-- pvc_recommendation_sets: stores PVC right-sizing recommendations.
CREATE TABLE IF NOT EXISTS pvc_recommendation_sets (
    id                  BIGSERIAL PRIMARY KEY,
    org_id              TEXT NOT NULL,
    cluster_uuid        UUID NOT NULL,
    namespace           TEXT NOT NULL,
    persistentvolumeclaim TEXT NOT NULL,
    persistentvolume    TEXT NOT NULL DEFAULT '',
    storageclass        TEXT NOT NULL DEFAULT '',
    capacity_bytes      BIGINT NOT NULL DEFAULT 0,
    usage_bytes_max     BIGINT NOT NULL DEFAULT 0,
    usage_ratio         REAL NOT NULL DEFAULT 0,
    recommendation_type TEXT NOT NULL DEFAULT '',
    recommended_bytes   BIGINT,
    days_to_full        INT,
    growth_bytes_per_day BIGINT,
    notification_codes  SMALLINT[] NOT NULL DEFAULT '{}',
    data_days           INT NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, cluster_uuid, namespace, persistentvolumeclaim)
);
