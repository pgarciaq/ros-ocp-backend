CREATE TABLE IF NOT EXISTS report_file_status (
    id              SERIAL PRIMARY KEY,
    manifest_id     TEXT NOT NULL,
    cluster_id      TEXT NOT NULL,
    org_id          TEXT NOT NULL,
    filename        TEXT NOT NULL,
    report_type     TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    error_message   TEXT,
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT report_file_status_manifest_filename_unique UNIQUE (manifest_id, filename),
    CONSTRAINT report_file_status_status_check CHECK (
        status IN ('pending', 'processing', 'done', 'failed')
    )
);

CREATE INDEX IF NOT EXISTS idx_report_file_status_manifest_status
    ON report_file_status (manifest_id, status);
