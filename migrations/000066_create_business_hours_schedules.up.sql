-- Per-org/cluster/namespace business hours schedules for dual digest aggregation.
-- Sentinel values: all-zero UUID = org default; empty namespace = cluster/org scope.
CREATE TABLE business_hours_schedules (
    org_id               TEXT NOT NULL,
    cluster_uuid         UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    namespace            TEXT NOT NULL DEFAULT '',
    timezone             TEXT NOT NULL,
    days                 TEXT[] NOT NULL,
    start_time           TIME NOT NULL,
    end_time             TIME NOT NULL,
    off_hours_weight     REAL NOT NULL DEFAULT 0.0,
    enabled              BOOLEAN NOT NULL DEFAULT true,
    reship_pending_since TIMESTAMPTZ,       -- set when masu reship is pending; cleared on success
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, cluster_uuid, namespace)
);

COMMENT ON COLUMN business_hours_schedules.reship_pending_since IS
    'Timestamp when a masu reship_ros call failed; background poller retries until cleared';

COMMENT ON COLUMN business_hours_schedules.cluster_uuid IS
    'Cluster scope; all-zero UUID sentinel denotes org-wide default';

COMMENT ON COLUMN business_hours_schedules.namespace IS
    'Namespace scope; empty string sentinel denotes org or cluster default';

CREATE INDEX idx_bh_schedules_org ON business_hours_schedules (org_id);
CREATE INDEX idx_bh_schedules_org_cluster ON business_hours_schedules (org_id, cluster_uuid);
