INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
  (67, 'VM_STORAGE_TIER_COLD', 'INFO', 'VM disk has sustained minimal I/O — consider lower-cost storage tier'),
  (68, 'VM_STORAGE_TIER_IOPS', 'INFO', 'VM disk shows sustained random high IOPS — IOPS-optimized storage recommended'),
  (69, 'VM_STORAGE_TIER_THROUGHPUT', 'INFO', 'VM disk shows sustained sequential high throughput — throughput-optimized storage recommended')
ON CONFLICT (code) DO UPDATE SET
  name = EXCLUDED.name,
  severity = EXCLUDED.severity,
  description = EXCLUDED.description;
