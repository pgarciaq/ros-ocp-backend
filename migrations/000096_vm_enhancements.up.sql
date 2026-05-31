-- Restart count for crash loop detection (P99 columns already exist on daily_vm_digests).
ALTER TABLE daily_vm_digests ADD COLUMN IF NOT EXISTS restart_count_sum INTEGER NOT NULL DEFAULT 0;

INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (46, 'VM_UNKNOWN_OS', 'INFO', 'Guest OS not detected — using Linux defaults. Install qemu-guest-agent for OS-specific thresholds.'),
    (47, 'VM_WINDOWS_UPDATE_SPIKE', 'INFO', 'Periodic usage spikes detected (possibly OS updates); P95 sizing accounts for this'),
    (48, 'VM_CRASH_LOOP', 'WARNING', 'VM restarted multiple times in the observation window — possible instability or crash loop'),
    (49, 'VM_DOWNSIZE_HELD', 'INFO', 'Downsize recommendation suppressed: usage not consistently below threshold')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
