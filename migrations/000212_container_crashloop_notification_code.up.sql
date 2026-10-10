INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (86, 'UNSTABLE_CONTAINER', 'WARNING', 'Container restarted repeatedly in the observation window — possible crash loop; downsize withheld')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
