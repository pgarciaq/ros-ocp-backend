-- snapshot_inventory: raw inventory ingested from operator CSV.
-- One row per snapshot per ingestion cycle. Retention: 48 hours.
CREATE TABLE IF NOT EXISTS snapshot_inventory (
    id                      BIGSERIAL PRIMARY KEY,
    ingested_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    org_id                  TEXT NOT NULL,
    cluster_uuid            UUID NOT NULL,
    namespace               TEXT NOT NULL,
    snapshot_name           TEXT NOT NULL,
    source_pvc_name         TEXT NOT NULL DEFAULT '',
    volume_snapshot_class   TEXT NOT NULL DEFAULT '',
    storageclass            TEXT NOT NULL DEFAULT '',
    creation_timestamp      TIMESTAMPTZ NOT NULL,
    ready_to_use            BOOLEAN NOT NULL DEFAULT false,
    restore_size_bytes      BIGINT NOT NULL DEFAULT 0,
    source_pvc_exists       BOOLEAN NOT NULL DEFAULT true,
    restored_pvc_count      INT NOT NULL DEFAULT 0,
    labels                  JSONB NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_snapshot_inventory_lookup
    ON snapshot_inventory (org_id, cluster_uuid, namespace, snapshot_name);
CREATE INDEX IF NOT EXISTS idx_snapshot_inventory_ingested
    ON snapshot_inventory (ingested_at);

-- snapshot_recommendation_sets: classified snapshot recommendations.
-- One row per snapshot per cluster. Updated via UPSERT on each ingestion.
CREATE TABLE IF NOT EXISTS snapshot_recommendation_sets (
    id                      BIGSERIAL PRIMARY KEY,
    org_id                  TEXT NOT NULL,
    cluster_uuid            UUID NOT NULL,
    namespace               TEXT NOT NULL,
    snapshot_name           TEXT NOT NULL,
    source_pvc_name         TEXT NOT NULL DEFAULT '',
    volume_snapshot_class   TEXT NOT NULL DEFAULT '',
    storageclass            TEXT NOT NULL DEFAULT '',
    creation_timestamp      TIMESTAMPTZ NOT NULL,
    restore_size_bytes      BIGINT NOT NULL DEFAULT 0,
    age_days                INT NOT NULL DEFAULT 0,
    source_pvc_exists       BOOLEAN NOT NULL DEFAULT true,
    restored_pvc_count      INT NOT NULL DEFAULT 0,
    managed_by              TEXT NOT NULL DEFAULT '',
    recommendation_type     TEXT NOT NULL DEFAULT '',
    estimated_monthly_cost_usd REAL,
    notification_codes      SMALLINT[] NOT NULL DEFAULT '{}',
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, cluster_uuid, namespace, snapshot_name)
);

-- snapshot_settings: per-org user-configurable thresholds and cost rate.
CREATE TABLE IF NOT EXISTS snapshot_settings (
    org_id                  TEXT PRIMARY KEY,
    orphan_age_days         INT NOT NULL DEFAULT 7,
    never_restored_days     INT NOT NULL DEFAULT 30,
    stale_days              INT NOT NULL DEFAULT 90,
    redundant_threshold     INT NOT NULL DEFAULT 3,
    cost_per_gib_month_usd  REAL NOT NULL DEFAULT 0.05,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
