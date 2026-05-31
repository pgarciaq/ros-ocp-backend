DELETE FROM notification_code_definitions WHERE code = 54;

ALTER TABLE vm_recommendations DROP COLUMN IF EXISTS recommended_time_slice_count;
ALTER TABLE daily_vm_digests DROP COLUMN IF EXISTS gpu_devices;
