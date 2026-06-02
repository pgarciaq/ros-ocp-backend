INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (74, 'NODE_POD_SCHEDULING_LIMIT', 'WARNING', 'Node is near pod scheduling limit — limited headroom for additional pods')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
