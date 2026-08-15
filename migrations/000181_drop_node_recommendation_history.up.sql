-- Drop the unused ghost table created only by testdb/bench stubs so 000179
-- could ALTER it. Node CPU/memory recommendations have no history product
-- (#464). DROP IF EXISTS is a no-op on databases that never had the stub.
DROP TABLE IF EXISTS node_recommendation_history;
