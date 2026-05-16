-- Restore inline parent filter (pre-registry). Keeps partition drops scoped to ROS tables.
CREATE OR REPLACE FUNCTION drop_ros_partition(tableDate TEXT)
RETURNS void AS
$BODY$
DECLARE
    partitionTables TEXT[];
    partitionTable TEXT;
BEGIN
    SELECT array_agg(partition_table::TEXT) INTO partitionTables FROM (
        SELECT c.relname AS partition_table,
               matches[1]::date AS min_rangeval,
               matches[2]::date AS max_rangeval
        FROM pg_class c
        INNER JOIN pg_inherits i ON i.inhrelid = c.oid
        INNER JOIN pg_class parent ON parent.oid = i.inhparent
        CROSS JOIN regexp_matches(pg_get_expr(c.relpartbound, c.oid), '\((.+?)\).+\((.+?)\)') AS matches
        WHERE c.relispartition AND c.relkind = 'r'
          AND (
            parent.relname IN (
              'daily_container_digests',
              'daily_namespace_digests',
              'daily_pvc_digests',
              'daily_node_digests',
              'container_usage_samples',
              'namespace_usage_samples',
              'gpu_container_digests',
              'recommendation_quality',
              'recommendation_history'
            )
            OR parent.relname LIKE 'workload_metrics\_%' ESCAPE '\'
            OR parent.relname LIKE 'historical_recommendation_sets\_%' ESCAPE '\'
          )
    ) nn WHERE nn.min_rangeval < tableDate::date;

    IF array_length(partitionTables, 1) > 0 THEN
        FOREACH partitionTable IN ARRAY partitionTables
        LOOP
            EXECUTE 'DROP TABLE IF EXISTS '||quote_ident(partitionTable)||';';
        END LOOP;
    END IF;
END;
$BODY$
LANGUAGE plpgsql;

DROP TABLE IF EXISTS ros_partitioned_parent_registry;
