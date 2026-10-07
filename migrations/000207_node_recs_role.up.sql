-- #671 Track-M: node role passthrough for API visibility (role visible
-- on list/detail alongside machineset_name). Nullable; empty = unknown.
-- plugin: none (store-only column)
ALTER TABLE node_recommendations ADD COLUMN IF NOT EXISTS node_role TEXT;
