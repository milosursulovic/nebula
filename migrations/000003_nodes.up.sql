CREATE TABLE compute_nodes (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hostname             TEXT NOT NULL UNIQUE,
    ip                   TEXT NOT NULL,
    status               TEXT NOT NULL CHECK (status IN ('ONLINE', 'DEGRADED', 'OFFLINE', 'DRAINING')),
    total_cpu            INTEGER NOT NULL,
    available_cpu        INTEGER NOT NULL,
    total_memory_mb      INTEGER NOT NULL,
    available_memory_mb  INTEGER NOT NULL,
    total_disk_gb        INTEGER NOT NULL,
    available_disk_gb    INTEGER NOT NULL,
    load_average         DOUBLE PRECISION NOT NULL DEFAULT 0,
    running_instances    INTEGER NOT NULL DEFAULT 0,
    last_heartbeat_at    TIMESTAMPTZ,
    token_hash           TEXT NOT NULL UNIQUE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_compute_nodes_status ON compute_nodes (status);
