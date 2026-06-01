ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS is_redundant_placement BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS has_shared_storage BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS numa_oversized BOOLEAN NOT NULL DEFAULT false;

INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
  (60, 'VM_REDUNDANT_COLOCATION', 'WARNING', 'Redundant VMs co-located on same node — consider adding anti-affinity rules'),
  (61, 'VM_UNEVEN_NODE_DISTRIBUTION', 'INFO', 'Uneven VM distribution across nodes — consider topologySpreadConstraints'),
  (62, 'VM_SHARED_STORAGE', 'INFO', 'VM shares storage with other VMs — correlated workload group detected'),
  (63, 'VM_NUMA_OVERSIZED', 'WARNING', 'VM memory exceeds single NUMA node capacity — NUMA pinning not possible')
ON CONFLICT (code) DO NOTHING;
