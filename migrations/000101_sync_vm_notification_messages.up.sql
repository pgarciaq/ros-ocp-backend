-- Align VM notification descriptions with internal/notifications.Definitions (API/Kruize mapping).
UPDATE notification_code_definitions SET
    severity = 'INFO',
    description = 'QEMU guest agent not installed: recommendations based on hypervisor metrics only (moderate confidence)'
WHERE code = 38;

UPDATE notification_code_definitions SET
    severity = 'WARNING',
    description = 'High disk I/O detected: consider storage-optimized instance type or faster storage class'
WHERE code = 39;

UPDATE notification_code_definitions SET
    severity = 'WARNING',
    description = 'Filesystem usage growing toward capacity at current growth rate'
WHERE code = 40;

UPDATE notification_code_definitions SET
    severity = 'INFO',
    description = 'Recommended instance type available for virtual machine sizing'
WHERE code = 41;

UPDATE notification_code_definitions SET
    severity = 'CRITICAL',
    description = 'Filesystem critically full: immediate expansion recommended'
WHERE code = 42;
