CREATE TABLE instances (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN (
        'PENDING', 'PROVISIONING', 'RUNNING', 'STOPPING',
        'STOPPED', 'ERROR', 'DELETING', 'DELETED'
    )),
    cpu         INTEGER NOT NULL,
    memory_mb   INTEGER NOT NULL,
    disk_gb     INTEGER NOT NULL,
    image       TEXT NOT NULL,
    node_id     UUID REFERENCES compute_nodes (id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX idx_instances_tenant_id ON instances (tenant_id);
