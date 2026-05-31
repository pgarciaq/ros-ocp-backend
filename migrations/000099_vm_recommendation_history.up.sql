CREATE TABLE IF NOT EXISTS vm_recommendation_history (
    id BIGSERIAL PRIMARY KEY,
    org_id TEXT NOT NULL,
    cluster_id TEXT NOT NULL,
    vm_name TEXT NOT NULL,
    namespace TEXT NOT NULL,
    term TEXT NOT NULL,
    engine TEXT NOT NULL,
    recommended_vcpu INTEGER NOT NULL,
    recommended_memory_gib DOUBLE PRECISION NOT NULL,
    recommended_instance_type TEXT NOT NULL DEFAULT '',
    gpu_classification TEXT NOT NULL DEFAULT '',
    recommended_gpu_action TEXT NOT NULL DEFAULT '',
    is_idle BOOLEAN NOT NULL DEFAULT false,
    is_abandoned BOOLEAN NOT NULL DEFAULT false,
    confidence TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_vm_rec_history_lookup
    ON vm_recommendation_history (org_id, cluster_id, vm_name, namespace, term, engine, created_at DESC);
