INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (11, 'NODE_UNDERUTILIZED', 'INFO', 'Node resources underutilized — consider consolidation'),
    (12, 'NODE_OVERCOMMITTED', 'WARNING', 'Node request overcommit ratio exceeds threshold'),
    (13, 'STRANDED_RESOURCES', 'INFO', 'Imbalanced CPU/memory utilization — consider different instance family')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
