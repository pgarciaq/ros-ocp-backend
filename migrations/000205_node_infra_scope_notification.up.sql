-- #670 Track-I: infra-scope marker for node recommendations served on
-- infra machinesets. Follows the 000197 pattern (INSERT + paired DELETE).
-- INFO severity: framing only, changes no numbers. Narrow-side matching
-- (see isInfraMachineSet): a missed infra node stays silent, never misframed.
INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (84, 'NODE_INFRA_SCOPE', 'INFO', 'Infrastructure node — apply via the infra MachineSet template (keep N+1, drain in order); smaller instances, never fewer subscriptions');
