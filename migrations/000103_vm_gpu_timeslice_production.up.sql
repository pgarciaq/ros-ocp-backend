ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS gpu_timeslice_confidence VARCHAR(16) NOT NULL DEFAULT '';
ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS gpu_timeslice_rationale TEXT NOT NULL DEFAULT '';
ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS recommended_vgpu_profile VARCHAR(64) NOT NULL DEFAULT '';

INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
  (56, 'VM_VGPU_PROFILE_RECOMMENDED', 'INFO', 'vGPU profile recommended — see recommended_vgpu_profile in GPU details'),
  (57, 'VM_GPU_TIMESLICE_UNSAFE_FB', 'WARNING', 'GPU time-slicing not safe — frame-buffer usage too high for shared vGPU')
ON CONFLICT (code) DO NOTHING;
