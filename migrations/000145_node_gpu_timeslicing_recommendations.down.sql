DROP TABLE IF EXISTS node_gpu_timeslicing_recommendation_history;
DROP TABLE IF EXISTS node_gpu_timeslicing_recommendations;
ALTER TABLE recommendation_sets
    DROP COLUMN IF EXISTS time_slicing_node,
    DROP COLUMN IF EXISTS time_slicing_replicas;
