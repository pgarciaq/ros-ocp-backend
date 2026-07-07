-- ARV-11: Relax autovacuum settings on INSERT-only quality tables.
--
-- Migration 000168 set autovacuum_vacuum_scale_factor=0.05 and
-- autovacuum_analyze_scale_factor=0.02 on quality partitions. These tables are
-- INSERT-only (no UPDATEs/DELETEs), so vacuum finds no dead tuples — wasted I/O.
-- ANALYZE at 0.02 fires after every reconcile cycle (~1000 inserts on 50k rows).
--
-- Fix: reset vacuum to default (remove the setting) and raise analyze to 0.05.
-- PostgreSQL 13+ handles freeze duty via autovacuum_vacuum_insert_scale_factor
-- (default 0.20), which is appropriate for INSERT-only tables.

-- recommendation_quality
DO $$
DECLARE
    part regclass;
BEGIN
    FOR part IN
        SELECT inhrelid::regclass
        FROM pg_inherits
        WHERE inhparent = 'recommendation_quality'::regclass
    LOOP
        EXECUTE format(
            'ALTER TABLE %s RESET (autovacuum_vacuum_scale_factor)',
            part
        );
        EXECUTE format(
            'ALTER TABLE %s SET (autovacuum_analyze_scale_factor = 0.05)',
            part
        );
    END LOOP;
END $$;

-- pvc_recommendation_quality
DO $$
DECLARE
    part regclass;
BEGIN
    FOR part IN
        SELECT inhrelid::regclass
        FROM pg_inherits
        WHERE inhparent = 'pvc_recommendation_quality'::regclass
    LOOP
        EXECUTE format(
            'ALTER TABLE %s RESET (autovacuum_vacuum_scale_factor)',
            part
        );
        EXECUTE format(
            'ALTER TABLE %s SET (autovacuum_analyze_scale_factor = 0.05)',
            part
        );
    END LOOP;
END $$;

-- vm_recommendation_quality
DO $$
DECLARE
    part regclass;
BEGIN
    FOR part IN
        SELECT inhrelid::regclass
        FROM pg_inherits
        WHERE inhparent = 'vm_recommendation_quality'::regclass
    LOOP
        EXECUTE format(
            'ALTER TABLE %s RESET (autovacuum_vacuum_scale_factor)',
            part
        );
        EXECUTE format(
            'ALTER TABLE %s SET (autovacuum_analyze_scale_factor = 0.05)',
            part
        );
    END LOOP;
END $$;

-- gpu_mig_recommendation_quality
DO $$
DECLARE
    part regclass;
BEGIN
    FOR part IN
        SELECT inhrelid::regclass
        FROM pg_inherits
        WHERE inhparent = 'gpu_mig_recommendation_quality'::regclass
    LOOP
        EXECUTE format(
            'ALTER TABLE %s RESET (autovacuum_vacuum_scale_factor)',
            part
        );
        EXECUTE format(
            'ALTER TABLE %s SET (autovacuum_analyze_scale_factor = 0.05)',
            part
        );
    END LOOP;
END $$;

-- snapshot_recommendation_quality
DO $$
DECLARE
    part regclass;
BEGIN
    FOR part IN
        SELECT inhrelid::regclass
        FROM pg_inherits
        WHERE inhparent = 'snapshot_recommendation_quality'::regclass
    LOOP
        EXECUTE format(
            'ALTER TABLE %s RESET (autovacuum_vacuum_scale_factor)',
            part
        );
        EXECUTE format(
            'ALTER TABLE %s SET (autovacuum_analyze_scale_factor = 0.05)',
            part
        );
    END LOOP;
END $$;
