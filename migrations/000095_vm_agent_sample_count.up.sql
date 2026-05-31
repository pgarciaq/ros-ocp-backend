ALTER TABLE daily_vm_digests
    ADD COLUMN IF NOT EXISTS agent_sample_count INTEGER NOT NULL DEFAULT 0;

INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (44, 'VM_GUEST_AGENT_INTERRUPTED', 'INFO', 'Guest agent data interrupted — recommendations use hypervisor metrics'),
    (45, 'VM_INSUFFICIENT_DATA', 'INFO', 'Insufficient metrics — less than one full day of data available')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
