DELETE FROM notification_code_definitions WHERE code = 55;

ALTER TABLE vm_recommendations DROP COLUMN IF EXISTS is_network_bound;

ALTER TABLE daily_vm_digests DROP COLUMN IF EXISTS net_drop_ratio_max_bp;
ALTER TABLE daily_vm_digests DROP COLUMN IF EXISTS net_pps_p95;
ALTER TABLE daily_vm_digests DROP COLUMN IF EXISTS net_throughput_p95_bps;
