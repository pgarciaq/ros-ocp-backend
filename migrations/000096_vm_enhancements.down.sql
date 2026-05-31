ALTER TABLE daily_vm_digests DROP COLUMN IF EXISTS restart_count_sum;
DELETE FROM notification_code_definitions WHERE code IN (46, 47, 48, 49);
