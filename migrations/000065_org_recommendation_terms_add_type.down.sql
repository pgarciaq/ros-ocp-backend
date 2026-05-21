ALTER TABLE org_recommendation_terms DROP CONSTRAINT IF EXISTS org_recommendation_terms_pkey;

ALTER TABLE org_recommendation_terms
    DROP COLUMN IF EXISTS recommendation_type,
    DROP COLUMN IF EXISTS min_data_days;

ALTER TABLE org_recommendation_terms
    ADD PRIMARY KEY (org_id, term_ord);
