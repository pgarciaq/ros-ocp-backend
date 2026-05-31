DELETE FROM notification_code_definitions WHERE code IN (50, 51, 52, 53);

ALTER TABLE vm_recommendations
  DROP COLUMN IF EXISTS gpu_utilization_avg_bp,
  DROP COLUMN IF EXISTS recommended_gpu_profile,
  DROP COLUMN IF EXISTS recommended_gpu_action,
  DROP COLUMN IF EXISTS gpu_classification,
  DROP COLUMN IF EXISTS gpu_model,
  DROP COLUMN IF EXISTS gpu_count;

ALTER TABLE daily_vm_digests
  DROP COLUMN IF EXISTS has_gpu,
  DROP COLUMN IF EXISTS gpu_max_slices,
  DROP COLUMN IF EXISTS gpu_mig_profile,
  DROP COLUMN IF EXISTS gpu_dram_avg_bp,
  DROP COLUMN IF EXISTS gpu_tensor_avg_bp,
  DROP COLUMN IF EXISTS gpu_sm_active_avg_bp,
  DROP COLUMN IF EXISTS gpu_fb_used_max_mib,
  DROP COLUMN IF EXISTS gpu_fb_used_avg_mib,
  DROP COLUMN IF EXISTS gpu_util_max_bp,
  DROP COLUMN IF EXISTS gpu_util_avg_bp,
  DROP COLUMN IF EXISTS gpu_model,
  DROP COLUMN IF EXISTS gpu_count;
