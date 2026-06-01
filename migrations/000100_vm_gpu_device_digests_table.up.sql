CREATE TABLE vm_gpu_device_digests (
    id BIGSERIAL PRIMARY KEY,
    vm_digest_id BIGINT NOT NULL REFERENCES daily_vm_digests(id) ON DELETE CASCADE,
    gpu_uuid TEXT NOT NULL,
    gpu_model TEXT NOT NULL DEFAULT '',
    util_avg_bp INTEGER NOT NULL DEFAULT 0,
    util_max_bp INTEGER NOT NULL DEFAULT 0,
    fb_used_avg_mib DOUBLE PRECISION NOT NULL DEFAULT 0,
    fb_used_max_mib DOUBLE PRECISION NOT NULL DEFAULT 0,
    sm_active_avg_bp INTEGER NOT NULL DEFAULT 0,
    tensor_avg_bp INTEGER NOT NULL DEFAULT 0,
    dram_avg_bp INTEGER NOT NULL DEFAULT 0,
    mig_profile TEXT NOT NULL DEFAULT '',
    max_slices INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_vm_gpu_device_digest_parent ON vm_gpu_device_digests(vm_digest_id);
CREATE UNIQUE INDEX idx_vm_gpu_device_digest_unique ON vm_gpu_device_digests(vm_digest_id, gpu_uuid);

ALTER TABLE daily_vm_digests DROP COLUMN IF EXISTS gpu_devices;
