-- Down reverses 000194 (hourly digest metric columns back to INTEGER).
-- Only for rollback before any >2GB hourly value is stored; such values
-- would fail to encode after downgrade.
ALTER TABLE IF EXISTS hourly_node_digests
    ALTER COLUMN cpu_usage_p95_mc TYPE INTEGER,
    ALTER COLUMN mem_usage_p95_kib TYPE INTEGER;

ALTER TABLE IF EXISTS hourly_vm_digests
    ALTER COLUMN cpu_usage_p95_mc TYPE INTEGER,
    ALTER COLUMN mem_usage_p95_kib TYPE INTEGER,
    ALTER COLUMN disk_read_iops_p95 TYPE INTEGER,
    ALTER COLUMN disk_write_iops_p95 TYPE INTEGER;
