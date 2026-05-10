INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (36, 'GPU_TIMESLICING_CANDIDATE', 'INFO', 'GPU time-slicing candidate — workload may benefit from shared GPU scheduling')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
