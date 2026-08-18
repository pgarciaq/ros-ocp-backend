-- #491: GPU timeslicing business-hours nested detail (code 81).
-- Persist tables stay all-hours. BH is recomputed at read time on
-- GET .../gpu/timeslicing/{node}. Do not add schedule_type to timeslicing rows.

INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (81, 'GPU_TS_BH_CLUSTER_WINDOW', 'WARNING',
     'Business-hours GPU time-slicing uses the cluster office window — overnight training and off-hours bursts are excluded')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
