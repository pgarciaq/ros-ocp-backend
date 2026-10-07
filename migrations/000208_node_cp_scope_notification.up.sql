-- #671 Track-M: control-plane-scope marker for node recommendations on
-- master-role nodes in 3+ master clusters. Follows the 000205 pattern
-- (INSERT + paired DELETE). INFO: framing only, changes no numbers.
INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (85, 'NODE_CP_SCOPE', 'INFO', 'Control-plane node — rightsizing via CPMS rolling update only, never below install minimums; quorum protected by mechanism');
