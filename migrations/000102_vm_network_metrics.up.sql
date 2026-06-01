ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS net_throughput_p95_bps BIGINT NOT NULL DEFAULT 0;
ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS net_pps_p95 BIGINT NOT NULL DEFAULT 0;
ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS net_drop_ratio_max_bp INT NOT NULL DEFAULT 0;

ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS is_network_bound BOOLEAN NOT NULL DEFAULT FALSE;

INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
  (55, 'VM_NETWORK_SATURATED', 'WARNING', 'Network-saturated workload: recommend n1 network-optimized instance type')
ON CONFLICT (code) DO NOTHING;
