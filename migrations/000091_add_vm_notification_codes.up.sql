UPDATE notification_code_definitions SET
    severity = 'WARNING',
    description = 'VM is idle: CPU and memory usage are consistently below thresholds'
WHERE code = 18;

UPDATE notification_code_definitions SET
    severity = 'WARNING',
    description = 'VM is oversized: recommended resources are significantly below current allocation'
WHERE code = 19;

INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (38, 'VM_NO_GUEST_AGENT', 'INFO', 'QEMU guest agent not installed — recommendations use hypervisor metrics only'),
    (39, 'VM_HIGH_IO', 'WARNING', 'High disk I/O detected on virtual machine'),
    (40, 'VM_DISK_FILLING', 'WARNING', 'Guest filesystem usage growing toward capacity'),
    (41, 'VM_INSTANCE_TYPE', 'INFO', 'Recommended cloud instance type for virtual machine sizing'),
    (42, 'VM_DISK_CRITICAL', 'CRITICAL', 'Guest filesystem critically full')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
