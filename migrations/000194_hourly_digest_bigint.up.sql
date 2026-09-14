-- Widen hourly digest metric columns to BIGINT. Hourly usage totals are int64
-- sums that can exceed int4 (observed: 5129184234 KiB in mem_usage_p95_kib),
-- aborting the pgx batch and routing the whole Kafka manifest to DLQ.
-- Sibling daily digest tables already use BIGINT.
ALTER TABLE IF EXISTS hourly_node_digests
    ALTER COLUMN cpu_usage_p95_mc TYPE BIGINT,
    ALTER COLUMN mem_usage_p95_kib TYPE BIGINT;

ALTER TABLE IF EXISTS hourly_vm_digests
    ALTER COLUMN cpu_usage_p95_mc TYPE BIGINT,
    ALTER COLUMN mem_usage_p95_kib TYPE BIGINT,
    ALTER COLUMN disk_read_iops_p95 TYPE BIGINT,
    ALTER COLUMN disk_write_iops_p95 TYPE BIGINT;
