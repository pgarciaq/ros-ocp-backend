-- #671 Track-M: node role for master-role flagging. Nullable (old
-- CSVs lack the column; empty reads as unknown, never a role).
-- plugin: none (store-only column)
ALTER TABLE daily_node_digests ADD COLUMN IF NOT EXISTS node_role TEXT;
