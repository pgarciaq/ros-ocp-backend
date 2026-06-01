DELETE FROM notification_code_definitions WHERE code IN (58, 59);

ALTER TABLE vm_recommendations DROP COLUMN IF EXISTS io_pattern;
