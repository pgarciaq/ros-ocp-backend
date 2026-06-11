INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (77, 'SPARSE_DATA', 'INFO', 'Recommendation based on limited data; accuracy improves with more observation time')
ON CONFLICT DO NOTHING;
