INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (25, 'NO_COST_DATA', 'INFO', 'No cost data available — savings estimate not computed')
ON CONFLICT (code) DO NOTHING;
