UPDATE notification_code_definitions SET
    name = 'AUTOSCALER_IDLE',
    severity = 'INFO',
    description = 'MachineAutoscaler at minReplicas sustained — consider decreasing'
WHERE code = 15;

DELETE FROM notification_code_definitions WHERE code = 75;
