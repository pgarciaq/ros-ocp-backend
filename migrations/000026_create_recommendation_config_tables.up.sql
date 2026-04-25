-- Phase 3: Recommendation profiles (replaces Kruize /listPerformanceProfiles).
-- Defines the percentile/margin parameters for cost and performance models.
CREATE TABLE IF NOT EXISTS recommendation_profiles (
    name                TEXT PRIMARY KEY,
    description         TEXT,
    cpu_percentile      DOUBLE PRECISION NOT NULL,
    mem_percentile      DOUBLE PRECISION NOT NULL,
    safety_margin       DOUBLE PRECISION NOT NULL DEFAULT 0.15,
    decay_halflife_hours DOUBLE PRECISION NOT NULL DEFAULT 168,
    is_default          BOOLEAN DEFAULT false,
    created_at          TIMESTAMPTZ DEFAULT now()
);

INSERT INTO recommendation_profiles (name, description, cpu_percentile, mem_percentile, safety_margin, decay_halflife_hours, is_default) VALUES
    ('cost', 'Minimize resource spend, tolerate occasional throttling', 0.60, 0.95, 0.15, 168, true),
    ('performance', 'Minimize throttling and OOM risk, higher spend', 0.98, 1.0, 0.15, 168, true)
ON CONFLICT (name) DO NOTHING;

-- Phase 3: Customer-defined term window overrides (REQ-1.8).
-- Empty for customers using defaults (1d/7d/15d) — Go uses hardcoded DefaultTerms.
-- Only populated when a customer explicitly customizes their term windows.
CREATE TABLE IF NOT EXISTS org_recommendation_terms (
    org_id               TEXT     NOT NULL,
    term_ord             SMALLINT NOT NULL CHECK (term_ord BETWEEN 1 AND 3),
    window_days          SMALLINT NOT NULL CHECK (window_days BETWEEN 1 AND 90),
    decay_halflife_hours REAL,
    PRIMARY KEY (org_id, term_ord)
);
