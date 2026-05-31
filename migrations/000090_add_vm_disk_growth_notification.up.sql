INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (37, 'VM_DISK_GROWING_NO_CAPACITY', 'WARNING', 'Virtual machine disk allocation is growing but guest-agent capacity data is unavailable')
ON CONFLICT (code) DO NOTHING;
