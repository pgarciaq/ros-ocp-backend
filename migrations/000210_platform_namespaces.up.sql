-- #675 cost-groups sync store: admin-curated platform namespaces pushed
-- from koku (replace semantics per org). Read by the zombie idle leg as
-- an augment to the compiled exclusion (either source saying platform
-- excludes; unknown stays user activity). Tiny volume: unpartitioned.
-- plugin: none (store-only)
CREATE TABLE IF NOT EXISTS hcp_platform_namespaces (
    org_id         TEXT NOT NULL,
    namespace      TEXT NOT NULL,
    is_prefix      BOOLEAN NOT NULL DEFAULT FALSE,
    system_default BOOLEAN NOT NULL DEFAULT FALSE,
    fetched_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, namespace)
);
