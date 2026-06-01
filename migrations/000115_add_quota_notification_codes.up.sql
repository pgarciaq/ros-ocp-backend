INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (70, 'QUOTA_NEAR_CAPACITY', 'WARNING', 'Namespace ResourceQuota utilization is at or above the high-risk threshold — consider raising limits'),
    (71, 'QUOTA_OVERSIZED', 'INFO', 'Namespace ResourceQuota hard limits exceed recommended values — capacity can be reclaimed by tightening'),
    (72, 'QUOTA_BLOCKING', 'CRITICAL', 'Namespace ResourceQuota used equals hard limit on one or more resources — new workloads may fail admission'),
    (73, 'CLUSTER_QUOTA_AT_CAPACITY', 'WARNING', 'ClusterResourceQuota utilization is at or above the high-risk threshold')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;

ALTER TABLE quota_recommendation_sets
    ADD COLUMN IF NOT EXISTS notification_codes SMALLINT[] NOT NULL DEFAULT '{}';

ALTER TABLE cluster_quota_recommendation_sets
    ADD COLUMN IF NOT EXISTS notification_codes SMALLINT[] NOT NULL DEFAULT '{}';
