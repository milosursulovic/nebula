CREATE TABLE jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type            TEXT NOT NULL CHECK (type IN (
        'CREATE_INSTANCE', 'DELETE_INSTANCE', 'START_INSTANCE', 'STOP_INSTANCE',
        'CREATE_NETWORK', 'DELETE_NETWORK', 'CREATE_DISK', 'DELETE_DISK'
    )),
    status          TEXT NOT NULL CHECK (status IN (
        'QUEUED', 'RUNNING', 'SUCCESS', 'FAILED', 'CANCELLED'
    )),
    tenant_id       UUID REFERENCES tenants (id),
    instance_id     UUID REFERENCES instances (id),
    node_id         UUID REFERENCES compute_nodes (id),
    attempts        INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 5,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    error           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ
);

CREATE INDEX idx_jobs_status_next_attempt ON jobs (status, next_attempt_at);
