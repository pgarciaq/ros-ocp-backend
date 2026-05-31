ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS gpu_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS gpu_model TEXT NOT NULL DEFAULT '';
ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS gpu_util_avg_bp INTEGER NOT NULL DEFAULT 0;
ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS gpu_util_max_bp INTEGER NOT NULL DEFAULT 0;
ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS gpu_fb_used_avg_mib DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS gpu_fb_used_max_mib DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS gpu_sm_active_avg_bp INTEGER NOT NULL DEFAULT 0;
ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS gpu_tensor_avg_bp INTEGER NOT NULL DEFAULT 0;
ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS gpu_dram_avg_bp INTEGER NOT NULL DEFAULT 0;
ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS gpu_mig_profile TEXT NOT NULL DEFAULT '';
ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS gpu_max_slices INTEGER NOT NULL DEFAULT 0;
ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS has_gpu BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS gpu_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS gpu_model TEXT NOT NULL DEFAULT '';
ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS gpu_classification TEXT NOT NULL DEFAULT '';
ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS recommended_gpu_action TEXT NOT NULL DEFAULT '';
ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS recommended_gpu_profile TEXT NOT NULL DEFAULT '';
ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS gpu_utilization_avg_bp INTEGER NOT NULL DEFAULT 0;

INSERT INTO notification_code_definitions (code, description) VALUES
  (50, 'GPU is idle — consider removing GPU passthrough/vGPU assignment'),
  (51, 'GPU underutilized — consider a smaller vGPU profile or MIG partition'),
  (52, 'GPU memory saturated — consider a larger GPU or additional GPU'),
  (53, 'GPU compute saturated — workload may benefit from a more powerful GPU')
ON CONFLICT (code) DO NOTHING;
