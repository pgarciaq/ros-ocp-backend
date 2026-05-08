INSERT INTO notification_code_definitions (code, severity, message) VALUES
    (29, 'INFO', 'PVC capacity significantly exceeds sustained usage — consider shrinking'),
    (30, 'WARNING', 'PVC usage approaching capacity — consider expanding or investigate growth')
ON CONFLICT (code) DO UPDATE SET
    severity = EXCLUDED.severity,
    message = EXCLUDED.message;
