DELETE FROM notification_code_definitions WHERE code = 64;

ALTER TABLE vm_recommendations DROP COLUMN IF EXISTS power_off_idle_ratio;
ALTER TABLE vm_recommendations DROP COLUMN IF EXISTS is_power_off_candidate;
