CREATE TABLE disks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    instance_id UUID REFERENCES instances (id),
    node_id     UUID NOT NULL REFERENCES compute_nodes (id),
    type        TEXT NOT NULL CHECK (type IN ('ROOT', 'DATA', 'BACKUP')),
    size_gb     INTEGER NOT NULL,
    file_path   TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_disks_tenant_id ON disks (tenant_id);
CREATE INDEX idx_disks_instance_id ON disks (instance_id) WHERE instance_id IS NOT NULL;
