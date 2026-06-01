ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS is_power_off_candidate BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE vm_recommendations ADD COLUMN IF NOT EXISTS power_off_idle_ratio INTEGER;

INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
  (64, 'VM_POWER_OFF_SCHEDULE', 'INFO', 'VM is idle on many observed days — consider scheduling power-off during inactive periods')
ON CONFLICT (code) DO UPDATE SET
  name = EXCLUDED.name,
  severity = EXCLUDED.severity,
  description = EXCLUDED.description;
