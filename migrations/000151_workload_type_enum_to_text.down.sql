-- Recreate the enum with the original six types plus namespace.
CREATE TYPE sorted_workloadtype AS ENUM (
    'daemonset', 'deployment', 'deploymentconfig',
    'replicaset', 'replicationcontroller', 'statefulset', 'namespace'
);
ALTER TABLE workloads ALTER COLUMN workload_type TYPE sorted_workloadtype
    USING workload_type::sorted_workloadtype;
