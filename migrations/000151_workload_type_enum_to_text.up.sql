-- COST-7274: Remove fixed workload_type enum to accept arbitrary Kubernetes owner kinds.
ALTER TABLE workloads ALTER COLUMN workload_type TYPE TEXT;
DROP TYPE IF EXISTS sorted_workloadtype;
