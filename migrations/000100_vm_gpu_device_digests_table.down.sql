ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS gpu_devices JSONB NOT NULL DEFAULT '[]'::jsonb;

DROP TABLE IF EXISTS vm_gpu_device_digests;
