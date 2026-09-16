-- #408 W0.3: hosted-scope marker for node recommendations computed under
-- hosted topology. Follows the 000122 pattern (INSERT + paired DELETE).
INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (83, 'NODE_HOSTED_SCOPE', 'INFO', 'Hosted cluster topology — node recommendations cover worker nodes only');
