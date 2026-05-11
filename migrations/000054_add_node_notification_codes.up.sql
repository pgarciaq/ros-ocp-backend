INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (11, 'NODE_UNDERUTILIZED', 'INFO', 'Node CPU and memory utilization both below threshold'),
    (12, 'NODE_OVERCOMMITTED', 'WARNING', 'Pod resource requests exceed node allocatable capacity'),
    (13, 'STRANDED_RESOURCES', 'INFO', 'CPU/memory utilization imbalance suggests wrong instance family')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
