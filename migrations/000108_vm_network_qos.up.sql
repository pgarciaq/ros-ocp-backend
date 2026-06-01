INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
  (65, 'VM_NETWORK_QOS_SRIOV', 'INFO', 'Network-bound VM with high throughput or packet drops — SR-IOV may improve performance'),
  (66, 'VM_NETWORK_QOS_DPDK', 'INFO', 'Network-bound VM with high PPS and small packets — DPDK userspace networking may reduce latency')
ON CONFLICT (code) DO UPDATE SET
  name = EXCLUDED.name,
  severity = EXCLUDED.severity,
  description = EXCLUDED.description;
