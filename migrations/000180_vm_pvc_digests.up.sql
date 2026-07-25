CREATE TABLE vm_pvc_digests (
    id BIGSERIAL PRIMARY KEY,
    vm_digest_id BIGINT NOT NULL REFERENCES daily_vm_digests(id) ON DELETE CASCADE,
    pvc_name TEXT NOT NULL,
    disk_capacity_bytes BIGINT NOT NULL DEFAULT 0,
    volume_mode TEXT NOT NULL DEFAULT 'Filesystem'
);

CREATE INDEX idx_vm_pvc_digest_parent ON vm_pvc_digests(vm_digest_id);
CREATE UNIQUE INDEX idx_vm_pvc_digest_unique ON vm_pvc_digests(vm_digest_id, pvc_name);
