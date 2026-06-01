ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS io_pattern VARCHAR(16) NOT NULL DEFAULT '';

INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
  (58, 'VM_IO_SEQUENTIAL', 'INFO', 'Sequential disk I/O pattern detected — consider storage optimized for throughput'),
  (59, 'VM_IO_RANDOM', 'INFO', 'Random disk I/O pattern detected — consider storage optimized for IOPS')
ON CONFLICT (code) DO NOTHING;
