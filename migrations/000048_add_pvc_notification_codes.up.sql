INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (26, 'GPU_IDLE', 'INFO', 'GPU utilization below idle threshold — consider removing GPU request'),
    (27, 'GPU_MEMORY_BOUND', 'INFO', 'GPU memory-bound — consider MIG profile with more HBM'),
    (28, 'GPU_NO_PROFILING_DATA', 'INFO', 'No GPU profiling data available — classification limited to frame buffer'),
    (29, 'PVC_OVERSIZED', 'INFO', 'PVC capacity significantly exceeds sustained usage — consider shrinking'),
    (30, 'PVC_NEAR_FULL', 'WARNING', 'PVC usage approaching capacity — consider expanding or investigate growth')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
