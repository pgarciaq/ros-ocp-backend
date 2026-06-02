ALTER TABLE pvc_recommendation_sets
    DROP COLUMN IF EXISTS vm_name;

ALTER TABLE daily_pvc_digests
    DROP COLUMN IF EXISTS vm_name;
