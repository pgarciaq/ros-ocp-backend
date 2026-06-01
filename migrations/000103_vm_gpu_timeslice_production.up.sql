ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS gpu_timeslice_confidence VARCHAR(16) NOT NULL DEFAULT '';
ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS gpu_timeslice_rationale TEXT NOT NULL DEFAULT '';
ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS recommended_vgpu_profile VARCHAR(64) NOT NULL DEFAULT '';
