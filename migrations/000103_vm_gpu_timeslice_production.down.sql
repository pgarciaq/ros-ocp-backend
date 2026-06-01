ALTER TABLE vm_recommendations DROP COLUMN IF EXISTS recommended_vgpu_profile;
ALTER TABLE vm_recommendations DROP COLUMN IF EXISTS gpu_timeslice_rationale;
ALTER TABLE vm_recommendations DROP COLUMN IF EXISTS gpu_timeslice_confidence;
