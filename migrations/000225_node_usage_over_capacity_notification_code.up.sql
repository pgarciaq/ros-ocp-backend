INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (88, 'NODE_USAGE_OVER_CAPACITY', 'WARNING', 'Node usage exceeds its allocatable capacity — the reported data is implausible (possible double-counting or misattributed pods upstream); sizing on this node is unreliable until the data is corrected')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
