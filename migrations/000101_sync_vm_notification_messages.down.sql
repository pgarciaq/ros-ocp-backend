UPDATE notification_code_definitions SET
    severity = 'INFO',
    description = 'QEMU guest agent not installed — recommendations use hypervisor metrics only'
WHERE code = 38;

UPDATE notification_code_definitions SET
    severity = 'WARNING',
    description = 'High disk I/O detected on virtual machine'
WHERE code = 39;

UPDATE notification_code_definitions SET
    severity = 'WARNING',
    description = 'Guest filesystem usage growing toward capacity'
WHERE code = 40;

UPDATE notification_code_definitions SET
    severity = 'INFO',
    description = 'Recommended cloud instance type for virtual machine sizing'
WHERE code = 41;

UPDATE notification_code_definitions SET
    severity = 'CRITICAL',
    description = 'Guest filesystem critically full'
WHERE code = 42;
