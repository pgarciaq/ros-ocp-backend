-- Code 15 is NotifNodeIdle (node idle/zombie), not MachineAutoscaler minReplicas.
UPDATE notification_code_definitions SET
    name = 'NODE_IDLE',
    severity = 'INFO',
    description = 'Node has been idle with minimal utilization over the analysis period'
WHERE code = 15;

-- Reserved for future Tier 3 MachineAutoscaler minReplicas recommendations.
INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (75, 'AUTOSCALER_MIN_REPLICAS_RESERVED', 'INFO', 'MachineAutoscaler at minReplicas sustained — consider decreasing')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
