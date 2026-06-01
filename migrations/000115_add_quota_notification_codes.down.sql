ALTER TABLE cluster_quota_recommendation_sets DROP COLUMN IF EXISTS notification_codes;
ALTER TABLE quota_recommendation_sets DROP COLUMN IF EXISTS notification_codes;

DELETE FROM notification_code_definitions WHERE code IN (70, 71, 72, 73);
