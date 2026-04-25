-- Phase 1: Notification code reference table.
-- Go code uses these codes as constants (internal/engine/notifications.go).
-- API exposes them via GET /recommendations/openshift/notification-codes/.
CREATE TABLE IF NOT EXISTS notification_code_definitions (
    code        SMALLINT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    severity    TEXT NOT NULL DEFAULT 'INFO',
    description TEXT NOT NULL
);

INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (1,  'LOW_CONFIDENCE',              'WARNING',  'Less than 4 days of data available for this workload'),
    (2,  'STALE_DATA',                  'WARNING',  'No new metrics data received for more than 48 hours'),
    (3,  'OOM_DETECTED',                'CRITICAL', 'OOM kill events detected within the analysis window'),
    (4,  'PDB_CAVEAT',                  'WARNING',  'PodDisruptionBudgets affect workloads on this MachineSet — review before scaling'),
    (5,  'IDLE_WORKLOAD',               'INFO',     'Workload uses less than 1% of requested resources'),
    (6,  'RECOMMENDATION_APPLIED',      'INFO',     'Resource change detected matching a previous recommendation'),
    (7,  'NEW_WORKLOAD',                'INFO',     'Less than 24 hours of data — recommendation may be unstable'),
    (8,  'ABANDONED_WORKLOAD',          'WARNING',  'Workload has zero usage for more than 72 hours'),
    (9,  'MEMORY_TRENDING_UP',          'WARNING',  'Memory usage trend suggests capacity risk within 30 days'),
    (10, 'GPU_UNDERUTILIZED',           'INFO',     'GPU utilization below threshold — consider MIG or smaller profile'),
    (11, 'NODE_UNDERUTILIZED',          'INFO',     'Node resources underutilized — consider consolidation'),
    (12, 'NODE_OVERCOMMITTED',          'WARNING',  'Node request overcommit ratio exceeds threshold'),
    (13, 'STRANDED_RESOURCES',          'INFO',     'Imbalanced CPU/memory utilization — consider different instance family'),
    (14, 'AUTOSCALER_SATURATED',        'WARNING',  'MachineAutoscaler at maxReplicas sustained — consider increasing'),
    (15, 'AUTOSCALER_IDLE',             'INFO',     'MachineAutoscaler at minReplicas sustained — consider decreasing'),
    (16, 'AUTOSCALER_FLAPPING',         'WARNING',  'Frequent scale events — widen stabilization window'),
    (17, 'AUTOSCALER_RECOMMENDED',      'INFO',     'MachineSet has variable load but no autoscaler configured'),
    (18, 'VM_IDLE',                     'WARNING',  'Virtual machine has near-zero utilization'),
    (19, 'VM_OVERSIZED',                'INFO',     'Virtual machine allocated resources exceed usage by resize threshold'),
    (20, 'PVC_ORPHANED',               'WARNING',  'PVC has zero usage across all intervals'),
    (21, 'HPA_SATURATED',              'WARNING',  'HPA at maxReplicas sustained — scaling is bottlenecked'),
    (22, 'HPA_ACTIVE',                 'INFO',     'Workload is managed by an HPA — replica count recommendations suppressed'),
    (23, 'INSTANCE_TYPE_NOT_IN_CATALOG','INFO',     'Current instance type is not in the cloud catalog — no resizing needed'),
    (24, 'INSTANCE_TYPE_DEPRECATED',    'INFO',     'Current instance type is deprecated — consider migrating to the recommended type')
ON CONFLICT (code) DO NOTHING;
