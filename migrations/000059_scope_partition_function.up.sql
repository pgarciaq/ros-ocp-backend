CREATE OR REPLACE FUNCTION drop_ros_partition(tableDate TEXT)
RETURNS void AS
$BODY$
DECLARE
    partitionTables TEXT[];
    partitionTable TEXT;
BEGIN
    -- Same logic as 000011; reapplies scoped retention if this migration is reapplied.
    SELECT array_agg(partition_table::TEXT) INTO partitionTables FROM (
        SELECT c.relname AS partition_table,
               matches[1]::date AS min_rangeval,
               matches[2]::date AS max_rangeval
        FROM pg_class c
        INNER JOIN pg_inherits i ON i.inhrelid = c.oid
        INNER JOIN pg_class parent ON parent.oid = i.inhparent
        CROSS JOIN regexp_matches(pg_get_expr(c.relpartbound, c.oid), '\((.+?)\).+\((.+?)\)') AS matches
        WHERE c.relispartition AND c.relkind = 'r'
          AND EXISTS (
              SELECT 1 FROM ros_partitioned_parent_registry r
              WHERE (r.match_kind = 'exact' AND parent.relname = r.pattern)
                 OR (r.match_kind = 'like' AND parent.relname LIKE r.pattern ESCAPE '\')
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
