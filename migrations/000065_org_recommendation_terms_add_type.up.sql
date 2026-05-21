-- Add recommendation_type column and min_data_days to org_recommendation_terms.
-- This enables per-plugin term customization. Existing rows are dropped since
-- this feature has never been released.
DELETE FROM org_recommendation_terms;

ALTER TABLE org_recommendation_terms DROP CONSTRAINT IF EXISTS org_recommendation_terms_pkey;

ALTER TABLE org_recommendation_terms
    ADD COLUMN IF NOT EXISTS recommendation_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS min_data_days SMALLINT NOT NULL DEFAULT 1;

ALTER TABLE org_recommendation_terms
    ADD PRIMARY KEY (org_id, recommendation_type, term_ord);
