ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS gpu_devices JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS recommended_time_slice_count INTEGER NOT NULL DEFAULT 0;

INSERT INTO notification_code_definitions (code, name, severity, description)
VALUES (54, 'VM_GPU_MIXED_IDLE', 'WARNING', 'One or more GPUs are idle while others are active')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
