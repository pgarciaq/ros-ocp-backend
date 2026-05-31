DROP INDEX IF EXISTS idx_vm_recommendations_abandoned;
ALTER TABLE vm_recommendations DROP COLUMN IF EXISTS is_abandoned;

DELETE FROM notification_code_definitions WHERE code = 43;
