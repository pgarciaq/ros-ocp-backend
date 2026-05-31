DELETE FROM notification_code_definitions WHERE code IN (44, 45);

ALTER TABLE daily_vm_digests DROP COLUMN IF EXISTS agent_sample_count;
