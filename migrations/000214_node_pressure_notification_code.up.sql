INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (87, 'NODE_PRESSURE', 'WARNING', 'Node pressure conditions observed in window — consolidation withheld, usage baselines untrustworthy')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
